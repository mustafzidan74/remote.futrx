package postrun

import (
	"context"
	"log"
	"sync"
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicenotify "github.com/futrx-com/remote.futrx.com/internal/service/notify"
	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
)

// followUpDelay is the pause before a synthetic run starts. It exists so the
// hub has settled its run lock and the browser has painted the finished turn
// before the next prompt lands — an instant follow-up reads as a glitch and
// races the FinishRun that the observer callback runs ahead of.
const followUpDelay = 2 * time.Second

// Chats is the driver's view of chat storage: read one chat's policy, and
// write back the round counter or a disarmed switch.
type Chats interface {
	Get(ctx context.Context, id servicechat.ID) (servicechat.Meta, error)
	Update(ctx context.Context, id servicechat.ID, fn func(*servicechat.Meta)) (servicechat.Meta, error)
}

// Runs answers whether a chat already has work in flight. The run hub
// implements it.
type Runs interface {
	IsRunning(id servicechat.ID) bool
}

// Starter launches a follow-up run. The prompt service implements it; the
// driver hands it the same StartInput a human prompt would use, differing only
// in the synthetic label and the actor.
type Starter interface {
	Start(input prompt.StartInput, emitTransient func(servicechat.Event)) (prompt.RunHandle, error)
}

// Schedules reports whether a scheduled task drives a chat. Those chats run on
// the scheduler's cadence and must never be auto-continued.
type Schedules interface {
	HasTasksForChat(ctx context.Context, chatID servicechat.ID) bool
}

// Notifier publishes an autopilot outcome through the notification service.
// It is implemented in the composition package, the only place allowed to
// enrich an event with chat and project identity.
type Notifier interface {
	PublishChatEvent(ctx context.Context, chatID servicechat.ID, event servicenotify.Event)
}

// Dependencies groups the driver's collaborators. Chats and Starter are
// required; everything else degrades gracefully to "no opinion".
type Dependencies struct {
	Chats     Chats
	Runs      Runs
	Starter   Starter
	Schedules Schedules
	Notifier  Notifier
}

// Driver is the RunObserver that turns a settled run into the next action.
type Driver struct {
	chats     Chats
	runs      Runs
	starter   Starter
	schedules Schedules
	notifier  Notifier

	now   func() time.Time
	delay func(time.Duration)

	// pending tracks in-flight follow-up goroutines so a test can wait for
	// the driver to finish reacting instead of sleeping.
	pending sync.WaitGroup
}

var _ prompt.RunObserver = (*Driver)(nil)

type Option func(*Driver)

// WithClock replaces the clock the duration budget is measured against.
func WithClock(now func() time.Time) Option {
	return func(d *Driver) {
		if now != nil {
			d.now = now
		}
	}
}

// WithDelay replaces the pause before a follow-up run. Tests pass a no-op so
// the reaction is immediate.
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
		runs:      deps.Runs,
		starter:   deps.Starter,
		schedules: deps.Schedules,
		notifier:  deps.Notifier,
		now:       time.Now,
		delay:     time.Sleep,
	}
	for _, option := range options {
		if option != nil {
			option(driver)
		}
	}
	return driver
}

// RunSettled reacts to a finished run.
//
// The prompt service calls observers on the run goroutine, before the hub has
// released the run lock, so the work happens on a goroutine of its own. Its
// context is rooted in Background for the same reason the run was: a follow-up
// must not die because the browser that started the first prompt went away.
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

// RunToolStarted is part of the observer contract and has nothing to do here:
// a tool call means the agent is still working, which is exactly the state
// this driver waits out.
func (d *Driver) RunToolStarted(context.Context, servicechat.ID, string) {}

// Wait blocks until every in-flight reaction has finished. It exists for tests
// and for an orderly shutdown; nothing on the request path calls it.
func (d *Driver) Wait() {
	if d != nil {
		d.pending.Wait()
	}
}

func (d *Driver) settle(ctx context.Context, outcome prompt.RunOutcome) {
	meta, err := d.chats.Get(ctx, outcome.ChatID)
	if err != nil {
		return
	}
	// Reading the policies is cheap; asking the scheduler is a store read, so
	// it only happens for a chat that actually has a policy armed.
	if !meta.Autopilot.Enabled && !meta.AutoTest.Enabled {
		return
	}

	decision := Decide(meta, Outcome{
		Output:    outcome.Output,
		Failed:    outcome.Err != nil || outcome.Cancelled,
		Scheduled: outcome.ScheduledTaskID != "",
		Synthetic: servicechat.NormalizeSynthetic(outcome.Synthetic),
	}, Conditions{
		RunActive:     d.runs != nil && d.runs.IsRunning(outcome.ChatID),
		ScheduledChat: d.schedules != nil && d.schedules.HasTasksForChat(ctx, outcome.ChatID),
		Now:           d.now(),
	})

	switch decision.Action {
	case ActionStop:
		d.stop(ctx, outcome.ChatID, meta, decision)
	case ActionTest:
		d.followUp(ctx, outcome.ChatID, meta, decision)
	case ActionContinue:
		d.continueRound(ctx, outcome.ChatID, meta, decision)
	}
}

// stop disarms autopilot and tells the operator why. Disarming is persisted
// before the notification so a delivery failure cannot leave a loop armed.
func (d *Driver) stop(
	ctx context.Context,
	chatID servicechat.ID,
	meta servicechat.Meta,
	decision Decision,
) {
	updated, err := d.chats.Update(ctx, chatID, func(m *servicechat.Meta) {
		m.Autopilot.Enabled = false
	})
	if err != nil {
		log.Printf("postrun: disarm autopilot for chat %s: %v", chatID, err)
		updated = meta
	}
	d.publishStop(ctx, chatID, updated.Autopilot.RoundsUsed, decision)
}

// continueRound spends one round before starting it. Charging the budget up
// front is deliberate: if the process dies between the write and the run, the
// loop resumes one round short rather than one round over.
func (d *Driver) continueRound(
	ctx context.Context,
	chatID servicechat.ID,
	meta servicechat.Meta,
	decision Decision,
) {
	updated, err := d.chats.Update(ctx, chatID, func(m *servicechat.Meta) {
		m.Autopilot.RoundsUsed++
	})
	if err != nil {
		log.Printf("postrun: count autopilot round for chat %s: %v", chatID, err)
		return
	}
	d.followUp(ctx, chatID, updated, decision)
}

// followUp waits out the settle delay, re-checks the one-run-per-chat
// invariant, and starts the synthetic run.
func (d *Driver) followUp(
	ctx context.Context,
	chatID servicechat.ID,
	meta servicechat.Meta,
	decision Decision,
) {
	d.delay(followUpDelay)

	// The pause is long enough for a human to send their own prompt. Theirs
	// wins: the driver drops its follow-up rather than queueing behind it,
	// and will be offered another chance when that run settles.
	if d.runs != nil && d.runs.IsRunning(chatID) {
		return
	}

	// Team mode reacts to the same settled run on a goroutine of its own, so
	// the phase read during Decide may already be stale by now. Re-reading it
	// here is what makes "the team goes first" hold without the two drivers
	// having to know about each other.
	if current, err := d.chats.Get(ctx, chatID); err == nil && servicechat.TeamActive(current.Team) {
		return
	}

	actor := meta.PolicyActor()
	_, err := d.starter.Start(prompt.StartInput{
		ChatID:        chatID,
		Prompt:        decision.Prompt,
		Actor:         prompt.Actor{Email: actor},
		Synthetic:     decision.Synthetic,
		ParentContext: ctx,
	}, nil)
	if err == nil {
		return
	}

	// A follow-up that cannot start is the end of the loop. Leaving autopilot
	// armed after a failed start would mean the chat looks like it is working
	// when nothing is.
	log.Printf("postrun: start %s follow-up for chat %s: %v", decision.Synthetic, chatID, err)
	if decision.Synthetic != servicechat.SyntheticAutopilot {
		return
	}
	d.stop(ctx, chatID, meta, Decision{Action: ActionStop, Reason: ReasonFailed})
}

func (d *Driver) publishStop(
	ctx context.Context,
	chatID servicechat.ID,
	rounds int,
	decision Decision,
) {
	if d.notifier == nil {
		return
	}
	// A blocked agent is the one stop that needs a human right now, so it
	// reports as attention rather than as a finished run.
	kind, status := servicenotify.KindRunFinished, servicenotify.StatusFinished
	if decision.Reason == ReasonBlocked {
		kind, status = servicenotify.KindNeedsAttention, servicenotify.StatusWaiting
	}
	d.notifier.PublishChatEvent(ctx, chatID, servicenotify.Event{
		Event:   kind,
		Status:  status,
		Summary: servicenotify.Summary(StopSummary(rounds, decision.Reason, decision.Detail)),
		// One message per autopilot session per reason: a loop cannot stop
		// for the same cause twice.
		DedupeKey: "autopilot:" + string(chatID) + ":" + string(decision.Reason),
	})
}
