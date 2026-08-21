package httphandlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	serviceendpoints "github.com/futrx-com/remote.futrx.com/internal/service/agentendpoints"
)

type agentEndpointsStub struct {
	list     []serviceendpoints.View
	choices  []serviceendpoints.Choice
	created  []serviceendpoints.Endpoint
	updated  []serviceendpoints.Endpoint
	deleted  []string
	enabled  []bool
	tested   [][3]string
	actors   []string
	writeErr error
	result   serviceendpoints.TestResult
}

func (s *agentEndpointsStub) List(context.Context) ([]serviceendpoints.View, error) {
	return append([]serviceendpoints.View(nil), s.list...), s.writeErr
}

func (s *agentEndpointsStub) Choices(context.Context) ([]serviceendpoints.Choice, error) {
	return append([]serviceendpoints.Choice(nil), s.choices...), s.writeErr
}

func (s *agentEndpointsStub) Create(
	_ context.Context,
	endpoint serviceendpoints.Endpoint,
	actor string,
) (serviceendpoints.View, error) {
	s.created = append(s.created, endpoint)
	s.actors = append(s.actors, actor)
	return serviceendpoints.View{Endpoint: endpoint}, s.writeErr
}

func (s *agentEndpointsStub) Update(
	_ context.Context,
	id string,
	endpoint serviceendpoints.Endpoint,
	actor string,
) (serviceendpoints.View, error) {
	endpoint.ID = id
	s.updated = append(s.updated, endpoint)
	s.actors = append(s.actors, actor)
	return serviceendpoints.View{Endpoint: endpoint}, s.writeErr
}

func (s *agentEndpointsStub) Delete(_ context.Context, id string) error {
	s.deleted = append(s.deleted, id)
	return s.writeErr
}

func (s *agentEndpointsStub) SetEnabled(
	_ context.Context,
	id string,
	enabled bool,
	actor string,
) (serviceendpoints.View, error) {
	s.enabled = append(s.enabled, enabled)
	s.actors = append(s.actors, actor)
	return serviceendpoints.View{
		Endpoint: serviceendpoints.Endpoint{ID: id, Enabled: enabled},
	}, s.writeErr
}

func (s *agentEndpointsStub) Test(
	_ context.Context,
	id, projectID, model string,
) (serviceendpoints.TestResult, error) {
	s.tested = append(s.tested, [3]string{id, projectID, model})
	if s.writeErr != nil {
		return serviceendpoints.TestResult{}, s.writeErr
	}
	return s.result, nil
}

func newAgentEndpointsHandler(
	service AgentEndpointsService,
	caller CallerResolver,
) *AgentEndpointsHandler {
	return &AgentEndpointsHandler{endpoints: service, caller: caller}
}

func serveAgentEndpoints(
	t *testing.T,
	handler *AgentEndpointsHandler,
	method, target, body string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	recorder := httptest.NewRecorder()
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	mux.ServeHTTP(recorder, request)
	return recorder
}

// The admin half decides which outside company sees a client's source code
// and can start an agent process inside somebody else's container, so every
// route on it is administrator-only.
func TestAgentEndpointAdminRoutesAreAdminOnly(t *testing.T) {
	const profile = `{"label":"Zhipu GLM","cli":"claude",` +
		`"baseUrl":"https://open.bigmodel.cn/api/anthropic","apiKeyRef":"ZHIPU_API_KEY"}`

	tests := []struct {
		name       string
		method     string
		target     string
		body       string
		caller     callerStub
		wantStatus int
	}{
		{
			name:       "anonymous listing is refused",
			method:     http.MethodGet,
			target:     "/api/admin/agent-endpoints",
			caller:     callerStub{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "a member may not list the register",
			method:     http.MethodGet,
			target:     "/api/admin/agent-endpoints",
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "an admin may list the register",
			method:     http.MethodGet,
			target:     "/api/admin/agent-endpoints",
			caller:     callerStub{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusOK,
		},
		{
			name:       "a member may not add a profile",
			method:     http.MethodPost,
			target:     "/api/admin/agent-endpoints",
			body:       profile,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "a member may not edit a profile",
			method:     http.MethodPut,
			target:     "/api/admin/agent-endpoints/zhipu-glm",
			body:       profile,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "a member may not delete a profile",
			method:     http.MethodDelete,
			target:     "/api/admin/agent-endpoints/zhipu-glm",
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "a member may not switch a profile on",
			method:     http.MethodPut,
			target:     "/api/admin/agent-endpoints/zhipu-glm/enabled",
			body:       `{"enabled":true}`,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "a member may not run the probe",
			method:     http.MethodPost,
			target:     "/api/admin/agent-endpoints/zhipu-glm/test",
			body:       `{"projectId":"p1"}`,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "an admin may run the probe",
			method:     http.MethodPost,
			target:     "/api/admin/agent-endpoints/zhipu-glm/test",
			body:       `{"projectId":"p1"}`,
			caller:     callerStub{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusOK,
		},
		{
			name:       "an unknown sub-route is a 404, not an action",
			method:     http.MethodPost,
			target:     "/api/admin/agent-endpoints/zhipu-glm/exec",
			caller:     callerStub{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "a missing id is a bad request",
			method:     http.MethodDelete,
			target:     "/api/admin/agent-endpoints/",
			caller:     callerStub{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			handler := newAgentEndpointsHandler(&agentEndpointsStub{}, testCase.caller)
			recorder := serveAgentEndpoints(t, handler, testCase.method, testCase.target, testCase.body)
			if recorder.Code != testCase.wantStatus {
				t.Fatalf(
					"status = %d, want %d (body %s)",
					recorder.Code, testCase.wantStatus, recorder.Body.String(),
				)
			}
		})
	}
}

// The composer's read has to work for an ordinary member, or nobody but an
// administrator could point a chat anywhere.
func TestAgentEndpointChoicesAreReadableByAnySignedInUser(t *testing.T) {
	stub := &agentEndpointsStub{choices: []serviceendpoints.Choice{{
		ID:     "zhipu-glm",
		Label:  "Zhipu GLM",
		CLI:    serviceendpoints.CLIClaude,
		Models: []serviceendpoints.Model{{ID: "glm-4.6", Label: "GLM-4.6"}},
	}}}
	handler := newAgentEndpointsHandler(stub, callerStub{email: "member@example.com"})

	recorder := serveAgentEndpoints(t, handler, http.MethodGet, "/api/agent-endpoints", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}

	var payload struct {
		Endpoints []serviceendpoints.Choice `json:"endpoints"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Endpoints) != 1 || payload.Endpoints[0].ID != "zhipu-glm" {
		t.Fatalf("choices = %+v, want the one enabled profile", payload.Endpoints)
	}

	// The member-facing shape must describe what to pick, never how to reach
	// it: no base URL, no key reference anywhere in the body.
	body := recorder.Body.String()
	for _, forbidden := range []string{"baseUrl", "apiKeyRef", "bigmodel.cn"} {
		if strings.Contains(body, forbidden) {
			t.Errorf("the member-facing payload leaks %q:\n%s", forbidden, body)
		}
	}
}

// A deployment with no register must not break the composer: it simply offers
// no third-party section.
func TestAgentEndpointChoicesDegradeToAnEmptyList(t *testing.T) {
	handler := newAgentEndpointsHandler(nil, callerStub{email: "member@example.com"})

	recorder := serveAgentEndpoints(t, handler, http.MethodGet, "/api/agent-endpoints", "")
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", recorder.Code)
	}
	var payload struct {
		Endpoints []serviceendpoints.Choice `json:"endpoints"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Endpoints) != 0 {
		t.Errorf("endpoints = %+v, want an empty list", payload.Endpoints)
	}
}

// A deployment with no register must refuse the admin routes loudly rather
// than pretending the register is empty.
func TestAgentEndpointAdminRoutesReportUnavailable(t *testing.T) {
	handler := newAgentEndpointsHandler(nil, callerStub{email: "admin@example.com", isAdmin: true})

	recorder := serveAgentEndpoints(t, handler, http.MethodGet, "/api/admin/agent-endpoints", "")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

// The admin's identity is what the audit log attributes an edit to, so it has
// to reach the service rather than being dropped at the edge.
func TestAgentEndpointWritesCarryTheCallersIdentity(t *testing.T) {
	stub := &agentEndpointsStub{}
	handler := newAgentEndpointsHandler(stub, callerStub{email: "admin@example.com", isAdmin: true})

	recorder := serveAgentEndpoints(
		t, handler, http.MethodPost, "/api/admin/agent-endpoints",
		`{"id":"zhipu-glm","label":"Zhipu GLM","cli":"claude",`+
			`"baseUrl":"https://open.bigmodel.cn/api/anthropic","apiKeyRef":"ZHIPU_API_KEY","enabled":true}`,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}
	if len(stub.created) != 1 {
		t.Fatalf("created %d profiles, want 1", len(stub.created))
	}
	if stub.created[0].APIKeyRef != "ZHIPU_API_KEY" {
		t.Errorf("apiKeyRef = %q, want the reference the admin sent", stub.created[0].APIKeyRef)
	}
	if len(stub.actors) != 1 || stub.actors[0] != "admin@example.com" {
		t.Errorf("actors = %v, want the calling admin", stub.actors)
	}
}

// The probe names both the project it runs in and the model it asks for; both
// have to survive the edge.
func TestAgentEndpointTestForwardsProjectAndModel(t *testing.T) {
	stub := &agentEndpointsStub{result: serviceendpoints.TestResult{OK: true, Output: "ready"}}
	handler := newAgentEndpointsHandler(stub, callerStub{email: "admin@example.com", isAdmin: true})

	recorder := serveAgentEndpoints(
		t, handler, http.MethodPost, "/api/admin/agent-endpoints/zhipu-glm/test",
		`{"projectId":"  p1  ","model":"glm-4.6"}`,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", recorder.Code, recorder.Body.String())
	}
	if len(stub.tested) != 1 {
		t.Fatalf("ran %d probes, want 1", len(stub.tested))
	}
	if stub.tested[0] != [3]string{"zhipu-glm", "p1", "glm-4.6"} {
		t.Errorf("probe args = %v, want the trimmed id, project and model", stub.tested[0])
	}
}
