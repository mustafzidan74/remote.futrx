package auxmodel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// stubCompleter answers whatever the test scripted and counts the calls, so a
// breaker test can prove the endpoint stopped being dialled at all.
type stubCompleter struct {
	mu      sync.Mutex
	calls   int
	answer  string
	err     error
	lastReq Completion
	delay   time.Duration
}

func (s *stubCompleter) Complete(ctx context.Context, req Completion) (string, error) {
	s.mu.Lock()
	s.calls++
	s.lastReq = req
	delay, answer, err := s.delay, s.answer, s.err
	s.mu.Unlock()

	if delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return answer, err
}

func (s *stubCompleter) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// memoryStore is a Store that never touches a disk.
type memoryStore struct {
	config  Config
	loadErr error
	saveErr error
	saves   int
}

func (m *memoryStore) Load(context.Context) (Config, error) {
	if m.loadErr != nil {
		return Config{}, m.loadErr
	}
	return m.config, nil
}

func (m *memoryStore) Save(_ context.Context, cfg Config) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saves++
	m.config = cfg
	return nil
}

func activeConfig() Config {
	cfg := DefaultConfig()
	cfg.Enabled = true
	return cfg
}

func newTestService(t *testing.T, cfg Config, completer Completer, options ...Option) *Service {
	t.Helper()
	all := append([]Option{WithCompleter(completer)}, options...)
	return New(context.Background(), &memoryStore{config: cfg}, all...)
}

func TestCompleteDegradesWhenDisabled(t *testing.T) {
	tests := []struct {
		name   string
		config func() Config
		job    Job
	}{
		{
			name:   "the whole service is off",
			config: DefaultConfig,
			job:    JobChatTitle,
		},
		{
			name: "the service is on but this job's toggle is off",
			config: func() Config {
				cfg := activeConfig()
				cfg.Jobs = JobSettings{JobChatTitle: false}
				return cfg.Normalize()
			},
			job: JobChatTitle,
		},
		{
			name: "the service is on but nothing is configured",
			config: func() Config {
				cfg := activeConfig()
				cfg.BaseURL = ""
				cfg.Model = ""
				cfg.Provider = ProviderOpenAICompatible
				return cfg
			},
			job: JobRunSummary,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			completer := &stubCompleter{answer: "should never be asked"}
			service := newTestService(t, test.config(), completer)

			_, err := service.Complete(context.Background(), test.job, "sys", "user text")
			if !errors.Is(err, ErrDisabled) {
				t.Fatalf("Complete() = %v, want ErrDisabled so the caller falls back", err)
			}
			if completer.count() != 0 {
				t.Fatalf("the endpoint was dialled %d times while disabled", completer.count())
			}
			if service.Available(test.job) {
				t.Fatal("Available() said yes for a job that cannot run")
			}
		})
	}
}

func TestCompleteRefusesEmptyInputWithoutDiallingAnything(t *testing.T) {
	completer := &stubCompleter{answer: "x"}
	service := newTestService(t, activeConfig(), completer)

	if _, err := service.Complete(context.Background(), JobChatTitle, "sys", "   \n  "); !errors.Is(err, ErrEmptyInput) {
		t.Fatalf("Complete() = %v, want ErrEmptyInput", err)
	}
	if completer.count() != 0 {
		t.Fatal("an empty prompt still reached the endpoint")
	}
}

func TestCompleteAppliesThePerJobTokenCapAndTheOperatorCeiling(t *testing.T) {
	tests := []struct {
		name      string
		job       Job
		maxTokens int
		want      int
	}{
		{name: "a title is capped far below the default", job: JobChatTitle, maxTokens: 256, want: 40},
		{name: "a translation gets the room it needs", job: JobTranslate, maxTokens: 4096, want: 1200},
		{name: "the operator ceiling wins when it is lower", job: JobTranslate, maxTokens: 64, want: 64},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cfg := activeConfig()
			cfg.MaxTokens = test.maxTokens
			completer := &stubCompleter{answer: "ok"}
			service := newTestService(t, cfg, completer)

			if _, err := service.Complete(context.Background(), test.job, "sys", "text"); err != nil {
				t.Fatalf("Complete() = %v", err)
			}
			if completer.lastReq.MaxTokens != test.want {
				t.Fatalf("MaxTokens = %d, want %d", completer.lastReq.MaxTokens, test.want)
			}
		})
	}
}

func TestCompleteEnforcesTheHardTimeout(t *testing.T) {
	// A slow endpoint must not hold a caller for longer than the configured
	// timeout, whatever the caller's own context says.
	cfg := activeConfig()
	cfg.TimeoutSeconds = MinTimeoutSeconds
	completer := &stubCompleter{answer: "too late", delay: 10 * time.Second}
	service := newTestService(t, cfg, completer)

	started := time.Now()
	_, err := service.Complete(context.Background(), JobChatTitle, "sys", "text")
	elapsed := time.Since(started)

	if err == nil {
		t.Fatal("Complete() = nil, want the timeout to fail the call")
	}
	if elapsed > time.Duration(MinTimeoutSeconds+2)*time.Second {
		t.Fatalf("Complete() waited %s, well past the %ds timeout", elapsed, MinTimeoutSeconds)
	}
}

func TestCompleteSurvivesAnAlreadyCancelledCallerContext(t *testing.T) {
	// A settled run hands its observers a cancelled context. The job still has
	// to be able to run: the work is *about* the run that just ended.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	completer := &stubCompleter{answer: "Fix the login loop"}
	service := newTestService(t, activeConfig(), completer)

	answer, err := service.Complete(ctx, JobChatSummary, "sys", "the run finished")
	if err != nil {
		t.Fatalf("Complete() = %v, want the job to run on a fresh context", err)
	}
	if answer != "Fix the login loop" {
		t.Fatalf("answer = %q", answer)
	}
}

func TestCircuitBreakerStopsCallingAndReopensAfterTheCooldown(t *testing.T) {
	now := time.Now()
	clock := func() time.Time { return now }
	completer := &stubCompleter{err: errors.New("connection refused")}
	service := newTestService(t, activeConfig(), completer,
		WithClock(clock), WithBreakerCooldown(5*time.Minute))

	for attempt := 1; attempt <= breakerThreshold; attempt++ {
		if _, err := service.Complete(context.Background(), JobChatTitle, "sys", "text"); err == nil {
			t.Fatalf("attempt %d succeeded against a refusing endpoint", attempt)
		}
	}
	if completer.count() != breakerThreshold {
		t.Fatalf("endpoint dialled %d times, want %d before the breaker opened",
			completer.count(), breakerThreshold)
	}

	// Past the threshold nothing is dialled at all.
	for i := 0; i < 5; i++ {
		if _, err := service.Complete(context.Background(), JobChatTitle, "sys", "text"); !errors.Is(err, ErrBreakerOpen) {
			t.Fatalf("Complete() = %v, want ErrBreakerOpen", err)
		}
	}
	if completer.count() != breakerThreshold {
		t.Fatalf("endpoint dialled %d times, want the breaker to have stopped every call",
			completer.count())
	}
	if service.Available(JobChatTitle) {
		t.Fatal("Available() said yes while the breaker is open")
	}

	// The cooldown expires and exactly one call is let through to find out
	// whether the endpoint came back.
	now = now.Add(5*time.Minute + time.Second)
	completer.mu.Lock()
	completer.err = nil
	completer.answer = "back"
	completer.mu.Unlock()

	answer, err := service.Complete(context.Background(), JobChatTitle, "sys", "text")
	if err != nil {
		t.Fatalf("Complete() after the cooldown = %v, want a retry", err)
	}
	if answer != "back" {
		t.Fatalf("answer = %q", answer)
	}
	if completer.count() != breakerThreshold+1 {
		t.Fatalf("endpoint dialled %d times, want exactly one retry", completer.count())
	}
}

func TestASuccessResetsTheFailureCount(t *testing.T) {
	completer := &stubCompleter{err: errors.New("boom")}
	service := newTestService(t, activeConfig(), completer)

	// Two failures, then a success: the third failure must not be the one
	// that opens the breaker, because the run of failures was broken.
	for i := 0; i < breakerThreshold-1; i++ {
		_, _ = service.Complete(context.Background(), JobChatTitle, "sys", "text")
	}
	completer.mu.Lock()
	completer.err = nil
	completer.answer = "fine"
	completer.mu.Unlock()
	if _, err := service.Complete(context.Background(), JobChatTitle, "sys", "text"); err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	completer.mu.Lock()
	completer.err = errors.New("boom")
	completer.answer = ""
	completer.mu.Unlock()
	if _, err := service.Complete(context.Background(), JobChatTitle, "sys", "text"); errors.Is(err, ErrBreakerOpen) {
		t.Fatal("the breaker opened after a run of failures that a success had already broken")
	}
	if !service.Available(JobChatTitle) {
		t.Fatal("the breaker is open after a single failure")
	}
}

func TestSaveValidatesPersistsAndClosesTheBreaker(t *testing.T) {
	store := &memoryStore{config: activeConfig()}
	completer := &stubCompleter{err: errors.New("refused")}
	service := New(context.Background(), store, WithCompleter(completer))

	for i := 0; i < breakerThreshold; i++ {
		_, _ = service.Complete(context.Background(), JobChatTitle, "sys", "text")
	}
	if service.Available(JobChatTitle) {
		t.Fatal("the breaker should be open after the threshold")
	}

	if _, err := service.Save(context.Background(), UpdateInput{
		Enabled:  true,
		Provider: ProviderOllama,
		BaseURL:  "not a url",
		Model:    "m",
	}); !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Save() = %v, want ErrInvalidConfig for a relative base URL", err)
	}
	if store.saves != 0 {
		t.Fatal("an invalid configuration was persisted")
	}

	public, err := service.Save(context.Background(), UpdateInput{
		Enabled:  true,
		Provider: ProviderOllama,
		BaseURL:  "http://127.0.0.1:11434",
		Model:    "qwen2.5:3b",
	})
	if err != nil {
		t.Fatalf("Save() = %v", err)
	}
	if store.saves != 1 || store.config.Model != "qwen2.5:3b" {
		t.Fatalf("store holds %+v after %d saves", store.config, store.saves)
	}
	if public.UpdatedAt == 0 {
		t.Fatal("Save() did not stamp UpdatedAt")
	}
	if !service.Available(JobChatTitle) {
		t.Fatal("saving new settings must close the breaker so a fix takes effect at once")
	}
}

func TestTestReportsLatencyAndTheModelsAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"message":{"content":"I am working, I am qwen2.5:3b."}}`))
	}))
	defer server.Close()

	cfg := activeConfig()
	cfg.BaseURL = server.URL
	now := time.Now()
	ticks := 0
	service := New(context.Background(), &memoryStore{config: cfg},
		WithHTTPClient(server.Client()),
		WithClock(func() time.Time {
			ticks++
			return now.Add(time.Duration(ticks-1) * 120 * time.Millisecond)
		}),
	)

	result := service.Test(context.Background())
	if !result.OK {
		t.Fatalf("Test() = %+v, want a successful round trip", result)
	}
	if !strings.Contains(result.Answer, "qwen2.5:3b") {
		t.Fatalf("answer = %q, want the model's own words", result.Answer)
	}
	if result.DurationMS != 120 {
		t.Fatalf("durationMs = %d, want the measured round trip", result.DurationMS)
	}
	if result.Model != DefaultModel || result.Provider != ProviderOllama {
		t.Fatalf("result = %+v, want it to name what it called", result)
	}
}

func TestTestReportsAFailureInsteadOfPanicking(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("ollama is not running"))
	}))
	defer server.Close()

	cfg := activeConfig()
	cfg.BaseURL = server.URL
	service := New(context.Background(), &memoryStore{config: cfg}, WithHTTPClient(server.Client()))

	result := service.Test(context.Background())
	if result.OK || result.Error == "" {
		t.Fatalf("Test() = %+v, want a reported failure", result)
	}
	if !strings.Contains(result.Error, "500") {
		t.Fatalf("error = %q, want it to name the status", result.Error)
	}
}

func TestNewSurvivesAnUnreadableStore(t *testing.T) {
	// A broken settings file must never keep the server from booting.
	service := New(context.Background(), &memoryStore{loadErr: errors.New("disk on fire")})
	if service == nil {
		t.Fatal("New() = nil")
	}
	if service.Config().Enabled {
		t.Fatal("an unreadable document must leave the auxiliary model off")
	}
	if _, err := service.Complete(context.Background(), JobChatTitle, "s", "u"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Complete() = %v, want ErrDisabled", err)
	}
}

func TestANilServiceIsUsable(t *testing.T) {
	// Callers hold optional handles; a nil one has to answer rather than
	// panic, because that is the "this deployment has no model" path.
	var service *Service
	if service.Available(JobChatTitle) {
		t.Fatal("a nil service reported a job available")
	}
	if _, err := service.Complete(context.Background(), JobChatTitle, "s", "u"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Complete() on a nil service = %v, want ErrDisabled", err)
	}
	if service.ClientConfig().Enabled {
		t.Fatal("a nil service reported itself enabled")
	}
	if got := service.PublicConfig().Provider; got != ProviderOllama {
		t.Fatalf("PublicConfig().Provider = %q, want the default", got)
	}
}
