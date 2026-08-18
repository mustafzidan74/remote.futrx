package postrun

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicenotify "github.com/futrx-com/remote.futrx.com/internal/service/notify"
	"github.com/futrx-com/remote.futrx.com/internal/service/prompt"
)

type fakeChats struct {
	mu   sync.Mutex
	meta servicechat.Meta
	err  error
}

func (c *fakeChats) Get(context.Context, servicechat.ID) (servicechat.Meta, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.meta, c.err
}

func (c *fakeChats) Update(
	_ context.Context,
	_ servicechat.ID,
	fn func(*servicechat.Meta),
) (servicechat.Meta, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return servicechat.Meta{}, c.err
	}
	fn(&c.meta)
	return c.meta, nil
}

func (c *fakeChats) snapshot() servicechat.Meta {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.meta
}

type fakeStarter struct {
	mu      sync.Mutex
	started []prompt.StartInput
	err     error
}

func (s *fakeStarter) Start(
	input prompt.StartInput,
	_ func(servicechat.Event),
) (prompt.RunHandle, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return prompt.RunHandle{}, s.err
	}
	s.started = append(s.started, input)
	return prompt.RunHandle{ID: uint64(len(s.started))}, nil
}

func (s *fakeStarter) calls() []prompt.StartInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]prompt.StartInput(nil), s.started...)
}

type fakeRuns struct{ running bool }

func (r fakeRuns) IsRunning(servicechat.ID) bool { return r.running }

type fakeSchedules struct{ owned bool }

func (s fakeSchedules) HasTasksForChat(context.Context, servicechat.ID) bool { return s.owned }

type fakeNotifier struct {
	mu     sync.Mutex
	events []servicenotify.Event
}

func (n *fakeNotifier) PublishChatEvent(
	_ context.Context,
	_ servicechat.ID,
	event servicenotify.Event,
) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.events = append(n.events, event)
}

func (n *fakeNotifier) published() []servicenotify.Event {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]servicenotify.Event(nil), n.events...)
}

type harness struct {
	driver    *Driver
	chats     *fakeChats
	starter   *fakeStarter
	notifier  *fakeNotifier
	slept     []time.Duration
	sleptLock sync.Mutex
}

func newHarness(t *testing.T, meta servicechat.Meta, conditions Conditions) *harness {
	t.Helper()
	h := &harness{
		chats:    &fakeChats{meta: meta},
		starter:  &fakeStarter{},
		notifier: &fakeNotifier{},
	}
	now := conditions.Now
	if now.IsZero() {
		now = armedAt.Add(time.Minute)
	}
	h.driver = New(
		Dependencies{
			Chats:     h.chats,
			Runs:      fakeRuns{running: conditions.RunActive},
			Starter:   h.starter,
			Schedules: fakeSchedules{owned: conditions.ScheduledChat},
			Notifier:  h.notifier,
		},
		WithClock(func() time.Time { return now }),
		WithDelay(func(d time.Duration) {
			h.sleptLock.Lock()
			h.slept = append(h.slept, d)
			h.sleptLock.Unlock()
		}),
	)
	return h
}

// settle drives one settled run to completion, so assertions never race the
// driver's reaction goroutine.
func (h *harness) settle(outcome prompt.RunOutcome) {
	h.driver.RunSettled(context.Background(), outcome)
	h.driver.Wait()
}

func TestDriverStartsAnAutopilotRound(t *testing.T) {
	h := newHarness(t, autopilotMeta(nil), Conditions{})

	h.settle(prompt.RunOutcome{ChatID: "abcd1234", Output: "step one done"})

	calls := h.starter.calls()
	if len(calls) != 1 {
		t.Fatalf("started %d runs, want 1", len(calls))
	}
	if calls[0].Synthetic != servicechat.SyntheticAutopilot {
		t.Errorf("synthetic = %q, want %q", calls[0].Synthetic, servicechat.SyntheticAutopilot)
	}
	// The follow-up is attributed to the human who armed the loop, not to
	// nobody: that email is what the audit trail and the provider see.
	if calls[0].Actor.Email != "operator@example.com" {
		t.Errorf("actor = %q, want the operator who enabled autopilot", calls[0].Actor.Email)
	}
	if got := h.chats.snapshot().Autopilot.RoundsUsed; got != 1 {
		t.Errorf("roundsUsed = %d, want 1", got)
	}
	h.sleptLock.Lock()
	defer h.sleptLock.Unlock()
	if len(h.slept) != 1 || h.slept[0] != followUpDelay {
		t.Errorf("delays = %v, want one %v pause before the follow-up", h.slept, followUpDelay)
	}
}

func TestDriverStopsOnTheDoneMarker(t *testing.T) {
	h := newHarness(t, autopilotMeta(func(m *servicechat.Meta) { m.Autopilot.RoundsUsed = 3 }), Conditions{})

	h.settle(prompt.RunOutcome{ChatID: "abcd1234", Output: "All green.\n<<DONE>>"})

	if len(h.starter.calls()) != 0 {
		t.Fatalf("started a follow-up after the agent reported completion")
	}
	if h.chats.snapshot().Autopilot.Enabled {
		t.Errorf("autopilot is still armed after the done marker")
	}
	events := h.notifier.published()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	if events[0].Event != servicenotify.KindRunFinished {
		t.Errorf("event kind = %q, want %q", events[0].Event, servicenotify.KindRunFinished)
	}
	if events[0].Summary != "Autopilot finished after 3 rounds — the agent reported the task complete." {
		t.Errorf("summary = %q", events[0].Summary)
	}
}

func TestDriverReportsABlockedAgentAsNeedingAttention(t *testing.T) {
	h := newHarness(t, autopilotMeta(nil), Conditions{})

	h.settle(prompt.RunOutcome{
		ChatID: "abcd1234",
		Output: "<<BLOCKED: the staging database is unreachable>>",
	})

	events := h.notifier.published()
	if len(events) != 1 {
		t.Fatalf("published %d events, want 1", len(events))
	}
	if events[0].Event != servicenotify.KindNeedsAttention {
		t.Errorf("event kind = %q, want %q", events[0].Event, servicenotify.KindNeedsAttention)
	}
	if events[0].Status != servicenotify.StatusWaiting {
		t.Errorf("status = %q, want %q", events[0].Status, servicenotify.StatusWaiting)
	}
	if want := "the staging database is unreachable"; !strings.Contains(events[0].Summary, want) {
		t.Errorf("summary %q does not carry the agent's reason %q", events[0].Summary, want)
	}
}

func TestDriverStopsOnAFailedRun(t *testing.T) {
	h := newHarness(t, autopilotMeta(nil), Conditions{})

	h.settle(prompt.RunOutcome{ChatID: "abcd1234", Err: errors.New("provider limit reached")})

	if len(h.starter.calls()) != 0 {
		t.Fatalf("started a follow-up after a failed run")
	}
	if h.chats.snapshot().Autopilot.Enabled {
		t.Errorf("autopilot is still armed after a failure")
	}
}

func TestDriverStopsOnACancelledRun(t *testing.T) {
	h := newHarness(t, autopilotMeta(nil), Conditions{})

	h.settle(prompt.RunOutcome{ChatID: "abcd1234", Cancelled: true})

	if len(h.starter.calls()) != 0 {
		t.Fatalf("started a follow-up after a cancelled run")
	}
	if h.chats.snapshot().Autopilot.Enabled {
		t.Errorf("autopilot is still armed after a cancellation")
	}
}

func TestDriverDoesNothingForAnUnarmedChat(t *testing.T) {
	h := newHarness(t, servicechat.Meta{ID: "abcd1234"}, Conditions{})

	h.settle(prompt.RunOutcome{ChatID: "abcd1234", Output: "done"})

	if len(h.starter.calls()) != 0 {
		t.Fatalf("started a follow-up in a chat with no policy")
	}
	if len(h.notifier.published()) != 0 {
		t.Fatalf("published a notification for a chat with no policy")
	}
}

func TestDriverYieldsToARunThatStartedDuringTheDelay(t *testing.T) {
	h := newHarness(t, autopilotMeta(nil), Conditions{RunActive: true})

	h.settle(prompt.RunOutcome{ChatID: "abcd1234", Output: "still going"})

	if len(h.starter.calls()) != 0 {
		t.Fatalf("started a follow-up while another run held the chat")
	}
	if got := h.chats.snapshot().Autopilot.RoundsUsed; got != 0 {
		t.Errorf("roundsUsed = %d, want 0 — a deferred decision must not spend budget", got)
	}
}

func TestDriverStopsWhenTheFollowUpCannotStart(t *testing.T) {
	h := newHarness(t, autopilotMeta(nil), Conditions{})
	h.starter.err = errors.New("a previous prompt is still running")

	h.settle(prompt.RunOutcome{ChatID: "abcd1234", Output: "still going"})

	if h.chats.snapshot().Autopilot.Enabled {
		t.Errorf("autopilot stayed armed after its follow-up failed to start")
	}
	if len(h.notifier.published()) != 1 {
		t.Errorf("published %d events, want a stop notification", len(h.notifier.published()))
	}
}

func TestDriverRunsTheTestBeforeTheNextRound(t *testing.T) {
	meta := autopilotMeta(func(m *servicechat.Meta) {
		m.AutoTest = servicechat.AutoTestPolicy{Enabled: true, EnabledBy: "operator@example.com"}
	})
	h := newHarness(t, meta, Conditions{})

	h.settle(prompt.RunOutcome{ChatID: "abcd1234", Output: "wired the button up"})
	calls := h.starter.calls()
	if len(calls) != 1 || calls[0].Synthetic != servicechat.SyntheticAutoTest {
		t.Fatalf("first follow-up = %+v, want an auto-test run", calls)
	}
	if got := h.chats.snapshot().Autopilot.RoundsUsed; got != 0 {
		t.Errorf("roundsUsed = %d, want 0 — a verification pass is not an autopilot round", got)
	}

	// The test run settling is what releases the next autopilot round.
	h.settle(prompt.RunOutcome{
		ChatID:    "abcd1234",
		Output:    "PASS 3/3",
		Synthetic: servicechat.SyntheticAutoTest,
	})
	calls = h.starter.calls()
	if len(calls) != 2 || calls[1].Synthetic != servicechat.SyntheticAutopilot {
		t.Fatalf("second follow-up = %+v, want an autopilot round", calls)
	}
	if got := h.chats.snapshot().Autopilot.RoundsUsed; got != 1 {
		t.Errorf("roundsUsed = %d, want 1", got)
	}
}

func TestDriverLeavesScheduledChatsAlone(t *testing.T) {
	h := newHarness(t, autopilotMeta(nil), Conditions{ScheduledChat: true})

	h.settle(prompt.RunOutcome{ChatID: "abcd1234", Output: "cron turn finished"})

	if len(h.starter.calls()) != 0 {
		t.Fatalf("started a follow-up in a chat the scheduler drives")
	}
}

func TestDriverAttributesAnAutoTestRunToWhoeverArmedIt(t *testing.T) {
	meta := servicechat.Meta{
		ID:       "abcd1234",
		AutoTest: servicechat.AutoTestPolicy{Enabled: true, EnabledBy: "qa@example.com"},
	}
	h := newHarness(t, meta, Conditions{})

	h.settle(prompt.RunOutcome{ChatID: "abcd1234", Output: "changed the form"})

	calls := h.starter.calls()
	if len(calls) != 1 {
		t.Fatalf("started %d runs, want 1", len(calls))
	}
	if calls[0].Actor.Email != "qa@example.com" {
		t.Errorf("actor = %q, want the operator who enabled auto-test", calls[0].Actor.Email)
	}
}
