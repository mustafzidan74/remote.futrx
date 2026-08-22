package agentendpoints

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

/* ------------------------------------------------------------------ *
 * Doubles
 * ------------------------------------------------------------------ */

type memoryStore struct {
	mu        sync.Mutex
	endpoints []Endpoint
	loadErr   error
}

func (s *memoryStore) Load(context.Context) ([]Endpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	return append([]Endpoint(nil), s.endpoints...), nil
}

func (s *memoryStore) Save(_ context.Context, endpoints []Endpoint) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.endpoints = append([]Endpoint(nil), endpoints...)
	return nil
}

// staticSecrets stands in for the vault. It answers only for keys it holds,
// which is exactly how an unscoped or missing entry behaves.
type staticSecrets map[string]string

func (s staticSecrets) PlatformValues(_ context.Context, keys []string) (map[string]string, error) {
	values := map[string]string{}
	for _, key := range keys {
		if value, ok := s[key]; ok {
			values[key] = value
		}
	}
	return values, nil
}

type recordedProbe struct {
	container string
	probe     Probe
}

type fakeContainers struct {
	mu      sync.Mutex
	calls   []recordedProbe
	output  string
	failure error
}

func (c *fakeContainers) Probe(_ context.Context, container string, probe Probe) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, recordedProbe{container: container, probe: probe})
	return c.output, c.failure
}

func (c *fakeContainers) last() recordedProbe {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.calls) == 0 {
		return recordedProbe{}
	}
	return c.calls[len(c.calls)-1]
}

type fakeProjects struct {
	target Target
	err    error
}

func (p fakeProjects) EndpointTarget(context.Context, string) (Target, error) {
	return p.target, p.err
}

func fixedClock() func() time.Time {
	moment := time.UnixMilli(1_700_000_000_000)
	return func() time.Time { return moment }
}

/* ------------------------------------------------------------------ *
 * Register
 * ------------------------------------------------------------------ */

func TestCreateRejectsInvalidProfiles(t *testing.T) {
	t.Parallel()

	valid := Endpoint{
		ID:        "vendor-x",
		Label:     "Vendor X",
		CLI:       CLIClaude,
		BaseURL:   "https://api.vendor.test/anthropic",
		APIKeyRef: "VENDOR_X_KEY",
		Enabled:   true,
	}
	mutate := func(change func(*Endpoint)) Endpoint {
		candidate := valid
		change(&candidate)
		return candidate
	}

	cases := []struct {
		name     string
		endpoint Endpoint
		want     error
	}{
		{"a valid profile is accepted", valid, nil},
		{
			name:     "an id that is not a bare TOML key is refused",
			endpoint: mutate(func(e *Endpoint) { e.ID = "vendor.x" }),
			want:     ErrInvalidID,
		},
		{
			name:     "an id starting with a separator is refused",
			endpoint: mutate(func(e *Endpoint) { e.ID = "-vendor" }),
			want:     ErrInvalidID,
		},
		{
			name:     "an unsupported CLI is refused",
			endpoint: mutate(func(e *Endpoint) { e.CLI = "kimi" }),
			want:     ErrInvalidCLI,
		},
		{
			name:     "a relative base URL is refused",
			endpoint: mutate(func(e *Endpoint) { e.BaseURL = "/anthropic" }),
			want:     ErrInvalidURL,
		},
		{
			name:     "a base URL carrying credentials is refused",
			endpoint: mutate(func(e *Endpoint) { e.BaseURL = "https://user:pass@api.vendor.test/v1" }),
			want:     ErrInvalidURL,
		},
		{
			name:     "an enabled profile with no key reference is refused",
			endpoint: mutate(func(e *Endpoint) { e.APIKeyRef = "" }),
			want:     ErrInvalidKeyRef,
		},
		{
			name:     "a lower-case key reference is refused",
			endpoint: mutate(func(e *Endpoint) { e.APIKeyRef = "vendor_key" }),
			want:     ErrInvalidKeyRef,
		},
		{
			name:     "a header name that is not a header token is refused",
			endpoint: mutate(func(e *Endpoint) { e.Headers = map[string]string{"bad name": "v"} }),
			want:     ErrInvalidHeader,
		},
		{
			name:     "a model id containing whitespace is refused",
			endpoint: mutate(func(e *Endpoint) { e.Models = []Model{{ID: "glm 4.6"}} }),
			want:     ErrInvalidModel,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			service := New(&memoryStore{}, WithClock(fixedClock()))
			_, err := service.Create(context.Background(), testCase.endpoint, "admin@example.test")
			if testCase.want == nil {
				if err != nil {
					t.Fatalf("Create: unexpected error %v", err)
				}
				return
			}
			if !errors.Is(err, testCase.want) {
				t.Fatalf("Create error = %v, want %v", err, testCase.want)
			}
		})
	}
}

// A template may sit disabled without naming a vault key — that is how every
// seeded profile ships — but it must not be switchable on until it does.
func TestSetEnabledRefusesAProfileWithNoKeyReference(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	service := New(store, WithClock(fixedClock()))
	template := Endpoint{
		ID:      "vendor-x",
		Label:   "Vendor X",
		CLI:     CLIClaude,
		BaseURL: "https://api.vendor.test/anthropic",
	}
	if _, err := service.Create(context.Background(), template, "admin@example.test"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := service.SetEnabled(context.Background(), "vendor-x", true, "admin@example.test"); !errors.Is(err, ErrInvalidKeyRef) {
		t.Fatalf("SetEnabled error = %v, want ErrInvalidKeyRef", err)
	}
}

func TestCreateRejectsADuplicateID(t *testing.T) {
	t.Parallel()

	service := New(&memoryStore{}, WithClock(fixedClock()))
	endpoint := claudeProfile()
	if _, err := service.Create(context.Background(), endpoint, "admin@example.test"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := service.Create(context.Background(), endpoint, "admin@example.test"); !errors.Is(err, ErrExists) {
		t.Fatalf("second Create error = %v, want ErrExists", err)
	}
}

// The admin table has to be able to say "this profile's key is not set"
// without ever handling the value itself.
func TestListReportsKeyResolutionWithoutTheValue(t *testing.T) {
	t.Parallel()

	store := &memoryStore{}
	secrets := staticSecrets{"ZHIPU_API_KEY": "vendor-key-123456"}
	service := New(store, WithSecrets(secrets), WithClock(fixedClock()))

	if _, err := service.Create(context.Background(), claudeProfile(), "admin@example.test"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	missing := codexProfile()
	missing.APIKeyRef = "NOT_IN_THE_VAULT"
	if _, err := service.Create(context.Background(), missing, "admin@example.test"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	views, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("List returned %d views, want 2", len(views))
	}
	for _, view := range views {
		want := view.ID == "zhipu-glm"
		if view.KeyResolved != want {
			t.Errorf("%s KeyResolved = %v, want %v", view.ID, view.KeyResolved, want)
		}
		// Belt and braces: no field of the projection may carry the value.
		if strings.Contains(view.Notes+view.BaseURL+view.APIKeyRef, "vendor-key-123456") {
			t.Errorf("%s: a resolved value reached the API projection", view.ID)
		}
	}
}

// The composer's read must be enabled-only and must not describe how to reach
// anything: a project member is not an administrator.
func TestChoicesExposeOnlyEnabledProfilesAndNoReachability(t *testing.T) {
	t.Parallel()

	service := New(&memoryStore{}, WithClock(fixedClock()))
	if _, err := service.Create(context.Background(), claudeProfile(), "admin@example.test"); err != nil {
		t.Fatalf("Create: %v", err)
	}
	disabled := codexProfile()
	disabled.Enabled = false
	if _, err := service.Create(context.Background(), disabled, "admin@example.test"); err != nil {
		t.Fatalf("Create: %v", err)
	}

	choices, err := service.Choices(context.Background())
	if err != nil {
		t.Fatalf("Choices: %v", err)
	}
	if len(choices) != 1 || choices[0].ID != "zhipu-glm" {
		t.Fatalf("Choices = %+v, want only the enabled profile", choices)
	}
	if len(choices[0].Models) != 2 {
		t.Errorf("choice models = %+v, want the profile's two", choices[0].Models)
	}
}

/* ------------------------------------------------------------------ *
 * The run path
 * ------------------------------------------------------------------ */

func TestRuntimeFor(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		endpoint Endpoint
		secrets  staticSecrets
		lookupID string
		model    string
		wantErr  error
		assert   func(*testing.T, Runtime)
	}{
		{
			name:     "an enabled profile with a resolvable key renders",
			endpoint: claudeProfile(),
			secrets:  staticSecrets{"ZHIPU_API_KEY": "vendor-key-123456"},
			lookupID: "zhipu-glm",
			model:    "glm-4.5-air",
			assert: func(t *testing.T, runtime Runtime) {
				if runtime.Model != "glm-4.5-air" {
					t.Errorf("model = %q, want glm-4.5-air", runtime.Model)
				}
				if runtime.Env[EnvAnthropicAuthToken] != "vendor-key-123456" {
					t.Errorf("auth token was not the resolved vault value")
				}
			},
		},
		{
			name: "a disabled profile refuses to run",
			endpoint: func() Endpoint {
				profile := claudeProfile()
				profile.Enabled = false
				return profile
			}(),
			secrets:  staticSecrets{"ZHIPU_API_KEY": "vendor-key-123456"},
			lookupID: "zhipu-glm",
			wantErr:  ErrDisabled,
		},
		{
			name:     "an unknown id refuses to run",
			endpoint: claudeProfile(),
			secrets:  staticSecrets{"ZHIPU_API_KEY": "vendor-key-123456"},
			lookupID: "no-such-endpoint",
			wantErr:  ErrNotFound,
		},
		{
			name:     "an unset vault key refuses to run",
			endpoint: claudeProfile(),
			secrets:  staticSecrets{},
			lookupID: "zhipu-glm",
			wantErr:  ErrKeyUnresolved{Endpoint: "zhipu-glm", Key: "ZHIPU_API_KEY"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			store := &memoryStore{endpoints: []Endpoint{testCase.endpoint}}
			service := New(store, WithSecrets(testCase.secrets), WithClock(fixedClock()))

			runtime, err := service.RuntimeFor(context.Background(), testCase.lookupID, testCase.model)
			if testCase.wantErr != nil {
				if err == nil {
					t.Fatalf("RuntimeFor: want error %v, got none", testCase.wantErr)
				}
				var unresolved ErrKeyUnresolved
				if errors.As(testCase.wantErr, &unresolved) {
					if !errors.As(err, &unresolved) {
						t.Fatalf("RuntimeFor error = %v, want an unresolved-key error", err)
					}
					return
				}
				if !errors.Is(err, testCase.wantErr) {
					t.Fatalf("RuntimeFor error = %v, want %v", err, testCase.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("RuntimeFor: %v", err)
			}
			testCase.assert(t, runtime)
		})
	}
}

// A deployment with no vault cannot resolve anything, and must fail closed
// rather than launching a CLI with an empty bearer token.
func TestRuntimeForFailsClosedWithoutAVault(t *testing.T) {
	t.Parallel()

	store := &memoryStore{endpoints: []Endpoint{claudeProfile()}}
	service := New(store, WithClock(fixedClock()))
	if _, err := service.RuntimeFor(context.Background(), "zhipu-glm", "glm-4.6"); err == nil {
		t.Fatal("RuntimeFor without a vault: want an error")
	}
}

/* ------------------------------------------------------------------ *
 * Test probe
 * ------------------------------------------------------------------ */

func TestTestProbeMasksTheKeyOutOfOutput(t *testing.T) {
	t.Parallel()

	const key = "vendor-key-123456"
	store := &memoryStore{endpoints: []Endpoint{claudeProfile()}}
	containers := &fakeContainers{output: "ready (auth=" + key + ")"}
	service := New(
		store,
		WithSecrets(staticSecrets{"ZHIPU_API_KEY": key}),
		WithContainers(containers),
		WithProjects(fakeProjects{target: Target{ContainerName: "proj-a", Running: true}}),
		WithClock(fixedClock()),
	)

	result, err := service.Test(context.Background(), "zhipu-glm", "project-1", "glm-4.6")
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if !result.OK {
		t.Errorf("result.OK = false, want true")
	}
	if strings.Contains(result.Output, key) {
		t.Fatalf("the resolved key survived into probe output: %q", result.Output)
	}
	if !strings.Contains(result.Output, "••••••••") {
		t.Errorf("output = %q, want the key masked", result.Output)
	}

	// The probe must have been handed the endpoint's environment, and a
	// two-word prompt.
	call := containers.last()
	if call.container != "proj-a" {
		t.Errorf("probed container = %q, want proj-a", call.container)
	}
	if call.probe.Env[EnvAnthropicBaseURL] != "https://open.bigmodel.cn/api/anthropic" {
		t.Errorf("probe env did not carry the endpoint's base URL")
	}
	if words := strings.Fields(call.probe.Prompt); len(words) != 2 {
		t.Errorf("probe prompt = %q, want two words", call.probe.Prompt)
	}
}

// Confirming a template's values before switching it on is the whole point of
// the Test button, so a disabled profile must still be testable.
func TestTestProbeRunsForADisabledProfile(t *testing.T) {
	t.Parallel()

	profile := claudeProfile()
	profile.Enabled = false
	store := &memoryStore{endpoints: []Endpoint{profile}}
	containers := &fakeContainers{output: "ready"}
	service := New(
		store,
		WithSecrets(staticSecrets{"ZHIPU_API_KEY": "vendor-key-123456"}),
		WithContainers(containers),
		WithProjects(fakeProjects{target: Target{ContainerName: "proj-a", Running: true}}),
		WithClock(fixedClock()),
	)

	if _, err := service.Test(context.Background(), "zhipu-glm", "project-1", ""); err != nil {
		t.Fatalf("Test on a disabled profile: %v", err)
	}
}

func TestTestProbeRefusesWithoutARunningContainer(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		projectID string
		target    Target
	}{
		{"no project named", "", Target{ContainerName: "proj-a", Running: true}},
		{"container stopped", "project-1", Target{ContainerName: "proj-a", Running: false}},
		{"project has no container", "project-1", Target{Running: true}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			service := New(
				&memoryStore{endpoints: []Endpoint{claudeProfile()}},
				WithSecrets(staticSecrets{"ZHIPU_API_KEY": "vendor-key-123456"}),
				WithContainers(&fakeContainers{output: "ready"}),
				WithProjects(fakeProjects{target: testCase.target}),
				WithClock(fixedClock()),
			)
			if _, err := service.Test(context.Background(), "zhipu-glm", testCase.projectID, ""); err == nil {
				t.Fatal("Test: want an error")
			}
		})
	}
}

// A failed probe has to leave a readable trace on the profile — and that
// trace must be masked too.
func TestTestProbeStampsTheProfileWithAMaskedOutcome(t *testing.T) {
	t.Parallel()

	const key = "vendor-key-123456"
	store := &memoryStore{endpoints: []Endpoint{claudeProfile()}}
	containers := &fakeContainers{
		output:  "401 unauthorized for token " + key + "\nsecond line",
		failure: errors.New("exit status 1"),
	}
	service := New(
		store,
		WithSecrets(staticSecrets{"ZHIPU_API_KEY": key}),
		WithContainers(containers),
		WithProjects(fakeProjects{target: Target{ContainerName: "proj-a", Running: true}}),
		WithClock(fixedClock()),
	)

	result, err := service.Test(context.Background(), "zhipu-glm", "project-1", "")
	if err != nil {
		t.Fatalf("Test: %v", err)
	}
	if result.OK {
		t.Fatal("result.OK = true, want false")
	}

	views, err := service.List(context.Background())
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(views) != 1 || views[0].LastTest == nil {
		t.Fatalf("expected a stamped test record, got %+v", views)
	}
	record := views[0].LastTest
	if record.OK {
		t.Error("record.OK = true, want false")
	}
	if record.ProjectID != "project-1" {
		t.Errorf("record.ProjectID = %q, want project-1", record.ProjectID)
	}
	if strings.Contains(record.Message, key) {
		t.Fatalf("the resolved key survived into the stored test record: %q", record.Message)
	}
	if strings.ContainsAny(record.Message, "\r\n") {
		t.Errorf("record.Message spans lines: %q", record.Message)
	}
}

/* ------------------------------------------------------------------ *
 * Seeds
 * ------------------------------------------------------------------ */

// The templates are the feature's compliance surface: every one of them must
// be a vendor's own published endpoint, must arrive switched off, and must
// name no credential.
func TestSeedTemplatesShipDisabledAndKeyless(t *testing.T) {
	t.Parallel()

	seeds := Seed()
	if len(seeds) == 0 {
		t.Fatal("Seed returned nothing")
	}
	ids := map[string]bool{}
	for _, endpoint := range seeds {
		t.Run(endpoint.ID, func(t *testing.T) {
			if endpoint.Enabled {
				t.Error("a seeded template must ship disabled")
			}
			if endpoint.APIKeyRef != "" {
				t.Errorf("a seeded template must name no vault key, got %q", endpoint.APIKeyRef)
			}
			if !strings.HasPrefix(endpoint.BaseURL, "https://") {
				t.Errorf("base URL %q is not https", endpoint.BaseURL)
			}
			if len(endpoint.Models) == 0 {
				t.Error("a seeded template should list at least one model")
			}
			if strings.TrimSpace(endpoint.Notes) == "" {
				t.Error("a seeded template must record where its values came from")
			}
			// Normalization is what the store and the run path both apply, so
			// a template that cannot survive it would silently disappear.
			if _, err := Normalize(endpoint); err != nil {
				t.Errorf("Normalize: %v", err)
			}
		})
		if ids[endpoint.ID] {
			t.Errorf("duplicate seed id %q", endpoint.ID)
		}
		ids[endpoint.ID] = true
	}

	// Both sanctioned shapes must be represented, or the feature only half
	// exists on a fresh install.
	var claude, codex int
	for _, endpoint := range seeds {
		switch endpoint.CLI {
		case CLIClaude:
			claude++
		case CLICodex:
			codex++
		default:
			t.Errorf("%s names unsupported CLI %q", endpoint.ID, endpoint.CLI)
		}
	}
	// Only the claude side is asserted. codex-cli 0.145.0 refuses a Chat
	// Completions provider outright and sends no credential for a Responses
	// one, so there is nothing honest to ship for it — see the comment on
	// Seed. If a codex template ever comes back, it comes back with a passing
	// Test behind it, not with this assertion loosened.
	if claude == 0 {
		t.Errorf("seeds cover %d claude profiles, want at least one", claude)
	}
}
