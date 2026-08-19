package httphandlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	servicemcp "github.com/futrx-com/remote.futrx.com/internal/service/mcp"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

type mcpStub struct {
	list        []servicemcp.View
	created     []servicemcp.Server
	updated     []servicemcp.Server
	deleted     []string
	tested      [][2]string
	savedFor    []string
	savedInput  []servicemcp.ProjectInput
	actors      []string
	writeErr    error
	projectView servicemcp.ProjectView
	result      servicemcp.TestResult
}

func (s *mcpStub) List(context.Context) ([]servicemcp.View, error) {
	return append([]servicemcp.View(nil), s.list...), nil
}

func (s *mcpStub) Create(
	_ context.Context,
	server servicemcp.Server,
	actor string,
) (servicemcp.View, error) {
	s.created = append(s.created, server)
	s.actors = append(s.actors, actor)
	if s.writeErr != nil {
		return servicemcp.View{}, s.writeErr
	}
	return servicemcp.View{Server: server}, nil
}

func (s *mcpStub) Update(
	_ context.Context,
	name string,
	server servicemcp.Server,
	actor string,
) (servicemcp.View, error) {
	server.Name = name
	s.updated = append(s.updated, server)
	s.actors = append(s.actors, actor)
	if s.writeErr != nil {
		return servicemcp.View{}, s.writeErr
	}
	return servicemcp.View{Server: server}, nil
}

func (s *mcpStub) Delete(_ context.Context, name string) error {
	s.deleted = append(s.deleted, name)
	return s.writeErr
}

func (s *mcpStub) Test(_ context.Context, name, projectID string) (servicemcp.TestResult, error) {
	s.tested = append(s.tested, [2]string{name, projectID})
	if s.writeErr != nil {
		return servicemcp.TestResult{}, s.writeErr
	}
	return s.result, nil
}

func (s *mcpStub) ProjectSettings(_ context.Context, projectID string) (servicemcp.ProjectView, error) {
	s.savedFor = append(s.savedFor, projectID)
	return s.projectView, s.writeErr
}

func (s *mcpStub) SaveProjectSettings(
	_ context.Context,
	projectID string,
	input servicemcp.ProjectInput,
	actor string,
) (servicemcp.ProjectView, error) {
	s.savedFor = append(s.savedFor, projectID)
	s.savedInput = append(s.savedInput, input)
	s.actors = append(s.actors, actor)
	return s.projectView, s.writeErr
}

func newMCPHandler(service MCPService, caller CallerResolver) *MCPHandler {
	return &MCPHandler{mcp: service, caller: caller}
}

func serveMCP(
	t *testing.T,
	handler *MCPHandler,
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

func TestMCPAdminRoutesAreAdminOnly(t *testing.T) {
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
			target:     "/api/admin/mcp-servers",
			caller:     callerStub{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "a member may not list the registry",
			method:     http.MethodGet,
			target:     "/api/admin/mcp-servers",
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "an admin may list the registry",
			method:     http.MethodGet,
			target:     "/api/admin/mcp-servers",
			caller:     callerStub{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusOK,
		},
		{
			name:       "a member may not add an entry",
			method:     http.MethodPost,
			target:     "/api/admin/mcp-servers",
			body:       `{"name":"fetch","transport":"stdio","command":"uvx","scope":{"all":true}}`,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "a member may not edit an entry",
			method:     http.MethodPut,
			target:     "/api/admin/mcp-servers/fetch",
			body:       `{"transport":"stdio","command":"uvx","scope":{"all":true}}`,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "a member may not delete an entry",
			method:     http.MethodDelete,
			target:     "/api/admin/mcp-servers/fetch",
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "a member may not run the probe",
			method:     http.MethodPost,
			target:     "/api/admin/mcp-servers/fetch/test",
			body:       `{"projectId":"p1"}`,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "an admin may run the probe",
			method:     http.MethodPost,
			target:     "/api/admin/mcp-servers/fetch/test",
			body:       `{"projectId":"p1"}`,
			caller:     callerStub{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusOK,
		},
		{
			name:       "an unknown sub-route is a 404, not an action",
			method:     http.MethodPost,
			target:     "/api/admin/mcp-servers/fetch/exec",
			caller:     callerStub{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "a missing name is a bad request",
			method:     http.MethodDelete,
			target:     "/api/admin/mcp-servers/",
			caller:     callerStub{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler := newMCPHandler(&mcpStub{}, tt.caller)
			recorder := serveMCP(t, handler, tt.method, tt.target, tt.body)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestMCPRoutesReport503WithoutARegistry(t *testing.T) {
	handler := newMCPHandler(nil, callerStub{email: "admin@example.com", isAdmin: true})
	recorder := serveMCP(t, handler, http.MethodGet, "/api/admin/mcp-servers", "")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func TestMCPCollectionListsTheProviderSupportMatrix(t *testing.T) {
	handler := newMCPHandler(&mcpStub{}, callerStub{email: "admin@example.com", isAdmin: true})
	recorder := serveMCP(t, handler, http.MethodGet, "/api/admin/mcp-servers", "")

	var body struct {
		Servers              []servicemcp.View `json:"servers"`
		SupportedProviders   []string          `json:"supportedProviders"`
		UnsupportedProviders []string          `json:"unsupportedProviders"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Servers == nil {
		t.Fatalf("servers should be an empty array, not null: %s", recorder.Body.String())
	}
	if len(body.SupportedProviders) == 0 || len(body.UnsupportedProviders) == 0 {
		t.Fatalf("provider matrix = %#v", body)
	}
}

func TestMCPWritesCarryTheCallerAsTheActor(t *testing.T) {
	service := &mcpStub{}
	handler := newMCPHandler(service, callerStub{email: "admin@example.com", isAdmin: true})

	serveMCP(t, handler, http.MethodPost, "/api/admin/mcp-servers",
		`{"name":"fetch","transport":"stdio","command":"uvx","scope":{"all":true}}`)
	if len(service.created) != 1 || service.created[0].Name != "fetch" {
		t.Fatalf("created = %#v", service.created)
	}
	if len(service.actors) != 1 || service.actors[0] != "admin@example.com" {
		t.Fatalf("actors = %v", service.actors)
	}

	serveMCP(t, handler, http.MethodPut, "/api/admin/mcp-servers/fetch",
		`{"transport":"stdio","command":"uvx","scope":{"all":true}}`)
	if len(service.updated) != 1 || service.updated[0].Name != "fetch" {
		t.Fatalf("updated = %#v", service.updated)
	}
}

func TestMCPProjectResourceServesMembersAndRejectsUnknownActions(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		rest       string
		body       string
		wantStatus int
	}{
		{name: "read", method: http.MethodGet, wantStatus: http.StatusOK},
		{
			name: "write", method: http.MethodPut,
			body:       `{"disabled":["fetch"],"servers":[]}`,
			wantStatus: http.StatusOK,
		},
		{name: "unknown sub-route", method: http.MethodGet, rest: "probe", wantStatus: http.StatusNotFound},
		{name: "unsupported method", method: http.MethodPost, wantStatus: http.StatusMethodNotAllowed},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &mcpStub{}
			handler := newMCPHandler(service, callerStub{email: "member@example.com"})
			request := httptest.NewRequest(tt.method, "/api/projects/p1/mcp", strings.NewReader(tt.body))
			recorder := httptest.NewRecorder()
			handler.HandleProjectResource(
				recorder, request, serviceproject.ID("p1"), tt.rest, "member@example.com",
			)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)",
					recorder.Code, tt.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestMCPProjectWriteForwardsTheMemberAndTheirInput(t *testing.T) {
	service := &mcpStub{}
	handler := newMCPHandler(service, callerStub{email: "member@example.com"})
	request := httptest.NewRequest(http.MethodPut, "/api/projects/p1/mcp", strings.NewReader(
		`{"disabled":["fetch"],"servers":[{"name":"shop","transport":"http","url":"https://s.example.com/mcp"}]}`,
	))
	handler.HandleProjectResource(
		httptest.NewRecorder(), request, serviceproject.ID("p1"), "", "member@example.com",
	)

	if len(service.savedInput) != 1 {
		t.Fatalf("savedInput = %#v", service.savedInput)
	}
	input := service.savedInput[0]
	if len(input.Disabled) != 1 || input.Disabled[0] != "fetch" {
		t.Errorf("disabled = %v", input.Disabled)
	}
	if len(input.Servers) != 1 || input.Servers[0].Name != "shop" ||
		input.Servers[0].Transport != servicemcp.TransportHTTP {
		t.Errorf("servers = %#v", input.Servers)
	}
	if len(service.actors) != 1 || service.actors[0] != "member@example.com" {
		t.Errorf("actors = %v", service.actors)
	}
}

func TestMCPProjectResourceReports503WithoutARegistry(t *testing.T) {
	handler := newMCPHandler(nil, callerStub{email: "member@example.com"})
	recorder := httptest.NewRecorder()
	handler.HandleProjectResource(
		recorder,
		httptest.NewRequest(http.MethodGet, "/api/projects/p1/mcp", nil),
		serviceproject.ID("p1"), "", "member@example.com",
	)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}
