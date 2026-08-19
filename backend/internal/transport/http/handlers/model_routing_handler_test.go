package httphandlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	servicerouting "github.com/futrx-com/remote.futrx.com/internal/service/routing"
)

type stubRoutingService struct {
	view     servicerouting.View
	decision servicerouting.Decision
	updated  []servicerouting.Policy
	actors   []string
	tested   []servicerouting.TestInput
	err      error
}

func (s *stubRoutingService) View(context.Context) (servicerouting.View, error) {
	if s.err != nil {
		return servicerouting.View{}, s.err
	}
	return s.view, nil
}

func (s *stubRoutingService) Update(
	_ context.Context,
	policy servicerouting.Policy,
	actor string,
) (servicerouting.View, error) {
	s.updated = append(s.updated, policy)
	s.actors = append(s.actors, actor)
	if s.err != nil {
		return servicerouting.View{}, s.err
	}
	s.view.Policy = policy
	return s.view, nil
}

func (s *stubRoutingService) Test(
	_ context.Context,
	in servicerouting.TestInput,
) (servicerouting.Decision, error) {
	s.tested = append(s.tested, in)
	if s.err != nil {
		return servicerouting.Decision{}, s.err
	}
	return s.decision, nil
}

func newRoutingMux(service ModelRoutingService, caller CallerResolver) *http.ServeMux {
	mux := http.NewServeMux()
	NewModelRoutingHandler(service, caller).RegisterRoutes(mux)
	return mux
}

func routingRequest(mux *http.ServeMux, method, path, body string) *httptest.ResponseRecorder {
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(method, path, reader))
	return recorder
}

func TestModelRoutingAdminRoutesRequireAnAdmin(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		caller CallerResolver
		want   int
	}{
		{"anonymous GET policy", http.MethodGet, "/api/admin/model-routing", stubCaller{}, http.StatusUnauthorized},
		{"anonymous PUT policy", http.MethodPut, "/api/admin/model-routing", stubCaller{}, http.StatusUnauthorized},
		{"anonymous test", http.MethodPost, "/api/admin/model-routing/test", stubCaller{}, http.StatusUnauthorized},
		{
			"broken session", http.MethodGet, "/api/admin/model-routing",
			stubCaller{email: "member@example.com", err: errors.New("bad cookie")},
			http.StatusUnauthorized,
		},
		{
			"member GET policy", http.MethodGet, "/api/admin/model-routing",
			stubCaller{email: "member@example.com"}, http.StatusForbidden,
		},
		{
			"member PUT policy", http.MethodPut, "/api/admin/model-routing",
			stubCaller{email: "member@example.com"}, http.StatusForbidden,
		},
		{
			"member test", http.MethodPost, "/api/admin/model-routing/test",
			stubCaller{email: "member@example.com"}, http.StatusForbidden,
		},
		{
			"admin GET policy", http.MethodGet, "/api/admin/model-routing",
			stubCaller{email: "admin@example.com", isAdmin: true}, http.StatusOK,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			mux := newRoutingMux(&stubRoutingService{}, testCase.caller)
			got := routingRequest(mux, testCase.method, testCase.path, "{}")
			if got.Code != testCase.want {
				t.Fatalf("status = %d, want %d (%s)", got.Code, testCase.want, got.Body.String())
			}
		})
	}
}

func TestModelRoutingPreviewNeedsOnlyASession(t *testing.T) {
	tests := []struct {
		name   string
		caller CallerResolver
		want   int
	}{
		{"anonymous", stubCaller{}, http.StatusUnauthorized},
		{"member", stubCaller{email: "member@example.com"}, http.StatusOK},
		{"admin", stubCaller{email: "admin@example.com", isAdmin: true}, http.StatusOK},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			service := &stubRoutingService{
				decision: servicerouting.Decision{Provider: "claude", Model: "haiku", Routed: true},
			}
			mux := newRoutingMux(service, testCase.caller)
			got := routingRequest(
				mux, http.MethodPost, "/api/model-routing/preview",
				`{"prompt":"ping","mode":"chat"}`,
			)
			if got.Code != testCase.want {
				t.Fatalf("status = %d, want %d (%s)", got.Code, testCase.want, got.Body.String())
			}
			if testCase.want != http.StatusOK {
				return
			}
			var decision servicerouting.Decision
			if err := json.Unmarshal(got.Body.Bytes(), &decision); err != nil {
				t.Fatalf("decode error = %v", err)
			}
			if decision.Model != "haiku" {
				t.Fatalf("decision = %+v, want the service's answer", decision)
			}
			if len(service.tested) != 1 || service.tested[0].Prompt != "ping" {
				t.Fatalf("service saw %+v, want the posted prompt", service.tested)
			}
		})
	}
}

func TestModelRoutingUpdatePassesThePolicyAndTheActor(t *testing.T) {
	service := &stubRoutingService{}
	mux := newRoutingMux(service, stubCaller{email: "admin@example.com", isAdmin: true})
	got := routingRequest(mux, http.MethodPut, "/api/admin/model-routing", `{
		"enabled": true,
		"default": {"provider": "claude", "model": "sonnet"},
		"rules": [{"id":"chat-mode","when":{"kind":"modeIs","value":"chat"},
		           "use":{"provider":"claude","model":"haiku"},"enabled":true}]
	}`)
	if got.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", got.Code, got.Body.String())
	}
	if len(service.updated) != 1 {
		t.Fatalf("updates = %d, want 1", len(service.updated))
	}
	policy := service.updated[0]
	if !policy.Enabled || policy.Default.Model != "sonnet" || len(policy.Rules) != 1 {
		t.Fatalf("decoded policy = %+v", policy)
	}
	if policy.Rules[0].When.Kind != servicerouting.KindModeIs {
		t.Fatalf("rule condition = %+v", policy.Rules[0].When)
	}
	if service.actors[0] != "admin@example.com" {
		t.Fatalf("actor = %q, want the session's email", service.actors[0])
	}
}

func TestModelRoutingRejectsAnInvalidPolicy(t *testing.T) {
	service := &stubRoutingService{err: servicerouting.ErrInvalidPolicy}
	mux := newRoutingMux(service, stubCaller{email: "admin@example.com", isAdmin: true})
	got := routingRequest(mux, http.MethodPut, "/api/admin/model-routing", `{"enabled":true}`)
	if got.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", got.Code, got.Body.String())
	}
}

func TestModelRoutingRejectsBadJSONAndBadMethods(t *testing.T) {
	admin := stubCaller{email: "admin@example.com", isAdmin: true}
	mux := newRoutingMux(&stubRoutingService{}, admin)

	if got := routingRequest(mux, http.MethodPut, "/api/admin/model-routing", "{"); got.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for malformed json", got.Code)
	}
	if got := routingRequest(mux, http.MethodDelete, "/api/admin/model-routing", ""); got.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", got.Code)
	}
	if got := routingRequest(mux, http.MethodGet, "/api/admin/model-routing/test", ""); got.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", got.Code)
	}
}

func TestModelRoutingReportsAnUnavailableService(t *testing.T) {
	mux := newRoutingMux(nil, stubCaller{email: "admin@example.com", isAdmin: true})
	for _, path := range []string{
		"/api/admin/model-routing",
		"/api/admin/model-routing/test",
		"/api/model-routing/preview",
	} {
		got := routingRequest(mux, http.MethodPost, path, "{}")
		if got.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s status = %d, want 503", path, got.Code)
		}
	}
}
