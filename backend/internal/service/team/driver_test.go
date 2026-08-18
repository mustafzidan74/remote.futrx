package team

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicenotify "github.com/futrx-com/remote.futrx.com/internal/service/notify"
	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
)

// chatStore is an in-memory stand-in for the chat repository and the chat
// service's create path. It is deliberately one type: the driver's whole job
// is to keep a parent and its companions consistent, and splitting them across
// two fakes would hide a mismatch rather than catch it.
type chatStore struct {
	mu    sync.Mutex
	metas map[servicechat.ID]servicechat.Meta
	next  int
	// created records every companion chat, so a test can assert reuse.
	created []servicechat.Meta
}

func newChatStore(parent servicechat.Meta) *chatStore {
	return &chatStore{metas: map[servicechat.ID]servicechat.Meta{parent.ID: parent}}
}

func (s *chatStore) Get(_ context.Context, id servicechat.ID) (servicechat.Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, ok := s.metas[id]
	if !ok {
		return servicechat.Meta{}, servicechat.ErrNotFound
	}
	return meta, nil
}

func (s *chatStore) Update(
	_ context.Context,
	id servicechat.ID,
	fn func(*servicechat.Meta),
) (servicechat.Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	meta, ok := s.metas[id]
	if !ok {
		return servicechat.Meta{}, servicechat.ErrNotFound
	}
	fn(&meta)
	meta.Team = servicechat.NormalizeTeam(meta.Team)
	s.metas[id] = meta
	return meta, nil
}

func (s *chatStore) Create(
	_ context.Context,
	in servicechat.CreateInput,
) (servicechat.Meta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	meta := servicechat.Meta{
		ID:             servicechat.ID("companion" + strconv.Itoa(s.next)),
		Title:          in.Title,
		Provider:       servicechat.NormalizeProvider(in.Provider),
		Model:          in.Model,
		Mode:           in.Mode,
		ProjectID:      in.ProjectID,
		Cwd:            in.Cwd,
		SelectedSkills: in.SelectedSkills,
		CompanionOf:    in.CompanionOf,
		CompanionRole:  in.CompanionRole,
	}
	s.metas[meta.ID] = meta
	s.created = append(s.created, meta)
	return meta, nil
}

type startedRun struct {
	chatID    servicechat.ID
	prompt    string
	synthetic string
	actor     string
}

type runRecorder struct {
	mu      sync.Mutex
	runs    []startedRun
	running map[servicechat.ID]bool
	err     error
}

func newRunRecorder() *runRecorder {
	return &runRecorder{running: map[servicechat.ID]bool{}}
}

func (r *runRecorder) Start(
	input prompt.StartInput,
	_ func(servicechat.Event),
) (prompt.RunHandle, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return prompt.RunHandle{}, r.err
	}
	r.runs = append(r.runs, startedRun{
		chatID:    input.ChatID,
		prompt:    input.Prompt,
		synthetic: input.Synthetic,
		actor:     input.Actor.Email,
	})
	return prompt.RunHandle{}, nil
}

func (r *runRecorder) IsRunning(id servicechat.ID) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running[id]
}

func (r *runRecorder) setRunning(id servicechat.ID, running bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.running[id] = running
}

func (r *runRecorder) started() []startedRun {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]startedRun(nil), r.runs...)
}

type eventRecorder struct {
	mu     sync.Mutex
	events []servicechat.Event
}

func (e *eventRecorder) Emit(_ servicechat.ID, ev servicechat.Event) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.events = append(e.events, ev)
}

func (e *eventRecorder) emitted() []servicechat.Event {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]servicechat.Event(nil), e.events...)
}

type notifyRecorder struct {
	mu     sync.Mutex
	events []servicenotify.Event
}

func (n *notifyRecorder) PublishChatEvent(
	_ context.Context,
	_ servicechat.ID,
	event servicenotify.Event,
) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, event)
}

func (n *notifyRecorder) published() []servicenotify.Event {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]servicenotify.Event(nil), n.events...)
}

type fixedProviders []servicechat.Provider

func (p fixedProviders) Connected() []servicechat.Provider { return p }

type fixedSkills []string

func (s fixedSkills) GlobalSkillNames(context.Context) []string { return s }

type harness struct {
	store    *chatStore
	runs     *runRecorder
	events   *eventRecorder
	notifier *notifyRecorder
	driver   *Driver
}

func newHarness(t *testing.T, parent servicechat.Meta) *harness {
	t.Helper()
	store := newChatStore(parent)
	runs := newRunRecorder()
	events := &eventRecorder{}
	notifier := &notifyRecorder{}
	driver := New(Dependencies{
		Chats:       store,
		ChatFactory: store,
		Runs:        runs,
		Starter:     runs,
		Events:      events,
		Providers: fixedProviders{
			servicechat.ProviderClaude, servicechat.ProviderCodex,
		},
		Skills:   fixedSkills{"review-protocol", "clean-code-guard", "playwright-e2e"},
		Notifier: notifier,
	}, WithDelay(func(time.Duration) {}), WithClock(func() time.Time {
		return time.UnixMilli(1_700_000_000_000)
	}))
	return &harness{store: store, runs: runs, events: events, notifier: notifier, driver: driver}
}

func (h *harness) settle(chatID servicechat.ID, outcome prompt.RunOutcome) {
	outcome.ChatID = chatID
	h.driver.RunSettled(context.Background(), outcome)
	h.driver.Wait()
}

// settleCompanion mirrors what the prompt service reports for a hop the loop
// started: the run carries the synthetic label the driver gave it, which is
// what tells a verdict apart from a human typing into the same chat.
func (h *harness) settleCompanion(
	chatID servicechat.ID,
	synthetic string,
	outcome prompt.RunOutcome,
) {
	outcome.Synthetic = synthetic
	h.settle(chatID, outcome)
}

func (h *harness) parent(t *testing.T) servicechat.Meta {
	t.Helper()
	meta, err := h.store.Get(context.Background(), "parent001")
	if err != nil {
		t.Fatal(err)
	}
	return meta
}

func teamParent() servicechat.Meta {
	return servicechat.Meta{
		ID:        "parent001",
		Title:     "Ship the checkout flow",
		Provider:  servicechat.ProviderClaude,
		ProjectID: "proj1",
		Team: servicechat.TeamPolicy{
			Enabled:   true,
			MaxLoops:  2,
			AutoFix:   true,
			EnabledBy: "operator@example.com",
			Roles: servicechat.TeamRoles{
				Implementer: servicechat.TeamRole{Enabled: true},
				Reviewer:    servicechat.TeamRole{Enabled: true},
				Tester:      servicechat.TeamRole{Enabled: true},
			},
		},
	}
}

// The whole point of the feature in one test: one human prompt, a review by a
// different provider, a fix round, a passing test, and a summary — with no
// further human input anywhere in the chain.
func TestDriverRunsAReviewFixTestLoop(t *testing.T) {
	h := newHarness(t, teamParent())

	h.settle("parent001", prompt.RunOutcome{Output: "done implementing"})

	parent := h.parent(t)
	if parent.Team.Phase != servicechat.TeamPhaseReviewing {
		t.Fatalf("phase = %q, want reviewing", parent.Team.Phase)
	}
	reviewer := parent.Team.Roles.Reviewer
	if reviewer.ChatID == "" {
		t.Fatal("no reviewer companion chat was created")
	}
	// Fresh eyes: a Claude chat must not be reviewed by Claude when Codex is
	// connected.
	if reviewer.Provider != servicechat.ProviderCodex {
		t.Errorf("reviewer provider = %q, want codex", reviewer.Provider)
	}
	runs := h.runs.started()
	if len(runs) != 1 || runs[0].chatID != reviewer.ChatID ||
		runs[0].synthetic != servicechat.SyntheticTeamReview {
		t.Fatalf("first hop = %+v", runs)
	}
	if runs[0].actor != "operator@example.com" {
		t.Errorf("actor = %q, want the human who armed the team", runs[0].actor)
	}

	h.settleCompanion(reviewer.ChatID, servicechat.SyntheticTeamReview,
		prompt.RunOutcome{Output: "VERDICT: FIX\n1. handle the empty cart"})

	parent = h.parent(t)
	if parent.Team.Phase != servicechat.TeamPhaseFixing || parent.Team.LoopsUsed != 1 {
		t.Fatalf("after FIX: phase=%q loops=%d", parent.Team.Phase, parent.Team.LoopsUsed)
	}
	runs = h.runs.started()
	fix := runs[len(runs)-1]
	if fix.chatID != "parent001" || fix.synthetic != servicechat.SyntheticTeamFix {
		t.Fatalf("fix hop = %+v", fix)
	}
	if fix.prompt != FixPrompt("1. handle the empty cart") {
		t.Errorf("fix prompt lost the findings: %q", fix.prompt)
	}

	h.settle("parent001", prompt.RunOutcome{
		Output: "fixed", Synthetic: servicechat.SyntheticTeamFix,
	})
	if phase := h.parent(t).Team.Phase; phase != servicechat.TeamPhaseReviewing {
		t.Fatalf("phase after the fix round = %q, want a second review", phase)
	}

	h.settleCompanion(h.parent(t).Team.Roles.Reviewer.ChatID, servicechat.SyntheticTeamReview,
		prompt.RunOutcome{Output: "VERDICT: SHIP"})

	parent = h.parent(t)
	if parent.Team.Phase != servicechat.TeamPhaseTesting {
		t.Fatalf("phase = %q, want testing", parent.Team.Phase)
	}
	tester := parent.Team.Roles.Tester
	if tester.ChatID == "" || tester.ChatID == parent.Team.Roles.Reviewer.ChatID {
		t.Fatalf("tester needs a companion chat of its own, got %q", tester.ChatID)
	}

	h.settleCompanion(tester.ChatID, servicechat.SyntheticTeamTest,
		prompt.RunOutcome{Output: "TESTS: PASS\n4 passed"})

	parent = h.parent(t)
	if parent.Team.Phase != servicechat.TeamPhaseDone ||
		parent.Team.Verdict != servicechat.TeamVerdictPass {
		t.Fatalf("closed with phase=%q verdict=%q", parent.Team.Phase, parent.Team.Verdict)
	}

	events := h.events.emitted()
	if len(events) != 1 {
		t.Fatalf("emitted %d closing messages, want exactly one", len(events))
	}
	if events[0].Synthetic != servicechat.SyntheticTeamSummary || events[0].Type != "user" {
		t.Errorf("closing message = %+v", events[0])
	}
	for _, want := range []string{"Codex", "PASS", "1 loop"} {
		if !strings.Contains(events[0].Text, want) {
			t.Errorf("summary %q missing %q", events[0].Text, want)
		}
	}
	if published := h.notifier.published(); len(published) != 1 ||
		published[0].Event != servicenotify.KindRunFinished {
		t.Errorf("published = %+v, want one runFinished", published)
	}

	// Two companions for four hops: the reviewer's chat is reused across
	// loops rather than one chat per pass.
	if len(h.store.created) != 2 {
		t.Errorf("created %d companion chats, want 2", len(h.store.created))
	}
	if len(parent.Team.Hops) != 5 {
		t.Errorf("timeline has %d hops, want 5", len(parent.Team.Hops))
	}
}

func TestDriverBuildsCompanionChatsFromTheParent(t *testing.T) {
	h := newHarness(t, teamParent())

	h.settle("parent001", prompt.RunOutcome{Output: "done"})

	if len(h.store.created) != 1 {
		t.Fatalf("created %d chats", len(h.store.created))
	}
	companion := h.store.created[0]
	if companion.CompanionOf != "parent001" || companion.CompanionRole != servicechat.TeamRoleReviewer {
		t.Fatalf("companion = %+v", companion)
	}
	if companion.ProjectID != "proj1" {
		t.Errorf("companion project = %q, want the parent's", companion.ProjectID)
	}
	if companion.Mode != "review" {
		t.Errorf("companion mode = %q, want review", companion.Mode)
	}
	if companion.Title != "🧐 Reviewer — Ship the checkout flow" {
		t.Errorf("companion title = %q", companion.Title)
	}
	names := skillNames(companion.SelectedSkills)
	if !contains(names, "review-protocol") || !contains(names, "clean-code-guard") {
		t.Errorf("companion skills = %v", names)
	}
}

// A hop must never race the one-run-per-chat invariant. When the target is
// busy the hop waits, and the next settled run on that chat is its retry.
func TestDriverQueuesAHopWhileTheTargetIsBusy(t *testing.T) {
	h := newHarness(t, teamParent())
	h.settle("parent001", prompt.RunOutcome{Output: "done"})

	reviewChat := h.parent(t).Team.Roles.Reviewer.ChatID
	// A human opened the reviewer's chat and typed into it.
	h.runs.setRunning("parent001", true)

	h.settleCompanion(reviewChat, servicechat.SyntheticTeamReview,
		prompt.RunOutcome{Output: "VERDICT: FIX\nnull deref"})

	started := h.runs.started()
	if len(started) != 1 {
		t.Fatalf("started %d runs, want the fix hop to be held back", len(started))
	}
	if phase := h.parent(t).Team.Phase; phase != servicechat.TeamPhaseFixing {
		t.Errorf("phase = %q, want the loop to have recorded the fix", phase)
	}

	// Their run settles; ours goes next.
	h.runs.setRunning("parent001", false)
	h.settle("parent001", prompt.RunOutcome{Output: "human answer", Synthetic: servicechat.SyntheticAutopilot})

	started = h.runs.started()
	if len(started) != 2 {
		t.Fatalf("started %d runs, want the queued fix hop to have run", len(started))
	}
	if started[1].synthetic != servicechat.SyntheticTeamFix || started[1].chatID != "parent001" {
		t.Errorf("queued hop = %+v", started[1])
	}
}

func TestDriverStopsWhenACompanionRunFails(t *testing.T) {
	h := newHarness(t, teamParent())
	h.settle("parent001", prompt.RunOutcome{Output: "done"})
	reviewChat := h.parent(t).Team.Roles.Reviewer.ChatID

	h.settleCompanion(reviewChat, servicechat.SyntheticTeamReview, prompt.RunOutcome{Cancelled: true})

	parent := h.parent(t)
	if parent.Team.Phase != servicechat.TeamPhaseError {
		t.Fatalf("phase = %q, want error", parent.Team.Phase)
	}
	if len(h.runs.started()) != 1 {
		t.Errorf("a failed hop must not start another run")
	}
	if events := h.events.emitted(); len(events) != 1 ||
		!strings.Contains(events[0].Text, "did not finish cleanly") {
		t.Errorf("closing message = %+v", events)
	}
}

// Silence is not consent: a reviewer that ignored the marker instruction stops
// the loop instead of being read as SHIP.
func TestDriverStopsWhenTheReviewerGivesNoVerdict(t *testing.T) {
	h := newHarness(t, teamParent())
	h.settle("parent001", prompt.RunOutcome{Output: "done"})
	reviewChat := h.parent(t).Team.Roles.Reviewer.ChatID

	h.settleCompanion(reviewChat, servicechat.SyntheticTeamReview,
		prompt.RunOutcome{Output: "Looks fine to me, nice work."})

	if phase := h.parent(t).Team.Phase; phase != servicechat.TeamPhaseError {
		t.Fatalf("phase = %q, want error", phase)
	}
	if events := h.events.emitted(); len(events) != 1 ||
		!strings.Contains(events[0].Text, "verdict line") {
		t.Errorf("closing message = %+v", events)
	}
}

func TestDriverIgnoresChatsWithoutTeamMode(t *testing.T) {
	parent := teamParent()
	parent.Team.Enabled = false
	h := newHarness(t, parent)

	h.settle("parent001", prompt.RunOutcome{Output: "done"})

	if started := h.runs.started(); len(started) != 0 {
		t.Fatalf("started %+v on a chat with team mode off", started)
	}
	if len(h.store.created) != 0 {
		t.Errorf("created a companion chat for a chat with team mode off")
	}
}

// A companion chat is an ordinary chat the operator can open and type into.
// A question they ask the reviewer must not be mistaken for a verdict — and
// must not abort the loop for lacking one.
func TestDriverIgnoresAHumanPromptInACompanionChat(t *testing.T) {
	h := newHarness(t, teamParent())
	h.settle("parent001", prompt.RunOutcome{Output: "done"})
	reviewChat := h.parent(t).Team.Roles.Reviewer.ChatID

	h.settle(reviewChat, prompt.RunOutcome{Output: "Sure — the retry lives in queue.go."})

	parent := h.parent(t)
	if parent.Team.Phase != servicechat.TeamPhaseReviewing {
		t.Fatalf("phase = %q, want the review still in flight", parent.Team.Phase)
	}
	if events := h.events.emitted(); len(events) != 0 {
		t.Errorf("emitted %+v, want the loop left alone", events)
	}
	if started := h.runs.started(); len(started) != 1 {
		t.Errorf("started %d runs, want only the review hop", len(started))
	}
}
