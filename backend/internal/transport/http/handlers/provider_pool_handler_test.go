package httphandlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	serviceproviderpool "github.com/futrx-com/remote.futrx.com/internal/service/providerpool"
)

// providerPoolTestService is the pool, scripted.
type providerPoolTestService struct {
	discovered    string
	adopted       string
	adoptedModels []string
	view          serviceproviderpool.PoolView
	quota         serviceproviderpool.QuotaView
	result        serviceproviderpool.Result
	bulkErr       error
	saveErr       error
	lastBulk      serviceproviderpool.BulkInput
	lastInput     serviceproviderpool.ProviderInput
	lastIDs       []string
	deleted       string
	tested        string
	bulkCalls     int
}

func (s *providerPoolTestService) View() serviceproviderpool.PoolView   { return s.view }
func (s *providerPoolTestService) Quota() serviceproviderpool.QuotaView { return s.quota }

func (s *providerPoolTestService) Save(
	_ context.Context,
	input serviceproviderpool.ProviderInput,
	_ string,
) (serviceproviderpool.PoolView, error) {
	s.lastInput = input
	if s.saveErr != nil {
		return serviceproviderpool.PoolView{}, s.saveErr
	}
	return s.view, nil
}

func (s *providerPoolTestService) Delete(
	_ context.Context, id, _ string,
) (serviceproviderpool.PoolView, error) {
	s.deleted = id
	return s.view, nil
}

func (s *providerPoolTestService) Reorder(
	_ context.Context, ids []string, _ string,
) (serviceproviderpool.PoolView, error) {
	s.lastIDs = ids
	return s.view, nil
}

func (s *providerPoolTestService) SaveSettings(
	_ context.Context,
	_ serviceproviderpool.SettingsInput,
	_ string,
) (serviceproviderpool.PoolView, error) {
	if s.saveErr != nil {
		return serviceproviderpool.PoolView{}, s.saveErr
	}
	return s.view, nil
}

func (s *providerPoolTestService) Test(_ context.Context, id string) serviceproviderpool.TestResult {
	s.tested = id
	return serviceproviderpool.TestResult{ProviderID: id, OK: true, Answer: "I am working."}
}

func (s *providerPoolTestService) Discover(_ context.Context, id string) serviceproviderpool.Discovery {
	s.discovered = id
	return serviceproviderpool.Discovery{
		ProviderID: id,
		Available:  []string{"model-a"},
		Missing:    []string{"model-gone"},
	}
}

func (s *providerPoolTestService) AdoptModels(
	_ context.Context,
	id string,
	models []string,
	_ string,
) (serviceproviderpool.PoolView, error) {
	s.adopted = id
	s.adoptedModels = models
	return serviceproviderpool.PoolView{}, nil
}

func (s *providerPoolTestService) Bulk(
	_ context.Context,
	input serviceproviderpool.BulkInput,
) (serviceproviderpool.Result, error) {
	s.bulkCalls++
	s.lastBulk = input
	if s.bulkErr != nil {
		return serviceproviderpool.Result{}, s.bulkErr
	}
	return s.result, nil
}

func newProviderPoolTestHandler(
	service ProviderPoolService,
	caller CallerResolver,
) (*http.ServeMux, *ProviderPoolHandler) {
	handler := &ProviderPoolHandler{
		pool:    service,
		caller:  caller,
		limiter: newFixedWindowLimiter(bulkRateLimit, bulkRateWindow),
	}
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return mux, handler
}

/* ------------------------------------------------------------------ *
 * Authorization
 * ------------------------------------------------------------------ */

func TestProviderRegistryRoutesRequireAnAdmin(t *testing.T) {
	routes := []struct {
		method string
		path   string
		body   string
	}{
		{method: http.MethodGet, path: "/api/admin/providers"},
		{method: http.MethodPost, path: "/api/admin/providers", body: `{"id":"groq"}`},
		{method: http.MethodPut, path: "/api/admin/providers", body: `{"autoSwitch":true}`},
		{method: http.MethodPut, path: "/api/admin/providers/groq", body: `{"label":"Groq"}`},
		{method: http.MethodDelete, path: "/api/admin/providers/groq"},
		{method: http.MethodPost, path: "/api/admin/providers/groq/test"},
		{method: http.MethodPost, path: "/api/admin/providers/reorder", body: `{"ids":["groq"]}`},
	}
	callers := []struct {
		name   string
		caller monitoringTestCaller
		want   int
	}{
		{name: "a signed-out visitor", caller: monitoringTestCaller{}, want: http.StatusUnauthorized},
		{
			name:   "a signed-in member",
			caller: monitoringTestCaller{email: "member@example.com"},
			want:   http.StatusForbidden,
		},
	}

	for _, route := range routes {
		for _, caller := range callers {
			t.Run(route.method+" "+route.path+" as "+caller.name, func(t *testing.T) {
				mux, _ := newProviderPoolTestHandler(&providerPoolTestService{}, caller.caller)
				recorder := httptest.NewRecorder()
				mux.ServeHTTP(recorder, httptest.NewRequest(
					route.method, route.path, strings.NewReader(route.body)))
				if recorder.Code != caller.want {
					t.Fatalf("status = %d, want %d", recorder.Code, caller.want)
				}
			})
		}
	}
}

func TestTheMemberRoutesNeedOnlyASession(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		caller monitoringTestCaller
		want   int
	}{
		{
			name:   "a signed-out visitor cannot read the quota card",
			method: http.MethodGet, path: "/api/providers/quota",
			caller: monitoringTestCaller{}, want: http.StatusUnauthorized,
		},
		{
			name:   "a signed-out visitor cannot spend the operator's free tiers",
			method: http.MethodPost, path: "/api/providers/complete",
			body:   `{"job":"bulk","prompt":"hello"}`,
			caller: monitoringTestCaller{}, want: http.StatusUnauthorized,
		},
		{
			name:   "a member reads the quota card",
			method: http.MethodGet, path: "/api/providers/quota",
			caller: monitoringTestCaller{email: "member@example.com"}, want: http.StatusOK,
		},
		{
			name:   "a member uses the bulk lane",
			method: http.MethodPost, path: "/api/providers/complete",
			body:   `{"job":"bulk","prompt":"describe this product"}`,
			caller: monitoringTestCaller{email: "member@example.com"}, want: http.StatusOK,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			mux, _ := newProviderPoolTestHandler(&providerPoolTestService{}, test.caller)
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, strings.NewReader(test.body)))
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func TestAMissingPoolReports503RatherThanPanicking(t *testing.T) {
	mux, _ := newProviderPoolTestHandler(nil, monitoringTestCaller{email: "admin@example.com", isAdmin: true})
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/api/admin/providers", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

/* ------------------------------------------------------------------ *
 * Routing
 * ------------------------------------------------------------------ */

func TestTheItemRoutesReachTheRightServiceCall(t *testing.T) {
	admin := monitoringTestCaller{email: "admin@example.com", isAdmin: true}

	t.Run("the path id wins over the body, so a mismatch renames nothing", func(t *testing.T) {
		service := &providerPoolTestService{}
		mux, _ := newProviderPoolTestHandler(service, admin)
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPut, "/api/admin/providers/groq",
			strings.NewReader(`{"id":"somethingelse","label":"Groq"}`)))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
		}
		if service.lastInput.ID != "groq" {
			t.Fatalf("id = %q, want the one in the path", service.lastInput.ID)
		}
	})

	t.Run("delete names the provider from the path", func(t *testing.T) {
		service := &providerPoolTestService{}
		mux, _ := newProviderPoolTestHandler(service, admin)
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodDelete, "/api/admin/providers/cerebras", nil))
		if recorder.Code != http.StatusOK || service.deleted != "cerebras" {
			t.Fatalf("status = %d, deleted = %q", recorder.Code, service.deleted)
		}
	})

	t.Run("test answers 200 with the probe inside the body", func(t *testing.T) {
		service := &providerPoolTestService{}
		mux, _ := newProviderPoolTestHandler(service, admin)
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/providers/groq/test", nil))
		if recorder.Code != http.StatusOK || service.tested != "groq" {
			t.Fatalf("status = %d, tested = %q", recorder.Code, service.tested)
		}
	})

	t.Run("reorder is a verb, not a provider id", func(t *testing.T) {
		service := &providerPoolTestService{}
		mux, _ := newProviderPoolTestHandler(service, admin)
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/providers/reorder",
			strings.NewReader(`{"ids":["cerebras","groq"]}`)))
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
		}
		if strings.Join(service.lastIDs, ",") != "cerebras,groq" {
			t.Fatalf("ids = %v, want the order the panel sent", service.lastIDs)
		}
		if service.deleted != "" || service.lastInput.ID != "" {
			t.Fatal("the reorder verb was mistaken for a provider id")
		}
	})

	t.Run("an unsupported method is refused rather than mishandled", func(t *testing.T) {
		service := &providerPoolTestService{}
		mux, _ := newProviderPoolTestHandler(service, admin)
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPatch, "/api/admin/providers/groq", nil))
		if recorder.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", recorder.Code)
		}
	})
}

/* ------------------------------------------------------------------ *
 * The bulk lane
 * ------------------------------------------------------------------ */

func TestTheBulkLaneIsRateLimitedPerCaller(t *testing.T) {
	service := &providerPoolTestService{}
	mux, handler := newProviderPoolTestHandler(service,
		monitoringTestCaller{email: "member@example.com"})
	// A hand-wound clock so the window does not depend on how fast the test
	// machine is.
	now := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	handler.limiter.now = func() time.Time { return now }

	send := func() int {
		recorder := httptest.NewRecorder()
		mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/providers/complete",
			strings.NewReader(`{"job":"bulk","prompt":"hello"}`)))
		return recorder.Code
	}

	for attempt := 1; attempt <= bulkRateLimit; attempt++ {
		if status := send(); status != http.StatusOK {
			t.Fatalf("request %d = %d, want 200 inside the budget", attempt, status)
		}
	}
	if status := send(); status != http.StatusTooManyRequests {
		t.Fatalf("the request past the budget = %d, want 429", status)
	}
	if service.bulkCalls != bulkRateLimit {
		t.Fatalf("reached the pool %d times, want the refused request stopped at the door", service.bulkCalls)
	}

	// A different member has their own budget: one stuck client must not lock
	// everybody out.
	otherMux, otherHandler := newProviderPoolTestHandler(service,
		monitoringTestCaller{email: "someone-else@example.com"})
	otherHandler.limiter.now = func() time.Time { return now }
	recorder := httptest.NewRecorder()
	otherMux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/providers/complete",
		strings.NewReader(`{"job":"bulk","prompt":"hello"}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("a second member = %d, want their own budget", recorder.Code)
	}

	// And the window reopens.
	now = now.Add(bulkRateWindow + time.Second)
	if status := send(); status != http.StatusOK {
		t.Fatalf("after the window = %d, want 200", status)
	}
}

func TestTheBulkLaneRefusesWhatItCannotServe(t *testing.T) {
	member := monitoringTestCaller{email: "member@example.com"}

	tests := []struct {
		name    string
		body    string
		bulkErr error
		want    int
	}{
		{name: "an empty prompt", body: `{"job":"bulk","prompt":"   "}`, want: http.StatusBadRequest},
		{name: "malformed json", body: `{"job":`, want: http.StatusBadRequest},
		{
			name: "a job this route does not serve",
			body: `{"job":"chatTitle","prompt":"hello"}`,
			want: http.StatusBadRequest,
		},
		{
			name:    "a prompt over the lane's ceiling",
			body:    `{"job":"bulk","prompt":"hello"}`,
			bulkErr: serviceproviderpool.ErrPromptTooLarge,
			want:    http.StatusRequestEntityTooLarge,
		},
		{
			name:    "an exhausted pool is a 503 the caller falls back from, not a 500",
			body:    `{"job":"bulk","prompt":"hello"}`,
			bulkErr: serviceproviderpool.ErrNoProvider,
			want:    http.StatusServiceUnavailable,
		},
		{
			name: "no job named at all is still bulk, because there is only one lane",
			body: `{"prompt":"hello"}`,
			want: http.StatusOK,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &providerPoolTestService{bulkErr: test.bulkErr}
			mux, _ := newProviderPoolTestHandler(service, member)
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/providers/complete",
				strings.NewReader(test.body)))
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func TestTheBulkLaneForwardsThePinnedProviderAndCap(t *testing.T) {
	service := &providerPoolTestService{
		result: serviceproviderpool.Result{Text: "a description", ProviderID: "groq", Model: "llama"},
	}
	mux, _ := newProviderPoolTestHandler(service, monitoringTestCaller{email: "member@example.com"})

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/providers/complete",
		strings.NewReader(`{"job":"bulk","prompt":"describe","system":"be terse","maxTokens":256,"providerId":"groq"}`)))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body.String())
	}
	if service.lastBulk.ProviderID != "groq" || service.lastBulk.MaxTokens != 256 {
		t.Fatalf("forwarded %+v", service.lastBulk)
	}
	if service.lastBulk.System != "be terse" {
		t.Fatalf("system prompt = %q", service.lastBulk.System)
	}

	var answer serviceproviderpool.Result
	if err := json.Unmarshal(recorder.Body.Bytes(), &answer); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if answer.Text != "a description" || answer.ProviderID != "groq" {
		t.Fatalf("response = %+v, want the answer and which provider produced it", answer)
	}
}

/* ------------------------------------------------------------------ *
 * Error mapping
 * ------------------------------------------------------------------ */

func TestAdminWriteFailuresMapToStatusesAnOperatorCanActOn(t *testing.T) {
	admin := monitoringTestCaller{email: "admin@example.com", isAdmin: true}
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "a fixable entry is a 400", err: serviceproviderpool.ErrInvalidProvider, want: http.StatusBadRequest},
		{name: "an unknown id is a 404", err: serviceproviderpool.ErrUnknownProvider, want: http.StatusNotFound},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &providerPoolTestService{saveErr: test.err}
			mux, _ := newProviderPoolTestHandler(service, admin)
			recorder := httptest.NewRecorder()
			mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/api/admin/providers",
				strings.NewReader(`{"id":"groq"}`)))
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d", recorder.Code, test.want)
			}
		})
	}
}
