package auxmodel

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

// Per-job routing: local, pool, or off.
//
// The rule this file pins is the one the whole feature rests on: a job never
// breaks. A pool job that cannot be served drops to the local endpoint, a
// local endpoint that cannot answer returns an error the caller turns into
// its original non-AI behaviour, and "off" is off.

// stubPool is the provider pool, scripted.
type stubPool struct {
	mu        sync.Mutex
	available bool
	answer    string
	err       error
	requests  []PoolRequest
}

func (p *stubPool) Available() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.available
}

func (p *stubPool) Complete(_ context.Context, request PoolRequest) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.requests = append(p.requests, request)
	if p.err != nil {
		return "", p.err
	}
	return p.answer, nil
}

func (p *stubPool) called() []PoolRequest {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]PoolRequest, len(p.requests))
	copy(out, p.requests)
	return out
}

// stubLocal is the operator's own endpoint, scripted.
type stubLocal struct {
	mu       sync.Mutex
	answer   string
	err      error
	requests []Completion
}

func (l *stubLocal) Complete(_ context.Context, request Completion) (string, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.requests = append(l.requests, request)
	if l.err != nil {
		return "", l.err
	}
	return l.answer, nil
}

func (l *stubLocal) callCount() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.requests)
}

// routingConfig is a service that is switched on with a working local
// endpoint described, so the only variable in these tests is the job's route.
func routingConfig(source JobSource) Config {
	config := DefaultConfig()
	config.Enabled = true
	config.Jobs = JobSettings{}
	for _, job := range Jobs() {
		config.Jobs[job] = source
	}
	return config.Normalize()
}

type staticStore struct{ config Config }

func (s staticStore) Load(context.Context) (Config, error) { return s.config, nil }
func (s staticStore) Save(context.Context, Config) error   { return nil }

func TestEachJobRoutesToItsOwnSource(t *testing.T) {
	tests := []struct {
		name          string
		source        JobSource
		poolAvailable bool
		poolAnswer    string
		poolErr       error
		localAnswer   string
		localErr      error
		wantAnswer    string
		wantErr       error
		wantPoolCalls int
		wantLocalCall int
	}{
		{
			name:          "a local job never touches the pool",
			source:        SourceLocal,
			poolAvailable: true,
			poolAnswer:    "from the pool",
			localAnswer:   "from the local model",
			wantAnswer:    "from the local model",
			wantPoolCalls: 0,
			wantLocalCall: 1,
		},
		{
			name:          "a pool job never touches the local endpoint when the pool answers",
			source:        SourcePool,
			poolAvailable: true,
			poolAnswer:    "from the pool",
			localAnswer:   "from the local model",
			wantAnswer:    "from the pool",
			wantPoolCalls: 1,
			wantLocalCall: 0,
		},
		{
			name:          "a pool job falls back to local when the pool is exhausted",
			source:        SourcePool,
			poolAvailable: true,
			poolErr:       errors.New("no provider in the pool can take this request"),
			localAnswer:   "from the local model",
			wantAnswer:    "from the local model",
			wantPoolCalls: 1,
			wantLocalCall: 1,
		},
		{
			name:          "a pool job with both routes dead reports a failure the caller falls back from",
			source:        SourcePool,
			poolAvailable: true,
			poolErr:       errors.New("pool exhausted"),
			localErr:      errors.New("connection refused"),
			wantErr:       errNotNil,
			wantPoolCalls: 1,
			wantLocalCall: 1,
		},
		{
			name:          "an off job is off whatever either route could do",
			source:        SourceOff,
			poolAvailable: true,
			poolAnswer:    "from the pool",
			localAnswer:   "from the local model",
			wantErr:       ErrDisabled,
			wantPoolCalls: 0,
			wantLocalCall: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			pool := &stubPool{available: test.poolAvailable, answer: test.poolAnswer, err: test.poolErr}
			local := &stubLocal{answer: test.localAnswer, err: test.localErr}
			service := New(context.Background(), staticStore{config: routingConfig(test.source)},
				WithCompleter(local), WithPool(pool))

			answer, err := service.Complete(context.Background(), JobChatTitle, "system", "user text")

			switch {
			case test.wantErr == errNotNil:
				if err == nil {
					t.Fatal("Complete() = nil, want a failure the caller turns into its fallback")
				}
			case test.wantErr != nil:
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("Complete() error = %v, want %v", err, test.wantErr)
				}
			default:
				if err != nil {
					t.Fatalf("Complete() = %v", err)
				}
				if answer != test.wantAnswer {
					t.Fatalf("answer = %q, want %q", answer, test.wantAnswer)
				}
			}
			if got := len(pool.called()); got != test.wantPoolCalls {
				t.Fatalf("pool calls = %d, want %d", got, test.wantPoolCalls)
			}
			if got := local.callCount(); got != test.wantLocalCall {
				t.Fatalf("local calls = %d, want %d", got, test.wantLocalCall)
			}
		})
	}
}

// errNotNil is the sentinel for "any error will do" in the table above.
var errNotNil = errors.New("any error")

func TestJobsCanBeRoutedIndividually(t *testing.T) {
	config := DefaultConfig()
	config.Enabled = true
	config.Jobs = JobSettings{
		JobChatTitle:     SourcePool,
		JobCommitMessage: SourceLocal,
		JobTranslate:     SourceOff,
	}
	pool := &stubPool{available: true, answer: "pool answer"}
	local := &stubLocal{answer: "local answer"}
	service := New(context.Background(), staticStore{config: config},
		WithCompleter(local), WithPool(pool))

	if _, err := service.Complete(context.Background(), JobChatTitle, "s", "u"); err != nil {
		t.Fatalf("the pool-routed job failed: %v", err)
	}
	if _, err := service.Complete(context.Background(), JobCommitMessage, "s", "u"); err != nil {
		t.Fatalf("the local-routed job failed: %v", err)
	}
	if _, err := service.Complete(context.Background(), JobTranslate, "s", "u"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("the off job = %v, want ErrDisabled", err)
	}
	// A job the document never mentioned keeps running locally, which is what
	// it did before per-job sources existed.
	if _, err := service.Complete(context.Background(), JobRunSummary, "s", "u"); err != nil {
		t.Fatalf("an unmentioned job failed: %v", err)
	}
	if got := len(pool.called()); got != 1 {
		t.Fatalf("pool calls = %d, want only the one job routed to it", got)
	}
	if got := local.callCount(); got != 2 {
		t.Fatalf("local calls = %d, want the local job and the unmentioned one, and nothing else", got)
	}
}

func TestAPoolJobCarriesTheJobNameAndCapabilityForTheLedger(t *testing.T) {
	pool := &stubPool{available: true, answer: "ok"}
	service := New(context.Background(), staticStore{config: routingConfig(SourcePool)},
		WithCompleter(&stubLocal{answer: "local"}), WithPool(pool))

	if _, err := service.Complete(context.Background(), JobCommitMessage, "system", "diff stat"); err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	requests := pool.called()
	if len(requests) != 1 {
		t.Fatalf("pool calls = %d", len(requests))
	}
	if requests[0].Job != string(JobCommitMessage) {
		t.Fatalf("job = %q, want the ledger to know which chore spent the tokens", requests[0].Job)
	}
	if requests[0].Capability != "code" {
		t.Fatalf("capability = %q, want a commit subject asking for a code-shaped model", requests[0].Capability)
	}
	// The per-job token cap still applies through the pool: a commit subject
	// must not cost what a translation costs.
	if requests[0].MaxTokens != maxOutputTokens(JobCommitMessage) {
		t.Fatalf("maxTokens = %d, want the job's own cap of %d",
			requests[0].MaxTokens, maxOutputTokens(JobCommitMessage))
	}
}

func TestWithNoPoolWiredEveryPoolJobRunsLocally(t *testing.T) {
	local := &stubLocal{answer: "local answer"}
	// No WithPool: this is a deployment that never connected any provider.
	service := New(context.Background(), staticStore{config: routingConfig(SourcePool)},
		WithCompleter(local))

	answer, err := service.Complete(context.Background(), JobChatTitle, "s", "u")
	if err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	if answer != "local answer" {
		t.Fatalf("answer = %q, want the local endpoint", answer)
	}
	if !service.Available(JobChatTitle) {
		t.Fatal("the job reported itself unavailable even though the local endpoint can serve it")
	}
}

func TestAPoolJobStaysAvailableWithNoLocalEndpointConfigured(t *testing.T) {
	config := routingConfig(SourcePool)
	// The operator never set up a local endpoint; the pool is the whole point
	// for them. It has to be the OpenAI-compatible provider, because an
	// Ollama configuration with a blank base URL is normalized back to the
	// loopback default this platform ships.
	config.Provider = ProviderOpenAICompatible
	config.BaseURL = ""
	config.Model = ""
	pool := &stubPool{available: true, answer: "pool answer"}
	service := New(context.Background(), staticStore{config: config},
		WithCompleter(&stubLocal{err: errors.New("should never be called")}), WithPool(pool))

	if !service.Available(JobChatTitle) {
		t.Fatal("a pool job was reported unavailable on a deployment with no local endpoint")
	}
	answer, err := service.Complete(context.Background(), JobChatTitle, "s", "u")
	if err != nil {
		t.Fatalf("Complete() = %v", err)
	}
	if answer != "pool answer" {
		t.Fatalf("answer = %q", answer)
	}

	// And when the pool cannot answer either, the caller gets a failure it
	// falls back from rather than a request to a base URL that is not there.
	pool.mu.Lock()
	pool.err = errors.New("pool exhausted")
	pool.mu.Unlock()
	if _, err := service.Complete(context.Background(), JobChatTitle, "s", "u"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Complete() = %v, want ErrDisabled once neither route can serve the job", err)
	}
}

func TestTheWholeServiceSwitchTakesEveryRouteWithIt(t *testing.T) {
	config := routingConfig(SourcePool)
	config.Enabled = false
	pool := &stubPool{available: true, answer: "pool answer"}
	service := New(context.Background(), staticStore{config: config},
		WithCompleter(&stubLocal{answer: "local"}), WithPool(pool))

	if service.Available(JobChatTitle) {
		t.Fatal("a job was offered while the whole service is switched off")
	}
	if _, err := service.Complete(context.Background(), JobChatTitle, "s", "u"); !errors.Is(err, ErrDisabled) {
		t.Fatalf("Complete() = %v, want ErrDisabled", err)
	}
	if len(pool.called()) != 0 {
		t.Fatal("the master switch did not stop the pool route")
	}
}

/* ------------------------------------------------------------------ *
 * The stored document
 * ------------------------------------------------------------------ */

func TestADocumentWrittenBeforeSourcesExistedStillMeansWhatItMeant(t *testing.T) {
	// This is the shape aux-model.json had before the provider pool arrived.
	legacy := []byte(`{
		"enabled": true,
		"provider": "ollama",
		"baseUrl": "http://127.0.0.1:11434",
		"model": "qwen2.5:3b",
		"jobs": {"chatTitle": true, "commitMessage": false}
	}`)

	var config Config
	if err := json.Unmarshal(legacy, &config); err != nil {
		t.Fatalf("an existing installation's document no longer parses: %v", err)
	}
	config = config.Normalize()

	if got := config.Jobs.Source(JobChatTitle); got != SourceLocal {
		t.Fatalf("chatTitle = %q, want the stored true to keep meaning \"the operator's own endpoint\"", got)
	}
	if got := config.Jobs.Source(JobCommitMessage); got != SourceOff {
		t.Fatalf("commitMessage = %q, want the stored false to keep meaning off", got)
	}
	if got := config.Jobs.Source(JobTranslate); got != SourceLocal {
		t.Fatalf("translate = %q, want a job the document never mentioned to keep running", got)
	}
}

func TestNormalizeSourceNeverTurnsATypoIntoSpendingSomebodysQuota(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  JobSource
	}{
		{name: "the three real values round-trip", input: "pool", want: SourcePool},
		{name: "case does not matter", input: "  OFF  ", want: SourceOff},
		{name: "local is local", input: "local", want: SourceLocal},
		{name: "an empty value is the safe default", input: "", want: SourceLocal},
		{name: "a typo is the safe default, never the pool", input: "pooll", want: SourceLocal},
		{name: "nonsense is the safe default", input: "remote", want: SourceLocal},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := NormalizeSource(test.input); got != test.want {
				t.Fatalf("NormalizeSource(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestApplyChangesOneJobsRouteWithoutRestatingTheRest(t *testing.T) {
	stored := DefaultConfig()
	next := stored.Apply(UpdateInput{
		Enabled:  true,
		Provider: ProviderOllama,
		BaseURL:  DefaultOllamaBaseURL,
		Model:    DefaultModel,
		Jobs:     map[string]string{string(JobTranslate): "pool", "not-a-job": "pool"},
	})

	if got := next.Jobs.Source(JobTranslate); got != SourcePool {
		t.Fatalf("translate = %q, want the change applied", got)
	}
	for _, job := range Jobs() {
		if job == JobTranslate {
			continue
		}
		if got := next.Jobs.Source(job); got != SourceLocal {
			t.Fatalf("%s = %q, want a job the patch never named left alone", job, got)
		}
	}
}

func TestThePublicViewNamesTheRoutesAndWhetherThePoolCouldServeThem(t *testing.T) {
	pool := &stubPool{available: true}
	service := New(context.Background(), staticStore{config: routingConfig(SourcePool)},
		WithCompleter(&stubLocal{}), WithPool(pool))

	public := service.PublicConfig()
	if !public.PoolAvailable {
		t.Fatal("the panel was not told the pool can serve a job")
	}
	if public.Jobs[string(JobChatTitle)] != "pool" {
		t.Fatalf("jobs = %+v, want the route named as a string", public.Jobs)
	}
	if strings.Join(public.Sources, ",") != "local,pool,off" {
		t.Fatalf("sources = %v, want the panel handed the vocabulary rather than hard-coding it", public.Sources)
	}

	pool.mu.Lock()
	pool.available = false
	pool.mu.Unlock()
	if service.PublicConfig().PoolAvailable {
		t.Fatal("the panel was not told the pool has nothing to offer")
	}
}
