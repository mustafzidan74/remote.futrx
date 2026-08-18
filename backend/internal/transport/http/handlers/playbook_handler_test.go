package httphandlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	serviceplaybooks "github.com/futrx-com/remote.futrx.com/internal/service/playbooks"
)

type playbookServiceStub struct {
	list        []serviceplaybooks.Playbook
	listErr     error
	replaced    []serviceplaybooks.Playbook
	replaceErr  error
	replaceActs []string
}

func (s *playbookServiceStub) List(context.Context) ([]serviceplaybooks.Playbook, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]serviceplaybooks.Playbook(nil), s.list...), nil
}

func (s *playbookServiceStub) Replace(
	_ context.Context,
	list []serviceplaybooks.Playbook,
	actor string,
) ([]serviceplaybooks.Playbook, error) {
	s.replaceActs = append(s.replaceActs, actor)
	if s.replaceErr != nil {
		return nil, s.replaceErr
	}
	s.replaced = append([]serviceplaybooks.Playbook(nil), list...)
	return s.replaced, nil
}

func newPlaybookHandler(service PlaybookService, caller CallerResolver) *PlaybookHandler {
	return &PlaybookHandler{playbooks: service, caller: caller}
}

func TestPlaybookHandlerAuthorization(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		target     string
		body       string
		caller     callerStub
		wantStatus int
	}{
		{
			name:       "anonymous read is refused",
			method:     http.MethodGet,
			target:     "/api/playbooks",
			caller:     callerStub{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "any signed-in member may read the library",
			method:     http.MethodGet,
			target:     "/api/playbooks",
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusOK,
		},
		{
			name:       "members cannot write the library",
			method:     http.MethodPut,
			target:     "/api/admin/playbooks",
			body:       `{"playbooks":[]}`,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "anonymous write is refused before role checks",
			method:     http.MethodPut,
			target:     "/api/admin/playbooks",
			body:       `{"playbooks":[]}`,
			caller:     callerStub{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "members cannot read through the admin route",
			method:     http.MethodGet,
			target:     "/api/admin/playbooks",
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "admins may write the library",
			method:     http.MethodPut,
			target:     "/api/admin/playbooks",
			body:       `{"playbooks":[{"id":"one","title":"One","prompt":"p"}]}`,
			caller:     callerStub{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusOK,
		},
		{
			name:       "the read route refuses writes",
			method:     http.MethodPut,
			target:     "/api/playbooks",
			body:       `{"playbooks":[]}`,
			caller:     callerStub{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "the admin route refuses unknown verbs",
			method:     http.MethodDelete,
			target:     "/api/admin/playbooks",
			caller:     callerStub{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service := &playbookServiceStub{}
			handler := newPlaybookHandler(service, tt.caller)
			mux := http.NewServeMux()
			handler.RegisterRoutes(mux)

			var body *strings.Reader
			if tt.body != "" {
				body = strings.NewReader(tt.body)
			} else {
				body = strings.NewReader("")
			}
			req := httptest.NewRequest(tt.method, tt.target, body)
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("%s %s = %d, want %d (body %s)", tt.method, tt.target, rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantStatus != http.StatusOK && len(service.replaced) > 0 {
				t.Fatal("a refused request reached the service")
			}
		})
	}
}

func TestPlaybookHandlerReadReturnsCollection(t *testing.T) {
	service := &playbookServiceStub{list: []serviceplaybooks.Playbook{
		{ID: "security-review", Title: "🔒 Security review", Prompt: "review it", Order: 0},
	}}
	handler := newPlaybookHandler(service, callerStub{email: "member@example.com"})
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/playbooks", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var payload struct {
		Playbooks []serviceplaybooks.Playbook `json:"playbooks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Playbooks) != 1 || payload.Playbooks[0].ID != "security-review" {
		t.Fatalf("payload = %#v", payload.Playbooks)
	}
}

func TestPlaybookHandlerPassesTheAdminEmailAsActor(t *testing.T) {
	service := &playbookServiceStub{}
	handler := newPlaybookHandler(service, callerStub{email: "admin@example.com", isAdmin: true})
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodPut,
		"/api/admin/playbooks",
		strings.NewReader(`{"playbooks":[{"id":"one","title":"One","prompt":"p"}]}`),
	)
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}
	if len(service.replaceActs) != 1 || service.replaceActs[0] != "admin@example.com" {
		t.Fatalf("actor = %#v, want the admin email", service.replaceActs)
	}
}

func TestPlaybookHandlerMapsValidationFailureTo400(t *testing.T) {
	service := &playbookServiceStub{replaceErr: serviceplaybooks.ErrInvalidPlaybooks}
	handler := newPlaybookHandler(service, callerStub{email: "admin@example.com", isAdmin: true})
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(
		http.MethodPut,
		"/api/admin/playbooks",
		strings.NewReader(`{"playbooks":[{"id":"","title":"","prompt":""}]}`),
	))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", rec.Code, rec.Body.String())
	}
}

func TestPlaybookHandlerReportsUnavailableService(t *testing.T) {
	handler := newPlaybookHandler(nil, callerStub{email: "admin@example.com", isAdmin: true})
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/playbooks", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}
