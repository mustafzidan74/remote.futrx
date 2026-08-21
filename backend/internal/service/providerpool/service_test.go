package providerpool

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"
)

/* ------------------------------------------------------------------ *
 * Test doubles
 * ------------------------------------------------------------------ */

// memoryStore is the registry store, in memory.
type memoryStore struct {
	mu       sync.Mutex
	registry Registry
	saves    int
	loadErr  error
	saveErr  error
}

func (s *memoryStore) Load(context.Context) (Registry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registry, s.loadErr
}

func (s *memoryStore) Save(_ context.Context, registry Registry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.registry = registry
	s.saves++
	return nil
}

// memoryLog is the usage ledger, in memory.
type memoryLog struct {
	mu      sync.Mutex
	records []UsageRecord
	seed    []UsageRecord
}

func (l *memoryLog) Append(_ context.Context, record UsageRecord) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records = append(l.records, record)
	return nil
}

func (l *memoryLog) Scan(_ context.Context, _ string, visit func(UsageRecord) bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, record := range l.seed {
		if !visit(record) {
			return nil
		}
	}
	return nil
}

func (l *memoryLog) events(event string) []UsageRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := []UsageRecord{}
	for _, record := range l.records {
		if record.Event == event {
			out = append(out, record)
		}
	}
	return out
}

// scriptedCompleter answers per provider id from a script, so a test can say
// "Groq refuses with a 429 and Cerebras answers" without a socket.
type scriptedCompleter struct {
	mu      sync.Mutex
	replies map[string]scriptedReply
	calls   []Call
}

type scriptedReply struct {
	result CallResult
	err    error
}

func (c *scriptedCompleter) Complete(_ context.Context, call Call) (CallResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, call)
	reply, found := c.replies[providerKeyOf(call)]
	if !found {
		return CallResult{Text: "ok", PromptTokens: 10, CompletionTokens: 5, Status: 200}, nil
	}
	return reply.result, reply.err
}

func (c *scriptedCompleter) called() []Call {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Call, len(c.calls))
	copy(out, c.calls)
	return out
}

// providerKeyOf identifies which provider a call went to. The scripts key on
// the base URL because that is the one field the pool copies straight through.
func providerKeyOf(call Call) string { return call.BaseURL }

// staticSecrets is the Secrets vault, in memory.
type staticSecrets map[string]string

func (s staticSecrets) Value(_ context.Context, key string) (string, bool, error) {
	value, found := s[key]
	return value, found, nil
}

/* ------------------------------------------------------------------ *
 * Fixtures
 * ------------------------------------------------------------------ */

// testClock is a hand-wound clock, so cooldown and window tests do not sleep.
type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func newClock() *testClock {
	return &testClock{now: time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)}
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) advance(by time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(by)
}

func provider(id string, priority int, options ...func(*Provider)) Provider {
	p := Provider{
		ID:       id,
		Label:    id,
		Kind:     KindOpenAI,
		BaseURL:  "https://" + id + ".example.com/v1",
		APIKey:   "sk-" + id,
		Priority: priority,
		Enabled:  true,
		Models: []Model{{
			ID:            id + "-model",
			ContextTokens: 128000,
			GoodFor:       []Capability{CapabilityText, CapabilityCode, CapabilityBulk},
		}},
	}
	for _, option := range options {
		option(&p)
	}
	return p.Normalize()
}

// newTestService builds a service over the given registry with a hand-wound
// clock and in-memory everything.
func newTestService(
	t *testing.T,
	providers []Provider,
	settings Settings,
	completer Completer,
	clock *testClock,
) (*Service, *memoryStore, *memoryLog) {
	t.Helper()
	store := &memoryStore{registry: Registry{Providers: providers, Settings: settings, Seeded: true}}
	ledger := &memoryLog{}
	service := New(context.Background(), store,
		WithCompleter(completer),
		WithUsageLog(ledger),
		WithClock(clock.Now),
	)
	return service, store, ledger
}

func autoSwitch() Settings { return Settings{AutoSwitch: true} }

/* ------------------------------------------------------------------ *
 * Picking and failover order
 * ------------------------------------------------------------------ */

func TestPickWalksPriorityOrderAndSkipsWhatCannotAnswer(t *testing.T) {
	clock := newClock()
	tests := []struct {
		name      string
		providers []Provider
		settings  Settings
		need      Need
		want      string
		wantErr   error
	}{
		{
			name:      "the lowest priority number wins",
			providers: []Provider{provider("second", 20), provider("first", 10)},
			settings:  autoSwitch(),
			want:      "first",
		},
		{
			name: "a disabled provider is skipped",
			providers: []Provider{
				provider("first", 10, func(p *Provider) { p.Enabled = false }),
				provider("second", 20),
			},
			settings: autoSwitch(),
			want:     "second",
		},
		{
			name: "a provider with no credential is skipped",
			providers: []Provider{
				provider("first", 10, func(p *Provider) { p.APIKey = "" }),
				provider("second", 20),
			},
			settings: autoSwitch(),
			want:     "second",
		},
		{
			name: "a provider whose models cannot do the job is skipped",
			providers: []Provider{
				provider("first", 10, func(p *Provider) {
					p.Models = []Model{{ID: "text-only", GoodFor: []Capability{CapabilityText}}}
				}),
				provider("second", 20),
			},
			settings: autoSwitch(),
			need:     Need{Want: CapabilityBulk},
			want:     "second",
		},
		{
			name: "a provider whose context window is too small is skipped",
			providers: []Provider{
				provider("first", 10, func(p *Provider) {
					p.Models = []Model{{ID: "small", ContextTokens: 8192}}
				}),
				provider("second", 20),
			},
			settings: autoSwitch(),
			need:     Need{MinContext: 100000},
			want:     "second",
		},
		{
			name:      "an explicit provider id pins the choice past the priority order",
			providers: []Provider{provider("first", 10), provider("second", 20)},
			settings:  autoSwitch(),
			need:      Need{ProviderID: "second"},
			want:      "second",
		},
		{
			name:      "manual mode uses the preferred provider, not the first one",
			providers: []Provider{provider("first", 10), provider("second", 20)},
			settings:  Settings{AutoSwitch: false, PreferredProviderID: "second"},
			want:      "second",
		},
		{
			name:      "manual mode with nothing chosen declines everything",
			providers: []Provider{provider("first", 10)},
			settings:  Settings{AutoSwitch: false},
			wantErr:   ErrNoProvider,
		},
		{
			name:      "an empty pool declines rather than panicking",
			providers: nil,
			settings:  autoSwitch(),
			wantErr:   ErrNoProvider,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, _, _ := newTestService(t, test.providers, test.settings, &scriptedCompleter{}, clock)
			chosen, model, err := service.Pick(context.Background(), test.need)
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("Pick() error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Pick() = %v", err)
			}
			if chosen.ID != test.want {
				t.Fatalf("Pick() chose %q, want %q", chosen.ID, test.want)
			}
			if model.ID == "" {
				t.Fatal("Pick() returned no model")
			}
		})
	}
}

func TestPickPrefersANamedModelWhenTheProviderHasIt(t *testing.T) {
	clock := newClock()
	only := provider("first", 10, func(p *Provider) {
		p.Models = []Model{
			{ID: "cheap", GoodFor: []Capability{CapabilityBulk}},
			{ID: "smart", GoodFor: []Capability{CapabilityCode}},
		}
	})
	service, _, _ := newTestService(t, []Provider{only}, autoSwitch(), &scriptedCompleter{}, clock)

	_, model, err := service.Pick(context.Background(), Need{PreferModel: "smart", Want: CapabilityBulk})
	if err != nil {
		t.Fatalf("Pick() = %v", err)
	}
	if model.ID != "smart" {
		t.Fatalf("model = %q, want the preferred one even though the capability pointed elsewhere", model.ID)
	}

	// A preferred model the provider does not have is a hint, not a demand:
	// the capability rule takes over rather than the pool refusing the job.
	_, model, err = service.Pick(context.Background(), Need{PreferModel: "nonexistent", Want: CapabilityBulk})
	if err != nil {
		t.Fatalf("Pick() = %v", err)
	}
	if model.ID != "cheap" {
		t.Fatalf("model = %q, want the capability match after an unknown preference", model.ID)
	}
}

/* ------------------------------------------------------------------ *
 * Failover
 * ------------------------------------------------------------------ */

func quotaError() error {
	header := http.Header{}
	header.Set("retry-after", "30")
	return &CallError{Status: http.StatusTooManyRequests, Header: header, Message: "rate limit reached"}
}

func TestCompleteMovesToTheNextProviderWhenAQuotaRunsOut(t *testing.T) {
	clock := newClock()
	first := provider("first", 10)
	second := provider("second", 20)
	completer := &scriptedCompleter{replies: map[string]scriptedReply{
		first.BaseURL: {err: quotaError()},
	}}
	service, _, ledger := newTestService(t,
		[]Provider{first, second}, autoSwitch(), completer, clock)

	result, err := service.Complete(context.Background(), Request{
		Need:   Need{Job: "chatTitle"},
		Prompt: "name this chat",
	})
	if err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	if result.ProviderID != "second" {
		t.Fatalf("answer came from %q, want the second provider after the first ran out", result.ProviderID)
	}
	if result.Failovers != 1 {
		t.Fatalf("Failovers = %d, want 1", result.Failovers)
	}
	if calls := completer.called(); len(calls) != 2 {
		t.Fatalf("made %d calls, want one per provider", len(calls))
	}

	failovers := ledger.events(EventFailover)
	if len(failovers) != 1 {
		t.Fatalf("recorded %d failovers, want 1", len(failovers))
	}
	if failovers[0].ProviderID != "first" || failovers[0].NextProviderID != "second" {
		t.Fatalf("failover record = %+v, want first -> second", failovers[0])
	}
}

func TestCompleteReportsFailureOnlyWhenEveryProviderRefuses(t *testing.T) {
	clock := newClock()
	first := provider("first", 10)
	second := provider("second", 20)
	completer := &scriptedCompleter{replies: map[string]scriptedReply{
		first.BaseURL:  {err: quotaError()},
		second.BaseURL: {err: quotaError()},
	}}
	service, _, ledger := newTestService(t,
		[]Provider{first, second}, autoSwitch(), completer, clock)

	if _, err := service.Complete(context.Background(), Request{Prompt: "hello"}); !errors.Is(err, ErrNoProvider) {
		t.Fatalf("Complete() error = %v, want ErrNoProvider", err)
	}
	// The last failover names no successor, which is how the ledger records
	// "and then it ran out of options".
	failovers := ledger.events(EventFailover)
	if len(failovers) != 2 {
		t.Fatalf("recorded %d failovers, want one per refusal", len(failovers))
	}
	if failovers[1].NextProviderID != "" {
		t.Fatalf("the last failover pointed at %q, want nothing", failovers[1].NextProviderID)
	}
}

func TestCompleteStopsAfterTheFailoverCeiling(t *testing.T) {
	clock := newClock()
	providers := []Provider{
		provider("one", 10), provider("two", 20), provider("three", 30),
		provider("four", 40), provider("five", 50),
	}
	replies := map[string]scriptedReply{}
	for _, p := range providers {
		replies[p.BaseURL] = scriptedReply{err: quotaError()}
	}
	completer := &scriptedCompleter{replies: replies}
	service, _, _ := newTestService(t, providers, autoSwitch(), completer, clock)

	if _, err := service.Complete(context.Background(), Request{Prompt: "hello"}); err == nil {
		t.Fatal("Complete() = nil, want a failure")
	}
	if calls := len(completer.called()); calls != MaxFailovers {
		t.Fatalf("tried %d providers, want the ceiling of %d — walking every dead provider is a stall", calls, MaxFailovers)
	}
}

func TestManualModeNeverFailsOver(t *testing.T) {
	clock := newClock()
	first := provider("first", 10)
	second := provider("second", 20)
	completer := &scriptedCompleter{replies: map[string]scriptedReply{
		first.BaseURL: {err: quotaError()},
	}}
	service, _, _ := newTestService(t, []Provider{first, second},
		Settings{AutoSwitch: false, PreferredProviderID: "first"}, completer, clock)

	if _, err := service.Complete(context.Background(), Request{Prompt: "hello"}); err == nil {
		t.Fatal("Complete() = nil, want the pinned provider's failure to stand")
	}
	if calls := completer.called(); len(calls) != 1 {
		t.Fatalf("made %d calls; \"do not switch\" has to mean it", len(calls))
	}
}

/* ------------------------------------------------------------------ *
 * Cooldown state machine
 * ------------------------------------------------------------------ */

func TestCooldownBacksOffExponentiallyAndClearsOnSuccess(t *testing.T) {
	tests := []struct {
		name     string
		failures int
		want     time.Duration
	}{
		{name: "the first refusal is a short nap", failures: 1, want: 30 * time.Second},
		{name: "the second is long enough for a minute window to roll", failures: 2, want: 5 * time.Minute},
		{name: "the third is the ceiling", failures: 3, want: 30 * time.Minute},
		{name: "past the ceiling it stays there rather than growing forever", failures: 9, want: 30 * time.Minute},
		{name: "a nonsense count still yields the first step", failures: 0, want: 30 * time.Second},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := cooldownFor(test.failures); got != test.want {
				t.Fatalf("cooldownFor(%d) = %s, want %s", test.failures, got, test.want)
			}
		})
	}
}

func TestACoolingProviderIsSkippedUntilItsWindowPasses(t *testing.T) {
	clock := newClock()
	first := provider("first", 10)
	second := provider("second", 20)
	completer := &scriptedCompleter{replies: map[string]scriptedReply{
		first.BaseURL: {err: quotaError()},
	}}
	service, _, _ := newTestService(t, []Provider{first, second}, autoSwitch(), completer, clock)

	// One refusal puts the first provider to sleep.
	if _, err := service.Complete(context.Background(), Request{Prompt: "one"}); err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	if cooling, _ := service.tracker.cooling("first"); !cooling {
		t.Fatal("a refusing provider was not put to sleep")
	}

	// While it sleeps it is not even offered.
	chosen, _, err := service.Pick(context.Background(), Need{})
	if err != nil {
		t.Fatalf("Pick() = %v", err)
	}
	if chosen.ID != "first" {
		// A retry-after of 30s means the nap is 30s, so it is still asleep.
		t.Logf("picked %q while the first provider sleeps", chosen.ID)
	}
	if chosen.ID == "first" {
		t.Fatal("a sleeping provider was offered again immediately")
	}

	// Past the window it wakes up on its own, with no timer and no goroutine.
	clock.advance(31 * time.Second)
	chosen, _, err = service.Pick(context.Background(), Need{})
	if err != nil {
		t.Fatalf("Pick() after the cooldown = %v", err)
	}
	if chosen.ID != "first" {
		t.Fatalf("after the cooldown the pool picked %q, want the recovered provider back at the front", chosen.ID)
	}

	// And a success clears the failure count, so the next refusal starts the
	// back-off from the beginning rather than from where it left off.
	completer.mu.Lock()
	completer.replies = map[string]scriptedReply{}
	completer.mu.Unlock()
	if _, err := service.Complete(context.Background(), Request{Prompt: "two"}); err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	entry := service.tracker.snapshot("first")
	if entry.failures != 0 {
		t.Fatalf("failures = %d after a success, want the back-off reset", entry.failures)
	}
}

func TestAVendorsRetryAfterOverridesAShorterCooldown(t *testing.T) {
	clock := newClock()
	only := provider("only", 10)
	header := http.Header{}
	header.Set("retry-after", "600")
	completer := &scriptedCompleter{replies: map[string]scriptedReply{
		only.BaseURL: {err: &CallError{Status: 429, Header: header, Message: "slow down"}},
	}}
	service, _, _ := newTestService(t, []Provider{only}, autoSwitch(), completer, clock)

	if _, err := service.Complete(context.Background(), Request{Prompt: "hello"}); err == nil {
		t.Fatal("Complete() = nil, want the refusal")
	}
	// Our own first step is 30s. The vendor said ten minutes, and the vendor
	// knows when its window opens.
	clock.advance(5 * time.Minute)
	if cooling, _ := service.tracker.cooling("only"); !cooling {
		t.Fatal("the pool woke a provider before the vendor said its window reopens")
	}
	clock.advance(6 * time.Minute)
	if cooling, _ := service.tracker.cooling("only"); cooling {
		t.Fatal("the provider never woke up after the vendor's own window")
	}
}

/* ------------------------------------------------------------------ *
 * Usage accounting
 * ------------------------------------------------------------------ */

func TestASuccessfulCallIsCountedAgainstEveryWindow(t *testing.T) {
	clock := newClock()
	only := provider("only", 10, func(p *Provider) {
		p.Limits = Limits{RPM: intp(10), RPD: intp(100), TPD: intp(1000), MonthlyTokens: intp(5000)}
	})
	completer := &scriptedCompleter{replies: map[string]scriptedReply{
		only.BaseURL: {result: CallResult{
			Text: "done", PromptTokens: 30, CompletionTokens: 12, Status: 200, TokenSource: SourceReported,
		}},
	}}
	service, _, ledger := newTestService(t, []Provider{only}, autoSwitch(), completer, clock)

	if _, err := service.Complete(context.Background(), Request{Need: Need{Job: "bulk"}, Prompt: "write"}); err != nil {
		t.Fatalf("Complete() = %v", err)
	}

	view := service.View().Providers[0]
	if view.Usage.RequestsToday.Used != 1 {
		t.Fatalf("requests today = %d, want 1", view.Usage.RequestsToday.Used)
	}
	if view.Usage.TokensToday.Used != 42 {
		t.Fatalf("tokens today = %d, want the 42 the provider reported", view.Usage.TokensToday.Used)
	}
	if view.Usage.TokensMonth.Used != 42 {
		t.Fatalf("tokens this month = %d, want 42", view.Usage.TokensMonth.Used)
	}
	if view.Status != StatusReady {
		t.Fatalf("status = %q, want ready", view.Status)
	}
	requests := ledger.events(EventRequest)
	if len(requests) != 1 || !requests[0].OK || requests[0].Job != "bulk" {
		t.Fatalf("ledger = %+v, want one successful bulk request", requests)
	}
}

func TestAProviderOverACountedLimitIsSkippedButStillShown(t *testing.T) {
	clock := newClock()
	spent := provider("spent", 10, func(p *Provider) { p.Limits = Limits{RPD: intp(1)} })
	spare := provider("spare", 20)
	service, _, _ := newTestService(t, []Provider{spent, spare}, autoSwitch(), &scriptedCompleter{}, clock)

	if _, err := service.Complete(context.Background(), Request{Prompt: "one"}); err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	// That single request used the whole documented daily allowance.
	chosen, _, err := service.Pick(context.Background(), Need{})
	if err != nil {
		t.Fatalf("Pick() = %v", err)
	}
	if chosen.ID != "spare" {
		t.Fatalf("Pick() chose %q, want the provider that has quota left", chosen.ID)
	}
	view := service.View().Providers[0]
	if view.ID != "spent" || view.Status != StatusExhausted {
		t.Fatalf("view = %+v, want the exhausted provider still listed with its status", view)
	}
}

func TestAWindowWithNoDocumentedLimitCanNeverExhaust(t *testing.T) {
	clock := newClock()
	// Zhipu and Moonshot ship with no documented caps at all. A provider like
	// that must stay usable rather than being compared against an invented
	// number.
	unlimited := provider("unlimited", 10, func(p *Provider) { p.Limits = Limits{} })
	service, _, _ := newTestService(t, []Provider{unlimited}, autoSwitch(), &scriptedCompleter{}, clock)

	for i := 0; i < 50; i++ {
		if _, err := service.Complete(context.Background(), Request{Prompt: "spend"}); err != nil {
			t.Fatalf("Complete() #%d = %v", i, err)
		}
	}
	view := service.View().Providers[0]
	if view.Status != StatusReady {
		t.Fatalf("status = %q after 50 calls with no documented cap, want ready", view.Status)
	}
	if view.Usage.RequestsToday.Limit != nil || view.Usage.RequestsToday.Percent != nil {
		t.Fatalf("meter = %+v, want no limit and no percentage to show", view.Usage.RequestsToday)
	}
	if view.Usage.RequestsToday.Used != 50 {
		t.Fatalf("used = %d, want the raw count even with nothing to compare it to", view.Usage.RequestsToday.Used)
	}
}

func TestReportedHeadersWinOverLocalCounting(t *testing.T) {
	clock := newClock()
	header := http.Header{}
	header.Set("x-ratelimit-limit-requests", "100")
	header.Set("x-ratelimit-remaining-requests", "40")
	only := provider("only", 10, func(p *Provider) { p.Limits = Limits{RPM: intp(100)} })
	completer := &scriptedCompleter{replies: map[string]scriptedReply{
		only.BaseURL: {result: CallResult{Text: "ok", Status: 200, Header: header}},
	}}
	service, _, _ := newTestService(t, []Provider{only}, autoSwitch(), completer, clock)

	if _, err := service.Complete(context.Background(), Request{Prompt: "hello"}); err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	meter := service.View().Providers[0].Usage.RequestsMinute
	if meter.Source != SourceReported {
		t.Fatalf("source = %q, want the provider's own number to be labelled as theirs", meter.Source)
	}
	if meter.Used != 60 {
		t.Fatalf("used = %d, want 100-40 from the headers rather than our count of 1", meter.Used)
	}
}

func TestAStaleHeaderReadingIsDroppedRatherThanShown(t *testing.T) {
	clock := newClock()
	header := http.Header{}
	header.Set("x-ratelimit-limit-requests", "100")
	header.Set("x-ratelimit-remaining-requests", "0")
	only := provider("only", 10, func(p *Provider) { p.Limits = Limits{RPM: intp(100)} })
	completer := &scriptedCompleter{replies: map[string]scriptedReply{
		only.BaseURL: {result: CallResult{Text: "ok", Status: 200, Header: header}},
	}}
	service, _, _ := newTestService(t, []Provider{only}, autoSwitch(), completer, clock)

	if _, err := service.Complete(context.Background(), Request{Prompt: "hello"}); err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	// An hour later, "nothing left" describes a window that closed long ago.
	clock.advance(time.Hour)
	meter := service.View().Providers[0].Usage.RequestsMinute
	if meter.Source != SourceCounted {
		t.Fatalf("source = %q, want the stale reading dropped back to local counting", meter.Source)
	}
}

/* ------------------------------------------------------------------ *
 * Window rollover
 * ------------------------------------------------------------------ */

func TestCountersRollOverAtTheMinuteDayAndMonthBoundaries(t *testing.T) {
	tests := []struct {
		name        string
		advance     time.Duration
		wantMinute  int
		wantDay     int
		wantMonth   int
		description string
	}{
		{
			name:       "inside the same minute nothing rolls",
			advance:    10 * time.Second,
			wantMinute: 1, wantDay: 1, wantMonth: 1,
		},
		{
			name:       "a new minute resets only the minute window",
			advance:    2 * time.Minute,
			wantMinute: 0, wantDay: 1, wantMonth: 1,
		},
		{
			name:       "a new day resets the minute and the day",
			advance:    25 * time.Hour,
			wantMinute: 0, wantDay: 0, wantMonth: 1,
		},
		{
			name:       "a new month resets all three",
			advance:    32 * 24 * time.Hour,
			wantMinute: 0, wantDay: 0, wantMonth: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			clock := newClock()
			only := provider("only", 10)
			service, _, _ := newTestService(t, []Provider{only}, autoSwitch(), &scriptedCompleter{}, clock)
			if _, err := service.Complete(context.Background(), Request{Prompt: "one"}); err != nil {
				t.Fatalf("Complete() = %v", err)
			}

			clock.advance(test.advance)
			usage := service.View().Providers[0].Usage
			if usage.RequestsMinute.Used != test.wantMinute {
				t.Fatalf("minute requests = %d, want %d", usage.RequestsMinute.Used, test.wantMinute)
			}
			if usage.RequestsToday.Used != test.wantDay {
				t.Fatalf("day requests = %d, want %d", usage.RequestsToday.Used, test.wantDay)
			}
			if usage.TokensMonth.Used > 0 != (test.wantMonth > 0) {
				t.Fatalf("month tokens = %d, want the month window %v", usage.TokensMonth.Used, test.wantMonth > 0)
			}
		})
	}
}

func TestARestartRebuildsTodayAndThisMonthFromTheLedger(t *testing.T) {
	clock := newClock()
	only := provider("only", 10, func(p *Provider) { p.Limits = Limits{RPD: intp(100), MonthlyTokens: intp(10000)} })
	store := &memoryStore{registry: Registry{Providers: []Provider{only}, Settings: autoSwitch(), Seeded: true}}
	ledger := &memoryLog{seed: []UsageRecord{
		// Earlier today.
		{At: clock.Now().Add(-2 * time.Hour).UnixMilli(), Event: EventRequest, ProviderID: "only", OK: true, PromptTokens: 100, CompletionTokens: 50},
		// Earlier this month but not today.
		{At: clock.Now().Add(-10 * 24 * time.Hour).UnixMilli(), Event: EventRequest, ProviderID: "only", OK: true, PromptTokens: 200, CompletionTokens: 100},
		// A failover line is not a request and must not be counted as one.
		{At: clock.Now().Add(-time.Hour).UnixMilli(), Event: EventFailover, ProviderID: "only"},
	}}

	service := New(context.Background(), store,
		WithCompleter(&scriptedCompleter{}), WithUsageLog(ledger), WithClock(clock.Now))

	usage := service.View().Providers[0].Usage
	if usage.RequestsToday.Used != 1 {
		t.Fatalf("requests today = %d, want only the one from today", usage.RequestsToday.Used)
	}
	if usage.TokensToday.Used != 150 {
		t.Fatalf("tokens today = %d, want 150", usage.TokensToday.Used)
	}
	if usage.TokensMonth.Used != 450 {
		t.Fatalf("tokens this month = %d, want both days' 450 — a restart must not hand the operator a comfortable lie", usage.TokensMonth.Used)
	}
}

/* ------------------------------------------------------------------ *
 * Credentials
 * ------------------------------------------------------------------ */

func TestAVaultReferenceResolvesAndNeverLeaksIntoTheView(t *testing.T) {
	clock := newClock()
	only := provider("only", 10, func(p *Provider) {
		p.APIKey = ""
		p.APIKeyRef = "GROQ_API_KEY"
	})
	completer := &scriptedCompleter{}
	store := &memoryStore{registry: Registry{Providers: []Provider{only}, Settings: autoSwitch(), Seeded: true}}
	service := New(context.Background(), store,
		WithCompleter(completer),
		WithUsageLog(&memoryLog{}),
		WithSecrets(staticSecrets{"GROQ_API_KEY": "sk-from-the-vault"}),
		WithClock(clock.Now),
	)

	if _, err := service.Complete(context.Background(), Request{Prompt: "hello"}); err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	calls := completer.called()
	if len(calls) != 1 || calls[0].APIKey != "sk-from-the-vault" {
		t.Fatalf("the vault value never reached the request: %+v", calls)
	}

	view := service.View().Providers[0]
	if view.APIKeyRef != "GROQ_API_KEY" {
		t.Fatalf("apiKeyRef = %q, want the key *name*, which was never secret", view.APIKeyRef)
	}
	if view.APIKeyMasked != "" {
		t.Fatalf("a vault-backed provider showed an inline mask %q", view.APIKeyMasked)
	}
	if view.KeySource != "vault" {
		t.Fatalf("keySource = %q, want vault", view.KeySource)
	}
}

func TestAProviderWhoseVaultKeyDisappearedFailsOverRatherThanCrashing(t *testing.T) {
	clock := newClock()
	broken := provider("broken", 10, func(p *Provider) {
		p.APIKey = ""
		p.APIKeyRef = "MISSING_KEY"
	})
	working := provider("working", 20)
	store := &memoryStore{
		registry: Registry{Providers: []Provider{broken, working}, Settings: autoSwitch(), Seeded: true},
	}
	service := New(context.Background(), store,
		WithCompleter(&scriptedCompleter{}),
		WithUsageLog(&memoryLog{}),
		WithSecrets(staticSecrets{}),
		WithClock(clock.Now),
	)

	result, err := service.Complete(context.Background(), Request{Prompt: "hello"})
	if err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	if result.ProviderID != "working" {
		t.Fatalf("answered by %q, want the failover past the provider with no resolvable key", result.ProviderID)
	}
}

/* ------------------------------------------------------------------ *
 * The bulk lane
 * ------------------------------------------------------------------ */

func TestBulkCapsBothEndsOfTheRequest(t *testing.T) {
	clock := newClock()
	only := provider("only", 10)
	completer := &scriptedCompleter{}
	service, _, _ := newTestService(t, []Provider{only}, autoSwitch(), completer, clock)

	// The completion cap is applied whatever the caller asks for.
	if _, err := service.Bulk(context.Background(), BulkInput{
		Prompt:    "write a product description",
		MaxTokens: 999999,
	}); err != nil {
		t.Fatalf("Bulk() = %v", err)
	}
	calls := completer.called()
	if calls[0].MaxTokens != BulkMaxCompletionTokens {
		t.Fatalf("MaxTokens = %d, want the lane's ceiling of %d", calls[0].MaxTokens, BulkMaxCompletionTokens)
	}

	// The prompt cap refuses rather than silently truncating: a bulk caller
	// that sends half a catalogue should hear about it.
	huge := make([]byte, (BulkMaxPromptTokens+1000)*4)
	for i := range huge {
		huge[i] = 'a'
	}
	if _, err := service.Bulk(context.Background(), BulkInput{Prompt: string(huge)}); !errors.Is(err, ErrPromptTooLarge) {
		t.Fatalf("Bulk() with an oversized prompt = %v, want ErrPromptTooLarge", err)
	}

	if _, err := service.Bulk(context.Background(), BulkInput{Prompt: "   "}); !errors.Is(err, ErrEmptyPrompt) {
		t.Fatalf("Bulk() with an empty prompt = %v, want ErrEmptyPrompt", err)
	}
}

func TestBulkAsksForABulkCapableModel(t *testing.T) {
	clock := newClock()
	codeOnly := provider("code-only", 10, func(p *Provider) {
		p.Models = []Model{{ID: "big", GoodFor: []Capability{CapabilityCode}}}
	})
	bulkReady := provider("bulk-ready", 20, func(p *Provider) {
		p.Models = []Model{{ID: "small", GoodFor: []Capability{CapabilityBulk}}}
	})
	service, _, _ := newTestService(t, []Provider{codeOnly, bulkReady}, autoSwitch(), &scriptedCompleter{}, clock)

	result, err := service.Bulk(context.Background(), BulkInput{Prompt: "describe this product"})
	if err != nil {
		t.Fatalf("Bulk() = %v", err)
	}
	if result.ProviderID != "bulk-ready" {
		t.Fatalf("bulk went to %q, want the provider with a bulk-tagged model", result.ProviderID)
	}
}

/* ------------------------------------------------------------------ *
 * The admin probe
 * ------------------------------------------------------------------ */

func TestTestProbesEvenACoolingProviderAndRevivesItOnSuccess(t *testing.T) {
	clock := newClock()
	only := provider("only", 10)
	completer := &scriptedCompleter{replies: map[string]scriptedReply{
		only.BaseURL: {err: quotaError()},
	}}
	service, _, _ := newTestService(t, []Provider{only}, autoSwitch(), completer, clock)

	if _, err := service.Complete(context.Background(), Request{Prompt: "hello"}); err == nil {
		t.Fatal("Complete() = nil, want the refusal that starts the cooldown")
	}
	if cooling, _ := service.tracker.cooling("only"); !cooling {
		t.Fatal("the provider is not cooling down")
	}

	// The operator fixes whatever was wrong and presses Test.
	completer.mu.Lock()
	completer.replies = map[string]scriptedReply{}
	completer.mu.Unlock()

	result := service.Test(context.Background(), "only")
	if !result.OK {
		t.Fatalf("Test() = %+v, want a successful probe against the sleeping provider", result)
	}
	if cooling, _ := service.tracker.cooling("only"); cooling {
		t.Fatal("a successful probe left the cooldown in place; \"fix it and press Test\" has to work")
	}
}

func TestTestReportsAFailureInTheResultRatherThanAsAnError(t *testing.T) {
	clock := newClock()
	only := provider("only", 10)
	completer := &scriptedCompleter{replies: map[string]scriptedReply{
		only.BaseURL: {err: &CallError{Status: 401, Message: "invalid api key"}},
	}}
	service, _, _ := newTestService(t, []Provider{only}, autoSwitch(), completer, clock)

	result := service.Test(context.Background(), "only")
	if result.OK {
		t.Fatal("Test() reported success against a provider that refused the key")
	}
	if result.Error == "" {
		t.Fatal("Test() dropped the reason, which is the whole point of the button")
	}

	missing := service.Test(context.Background(), "nobody")
	if missing.OK || missing.Error == "" {
		t.Fatalf("Test() on an unknown provider = %+v, want a named failure", missing)
	}
}

/* ------------------------------------------------------------------ *
 * A nil service
 * ------------------------------------------------------------------ */

func TestANilPoolDeclinesEverythingInsteadOfPanicking(t *testing.T) {
	var service *Service
	if service.Available() {
		t.Fatal("a nil pool reported itself available")
	}
	if _, _, err := service.Pick(context.Background(), Need{}); !errors.Is(err, ErrNoProvider) {
		t.Fatalf("Pick() = %v, want ErrNoProvider", err)
	}
	if _, err := service.Complete(context.Background(), Request{Prompt: "hello"}); !errors.Is(err, ErrNoProvider) {
		t.Fatalf("Complete() = %v, want ErrNoProvider", err)
	}
	if view := service.View(); len(view.Providers) != 0 {
		t.Fatalf("View() = %+v, want an empty panel", view)
	}
	if quota := service.Quota(); quota.Available {
		t.Fatal("Quota() on a nil pool reported itself set up")
	}
}
