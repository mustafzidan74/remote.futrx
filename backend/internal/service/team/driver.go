package team

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicenotify "github.com/futrx-com/remote.futrx.com/internal/service/notify"
	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
)

// hopDelay is the pause before a hop starts. It exists for the same reason the
// post-run driver's does: observers run on the run goroutine, *before* the hub
// releases the run lock, so a hop that started immediately would race the
// FinishRun of the turn that triggered it.
const hopDelay = 2 * time.Second

// Chats is the driver's view of chat storage.
type Chats interface {
	Get(ctx context.Context, id servicechat.ID) (servicechat.Meta, error)
	Update(ctx context.Context, id servicechat.ID, fn func(*servicechat.Meta)) (servicechat.Meta, error)
}

// ChatFactory creates the companion chats. The chat service implements it, so
// a companion picks up the project's workspace path and always-on skills the
// same way a chat the operator creates by hand does.
type ChatFactory interface {
	Create(ctx context.Context, in servicechat.CreateInput) (servicechat.Meta, error)
}

// Runs answers whether a chat already has work in flight. The run hub
// implements it.
type Runs interface {
	IsRunning(id servicechat.ID) bool
}

// Starter launches a hop. The prompt service implements it.
type Starter interface {
	Start(input prompt.StartInput, emitTransient func(servicechat.Event)) (prompt.RunHandle, error)
}

// Emitter persists and broadcasts one event into a chat. The run hub
// implements it; the driver uses it only for the closing summary.
type Emitter interface {
	Emit(id servicechat.ID, ev servicechat.Event)
}

// Providers reports which agent providers have a usable host-side login, which
// is what decides whether a review really can come from a second opinion.
type Providers interface {
	Connected() []servicechat.Provider
}

// SkillLibrary names the global skills an operator has published. The driver
// only ever *narrows* its wish list to what exists, so a box without the
// review-protocol skill still runs a review — just without the protocol.
type SkillLibrary interface {
	GlobalSkillNames(ctx context.Context) []string
}

// Notifier publishes a finished team session through the notification service.
// It is implemented in the composition package, the only place allowed to
// enrich an event with chat and project identity.
type Notifier interface {
	PublishChatEvent(ctx context.Context, chatID servicechat.ID, event servicenotify.Event)
}

// Dependencies groups the driver's collaborators. Chats, ChatFactory and
// Starter are required; everything else degrades to "no opinion".
type Dependencies struct {
	Chats       Chats
	ChatFactory ChatFactory
	Runs        Runs
	Starter     Starter
	Events      Emitter
	Providers   Providers
	Skills      SkillLibrary
	Notifier    Notifier
}

// Driver is the RunObserver that walks a team session from one hop to the next.
type Driver struct {
	chats     Chats
	factory   ChatFactory
	runs      Runs
	starter   Starter
	events    Emitter
	providers Providers
	skills    SkillLibrary
	notifier  Notifier

	now   func() time.Time
	delay func(time.Duration)

	// queued holds at most one deferred hop per target chat. A hop lands here
	// when its target is busy — a human typed into the reviewer's chat, say —
	// and is retried the next time that chat settles.
	mu     sync.Mutex
	queued map[servicechat.ID]queuedHop

	// pending tracks in-flight reactions so a test can wait for the driver
	// instead of sleeping.
	pending sync.WaitGroup
}

type queuedHop struct {
	parent   servicechat.ID
	decision Decision
}

var _ prompt.RunObserver = (*Driver)(nil)

type Option func(*Driver)

// WithClock replaces the clock hops are stamped with.
func WithClock(now func() time.Time) Option {
	return func(d *Driver) {
		if now != nil {
			d.now = now
		}
	}
}

// WithDelay replaces the pause before a hop starts. Tests pass a no-op.
func WithDelay(delay func(time.Duration)) Option {
	return func(d *Driver) {
		if delay != nil {
			d.delay = delay
		}
	}
}

// New builds the driver. It starts no goroutines of its own: every reaction is
// scoped to one settled run.
func New(deps Dependencies, options ...Option) *Driver {
	driver := &Driver{
		chats:     deps.Chats,
		factory:   deps.ChatFactory,
		runs:      deps.Runs,
		starter:   deps.Starter,
		events:    deps.Events,
		providers: deps.Providers,
		skills:    deps.Skills,
		notifier:  deps.Notifier,
		now:       time.Now,
		delay:     time.Sleep,
		queued:    map[servicechat.ID]queuedHop{},
	}
	for _, option := range options {
		if option != nil {
			option(driver)
		}
	}
	return driver
}

// RunSettled reacts to a finished run on its own goroutine, rooted in
// Background: a team session must not die because the browser that opened it
// went away, which is the same reason the run itself outlives the request.
func (d *Driver) RunSettled(_ context.Context, outcome prompt.RunOutcome) {
	if d == nil || d.chats == nil || d.starter == nil {
		return
	}
	d.pending.Add(1)
	go func() {
		defer d.pending.Done()
		d.settle(context.Background(), outcome)
	}()
}

// RunToolStarted is part of the observer contract and has nothing to do here.
func (d *Driver) RunToolStarted(context.Context, servicechat.ID, string) {}

// Wait blocks until every in-flight reaction has finished. It exists for tests
// and for an orderly shutdown; nothing on the request path calls it.
func (d *Driver) Wait() {
	if d != nil {
		d.pending.Wait()
	}
}

func (d *Driver) settle(ctx context.Context, outcome prompt.RunOutcome) {
	settled, err := d.chats.Get(ctx, outcome.ChatID)
	if err != nil {
		return
	}

	// A companion chat carries the seat it fills and the parent it answers to;
	// anything else is the implementer chat itself.
	role, parentID := servicechat.TeamRoleImplementer, outcome.ChatID
	if settled.CompanionOf != "" {
		role, parentID = strings.TrimSpace(settled.CompanionRole), settled.CompanionOf
	}

	parent := settled
	if parentID != outcome.ChatID {
		if parent, err = d.chats.Get(ctx, parentID); err != nil {
			return
		}
	}
	if !parent.Team.Enabled {
		d.discard(outcome.ChatID)
		return
	}

	decision := Decide(parent.Team, Signal{
		Role:      role,
		Output:    outcome.Output,
		Failed:    outcome.Err != nil || outcome.Cancelled,
		Scheduled: outcome.ScheduledTaskID != "",
		Synthetic: servicechat.NormalizeSynthetic(outcome.Synthetic),
	})
	if decision.Action == ActionNone {
		// Nothing new to do, which is exactly the moment a hop that was
		// deferred because this chat was busy gets its retry.
		d.drain(ctx, outcome.ChatID)
		return
	}
	d.apply(ctx, parent, decision)
}

func (d *Driver) apply(ctx context.Context, parent servicechat.Meta, decision Decision) {
	switch decision.Action {
	case ActionReview, ActionTest:
		d.startCompanionHop(ctx, parent, decision)
	case ActionFix:
		d.startFixHop(ctx, parent, decision)
	case ActionFinish:
		d.close(ctx, parent, decision, FinishSummary(parent.Team, decision.Verdict, decision.LoopsUsed))
	case ActionAbort:
		d.close(ctx, parent, decision, AbortSummary(decision.Role, decision.Reason))
	}
}

// startCompanionHop resolves the cast, makes sure the seat has a chat, records
// the hop, and starts the run.
//
// The state is written *before* the run starts. If the process dies in
// between, the chat reads as "reviewing" with a hop on the timeline and no run
// behind it, which an operator can see and restart — the opposite order would
// leave a running review that nothing on the page admits to.
func (d *Driver) startCompanionHop(ctx context.Context, parent servicechat.Meta, decision Decision) {
	roles := ResolveRoles(parent.Team, parent.Provider, d.connected())
	seat := roleSeat(roles, decision.Role)

	companion, err := d.companionChat(ctx, parent, decision.Role, seat)
	if err != nil {
		log.Printf("team: companion chat for %s of chat %s: %v", decision.Role, parent.ID, err)
		d.close(ctx, parent, Decision{Phase: servicechat.TeamPhaseError, LoopsUsed: decision.LoopsUsed},
			AbortSummary(decision.Role, ReasonRunFailed))
		return
	}
	seat.ChatID = companion.ID

	updated, err := d.chats.Update(ctx, parent.ID, func(m *servicechat.Meta) {
		m.Team.Roles = withSeat(roles, decision.Role, seat)
		m.Team.Phase = decision.Phase
		m.Team.LoopsUsed = decision.LoopsUsed
		m.Team.UpdatedAt = d.clock().UnixMilli()
		m.Team.Hops = servicechat.AppendTeamHop(m.Team.Hops, servicechat.TeamHop{
			Loop:   decision.LoopsUsed,
			Role:   decision.Role,
			Kind:   decision.Synthetic,
			ChatID: companion.ID,
			At:     d.clock().UnixMilli(),
		})
	})
	if err != nil {
		log.Printf("team: record %s hop for chat %s: %v", decision.Role, parent.ID, err)
		return
	}
	d.startHop(ctx, updated, companion.ID, decision)
}

// startFixHop sends the findings back to the implementer chat.
func (d *Driver) startFixHop(ctx context.Context, parent servicechat.Meta, decision Decision) {
	// The loop counter is spent before the run starts, for the same reason
	// autopilot charges its round up front: a crash in between must leave the
	// budget one short rather than one over.
	updated, err := d.chats.Update(ctx, parent.ID, func(m *servicechat.Meta) {
		m.Team.Phase = decision.Phase
		m.Team.LoopsUsed = decision.LoopsUsed
		m.Team.Verdict = decision.Verdict
		m.Team.UpdatedAt = d.clock().UnixMilli()
		m.Team.Hops = servicechat.AppendTeamHop(m.Team.Hops, servicechat.TeamHop{
			Loop:     decision.LoopsUsed,
			Role:     servicechat.TeamRoleImplementer,
			Kind:     decision.Synthetic,
			ChatID:   parent.ID,
			Verdict:  decision.Verdict,
			Findings: decision.Findings,
			At:       d.clock().UnixMilli(),
		})
	})
	if err != nil {
		log.Printf("team: record fix hop for chat %s: %v", parent.ID, err)
		return
	}
	d.startHop(ctx, updated, parent.ID, decision)
}

// startHop waits out the settle delay, re-checks the one-run-per-chat
// invariant, and starts the run — deferring it if the target is busy.
func (d *Driver) startHop(
	ctx context.Context,
	parent servicechat.Meta,
	target servicechat.ID,
	decision Decision,
) {
	d.delay(hopDelay)

	if d.runs != nil && d.runs.IsRunning(target) {
		// Someone else is using that chat. Hold the hop and retry when their
		// run settles rather than losing the loop to a timing accident.
		d.enqueue(target, queuedHop{parent: parent.ID, decision: decision})
		return
	}

	_, err := d.starter.Start(prompt.StartInput{
		ChatID:        target,
		Prompt:        decision.Prompt,
		Actor:         prompt.Actor{Email: parent.PolicyActor()},
		Synthetic:     decision.Synthetic,
		ParentContext: ctx,
	}, nil)
	if err == nil {
		return
	}
	log.Printf("team: start %s hop for chat %s: %v", decision.Synthetic, target, err)
	d.close(ctx, parent, Decision{Phase: servicechat.TeamPhaseError, LoopsUsed: decision.LoopsUsed},
		AbortSummary(decision.Role, ReasonRunFailed))
}

// close writes the terminal phase, posts the summary into the parent chat, and
// publishes one notification. The state is persisted before the message so a
// delivery failure cannot leave a session looking like it is still running.
func (d *Driver) close(
	ctx context.Context,
	parent servicechat.Meta,
	decision Decision,
	summary string,
) {
	d.discard(parent.ID)
	stamp := d.clock().UnixMilli()
	if _, err := d.chats.Update(ctx, parent.ID, func(m *servicechat.Meta) {
		m.Team.Phase = decision.Phase
		m.Team.Verdict = decision.Verdict
		m.Team.LoopsUsed = decision.LoopsUsed
		m.Team.UpdatedAt = stamp
		m.Team.Hops = servicechat.AppendTeamHop(m.Team.Hops, servicechat.TeamHop{
			Loop:     decision.LoopsUsed,
			Role:     servicechat.TeamRoleImplementer,
			Kind:     servicechat.SyntheticTeamSummary,
			ChatID:   parent.ID,
			Verdict:  decision.Verdict,
			Findings: decision.Findings,
			At:       stamp,
		})
	}); err != nil {
		log.Printf("team: close session for chat %s: %v", parent.ID, err)
	}

	if d.events != nil {
		// A user-typed event with a synthetic label is the shape the browser
		// already badges as platform-issued, so the closing note needs no
		// render path of its own.
		d.events.Emit(parent.ID, servicechat.Event{
			T:         stamp,
			Type:      "user",
			Text:      summary,
			Synthetic: servicechat.SyntheticTeamSummary,
		})
	}
	if d.notifier == nil {
		return
	}
	d.notifier.PublishChatEvent(ctx, parent.ID, servicenotify.Event{
		Event:   servicenotify.KindRunFinished,
		Status:  servicenotify.StatusFinished,
		Summary: servicenotify.Summary(summary),
		// One message per session per terminal phase: a loop cannot finish
		// twice for the same reason.
		DedupeKey: "team:" + string(parent.ID) + ":" + decision.Phase + ":" + decision.Verdict,
	})
}

// companionChat returns the seat's chat, creating it the first time. Reuse is
// deliberate: one reviewer chat per parent keeps every review of that task in
// one readable thread instead of littering the project with a chat per loop.
func (d *Driver) companionChat(
	ctx context.Context,
	parent servicechat.Meta,
	role string,
	seat servicechat.TeamRole,
) (servicechat.Meta, error) {
	if seat.ChatID != "" {
		existing, err := d.chats.Get(ctx, seat.ChatID)
		if err == nil && existing.CompanionOf == parent.ID && existing.CompanionRole == role {
			return existing, nil
		}
	}
	if d.factory == nil {
		return servicechat.Meta{}, errNoChatFactory
	}
	return d.factory.Create(ctx, servicechat.CreateInput{
		Title:     CompanionTitle(role, parent.Title),
		Provider:  seat.Provider,
		Model:     seat.Model,
		Mode:      CompanionMode(role),
		ProjectID: parent.ProjectID,
		// A loose chat has no project to resolve a workspace from, so the
		// companion inherits the parent's directory directly.
		Cwd:            companionCwd(parent),
		SelectedSkills: CompanionSkills(role, seat.Provider, d.globalSkillNames(ctx)),
		CompanionOf:    parent.ID,
		CompanionRole:  role,
	})
}

func (d *Driver) enqueue(target servicechat.ID, hop queuedHop) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.queued == nil {
		d.queued = map[servicechat.ID]queuedHop{}
	}
	// One deferred hop per chat. A newer decision supersedes an older one:
	// the loop only ever wants the next step, never a backlog of them.
	d.queued[target] = hop
}

func (d *Driver) discard(target servicechat.ID) {
	d.mu.Lock()
	defer d.mu.Unlock()
	delete(d.queued, target)
}

// drain retries a hop that was deferred because its target was busy.
func (d *Driver) drain(ctx context.Context, target servicechat.ID) {
	d.mu.Lock()
	hop, ok := d.queued[target]
	delete(d.queued, target)
	d.mu.Unlock()
	if !ok {
		return
	}
	parent, err := d.chats.Get(ctx, hop.parent)
	// A session switched off while a hop waited must not resume.
	if err != nil || !parent.Team.Enabled {
		return
	}
	d.startHop(ctx, parent, target, hop.decision)
}

func (d *Driver) connected() []servicechat.Provider {
	if d.providers == nil {
		return nil
	}
	return d.providers.Connected()
}

func (d *Driver) globalSkillNames(ctx context.Context) []string {
	if d.skills == nil {
		return nil
	}
	return d.skills.GlobalSkillNames(ctx)
}

func (d *Driver) clock() time.Time {
	if d == nil || d.now == nil {
		return time.Now()
	}
	return d.now()
}

func companionCwd(parent servicechat.Meta) string {
	if parent.ProjectID != "" {
		return ""
	}
	return parent.Cwd
}

func roleSeat(roles servicechat.TeamRoles, role string) servicechat.TeamRole {
	switch role {
	case servicechat.TeamRoleReviewer:
		return roles.Reviewer
	case servicechat.TeamRoleTester:
		return roles.Tester
	default:
		return roles.Implementer
	}
}

func withSeat(roles servicechat.TeamRoles, role string, seat servicechat.TeamRole) servicechat.TeamRoles {
	switch role {
	case servicechat.TeamRoleReviewer:
		roles.Reviewer = seat
	case servicechat.TeamRoleTester:
		roles.Tester = seat
	}
	return roles
}
