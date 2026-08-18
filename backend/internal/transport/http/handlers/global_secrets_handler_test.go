package httphandlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	serviceglobalsecrets "github.com/futrx-com/remote.futrx.com/internal/service/globalsecrets"
)

type globalSecretsStub struct {
	list     []serviceglobalsecrets.View
	listErr  error
	created  []serviceglobalsecrets.Input
	updated  []serviceglobalsecrets.Input
	deleted  []string
	tested   []string
	actors   []string
	writeErr error
	result   serviceglobalsecrets.TestResult
}

func (s *globalSecretsStub) List(context.Context) ([]serviceglobalsecrets.View, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return append([]serviceglobalsecrets.View(nil), s.list...), nil
}

func (s *globalSecretsStub) Create(
	_ context.Context,
	input serviceglobalsecrets.Input,
	actor string,
) (serviceglobalsecrets.View, error) {
	s.created = append(s.created, input)
	s.actors = append(s.actors, actor)
	if s.writeErr != nil {
		return serviceglobalsecrets.View{}, s.writeErr
	}
	return serviceglobalsecrets.View{Key: input.Key, Kind: input.Kind}, nil
}

func (s *globalSecretsStub) Update(
	_ context.Context,
	key string,
	input serviceglobalsecrets.Input,
	actor string,
) (serviceglobalsecrets.View, error) {
	input.Key = key
	s.updated = append(s.updated, input)
	s.actors = append(s.actors, actor)
	if s.writeErr != nil {
		return serviceglobalsecrets.View{}, s.writeErr
	}
	return serviceglobalsecrets.View{Key: key, Kind: input.Kind}, nil
}

func (s *globalSecretsStub) Delete(_ context.Context, key string) error {
	s.deleted = append(s.deleted, key)
	return s.writeErr
}

func (s *globalSecretsStub) TestSSH(_ context.Context, key string) (serviceglobalsecrets.TestResult, error) {
	s.tested = append(s.tested, key)
	if s.writeErr != nil {
		return serviceglobalsecrets.TestResult{}, s.writeErr
	}
	return s.result, nil
}

func newGlobalSecretsHandler(service GlobalSecretsService, caller CallerResolver) *GlobalSecretsHandler {
	return &GlobalSecretsHandler{secrets: service, caller: caller}
}

func serveSecrets(
	t *testing.T,
	handler *GlobalSecretsHandler,
	method, target, body string,
) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, target, reader)
	recorder := httptest.NewRecorder()
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	mux.ServeHTTP(recorder, request)
	return recorder
}

func TestGlobalSecretsHandlerIsAdminOnly(t *testing.T) {
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
			target:     "/api/admin/secrets",
			caller:     callerStub{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "a member may not list the vault",
			method:     http.MethodGet,
			target:     "/api/admin/secrets",
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "an admin may list the vault",
			method:     http.MethodGet,
			target:     "/api/admin/secrets",
			caller:     callerStub{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusOK,
		},
		{
			name:       "a member may not create an entry",
			method:     http.MethodPost,
			target:     "/api/admin/secrets",
			body:       `{"key":"GITHUB_TOKEN","kind":"env","scope":{"all":true}}`,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "a member may not update an entry",
			method:     http.MethodPut,
			target:     "/api/admin/secrets/GITHUB_TOKEN",
			body:       `{"kind":"env","scope":{"all":true}}`,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "a member may not delete an entry",
			method:     http.MethodDelete,
			target:     "/api/admin/secrets/GITHUB_TOKEN",
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "a member may not test an ssh target",
			method:     http.MethodPost,
			target:     "/api/admin/secrets/HESTIA/test",
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "anonymous test is refused before the role check",
			method:     http.MethodPost,
			target:     "/api/admin/secrets/HESTIA/test",
			caller:     callerStub{},
			wantStatus: http.StatusUnauthorized,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &globalSecretsStub{}
			handler := newGlobalSecretsHandler(service, test.caller)
			recorder := serveSecrets(t, handler, test.method, test.target, test.body)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if test.wantStatus != http.StatusOK {
				if len(service.created)+len(service.updated)+len(service.deleted)+len(service.tested) != 0 {
					t.Fatal("a refused request reached the service")
				}
			}
		})
	}
}

func TestGlobalSecretsHandlerRoutesWritesToTheVault(t *testing.T) {
	admin := callerStub{email: "admin@example.com", isAdmin: true}

	t.Run("create", func(t *testing.T) {
		service := &globalSecretsStub{}
		handler := newGlobalSecretsHandler(service, admin)
		recorder := serveSecrets(t, handler, http.MethodPost, "/api/admin/secrets",
			`{"key":" GITHUB_TOKEN ","kind":"env","value":"ghp_x","scope":{"all":true},"description":"gh"}`)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d (%s)", recorder.Code, recorder.Body.String())
		}
		if len(service.created) != 1 {
			t.Fatalf("created = %+v", service.created)
		}
		if service.created[0].Key != "GITHUB_TOKEN" {
			t.Fatalf("key = %q", service.created[0].Key)
		}
		if service.created[0].Kind != serviceglobalsecrets.KindEnv {
			t.Fatalf("kind = %q", service.created[0].Kind)
		}
		if service.actors[0] != "admin@example.com" {
			t.Fatalf("actor = %q", service.actors[0])
		}
	})

	t.Run("update carries the clear flag and the path key", func(t *testing.T) {
		service := &globalSecretsStub{}
		handler := newGlobalSecretsHandler(service, admin)
		recorder := serveSecrets(t, handler, http.MethodPut, "/api/admin/secrets/NPMRC",
			`{"kind":"file","path":"/root/.npmrc","clear":true,"scope":{"projectIds":["p1"]}}`)
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d (%s)", recorder.Code, recorder.Body.String())
		}
		if len(service.updated) != 1 || service.updated[0].Key != "NPMRC" {
			t.Fatalf("updated = %+v", service.updated)
		}
		if !service.updated[0].Clear || service.updated[0].Path != "/root/.npmrc" {
			t.Fatalf("update input = %+v", service.updated[0])
		}
		if len(service.updated[0].Scope.ProjectIDs) != 1 {
			t.Fatalf("scope = %+v", service.updated[0].Scope)
		}
	})

	t.Run("delete", func(t *testing.T) {
		service := &globalSecretsStub{}
		handler := newGlobalSecretsHandler(service, admin)
		recorder := serveSecrets(t, handler, http.MethodDelete, "/api/admin/secrets/NPMRC", "")
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d", recorder.Code)
		}
		if len(service.deleted) != 1 || service.deleted[0] != "NPMRC" {
			t.Fatalf("deleted = %v", service.deleted)
		}
	})

	t.Run("test returns the probe result", func(t *testing.T) {
		service := &globalSecretsStub{result: serviceglobalsecrets.TestResult{
			OK: true, Output: "ok", LatencyMS: 91,
		}}
		handler := newGlobalSecretsHandler(service, admin)
		recorder := serveSecrets(t, handler, http.MethodPost, "/api/admin/secrets/HESTIA/test", "")
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d (%s)", recorder.Code, recorder.Body.String())
		}
		var result serviceglobalsecrets.TestResult
		if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if !result.OK || result.LatencyMS != 91 {
			t.Fatalf("result = %+v", result)
		}
		if len(service.tested) != 1 || service.tested[0] != "HESTIA" {
			t.Fatalf("tested = %v", service.tested)
		}
	})
}

func TestGlobalSecretsHandlerRejectsUnusableRequests(t *testing.T) {
	admin := callerStub{email: "admin@example.com", isAdmin: true}
	tests := []struct {
		name       string
		method     string
		target     string
		body       string
		service    *globalSecretsStub
		wantStatus int
	}{
		{
			name:       "unsupported collection method",
			method:     http.MethodDelete,
			target:     "/api/admin/secrets",
			service:    &globalSecretsStub{},
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "unsupported item method",
			method:     http.MethodPatch,
			target:     "/api/admin/secrets/TOKEN",
			service:    &globalSecretsStub{},
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "test only accepts POST",
			method:     http.MethodGet,
			target:     "/api/admin/secrets/TOKEN/test",
			service:    &globalSecretsStub{},
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "unknown sub-resource",
			method:     http.MethodPost,
			target:     "/api/admin/secrets/TOKEN/rotate",
			service:    &globalSecretsStub{},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "missing key",
			method:     http.MethodPut,
			target:     "/api/admin/secrets/",
			body:       `{"kind":"env"}`,
			service:    &globalSecretsStub{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid json",
			method:     http.MethodPost,
			target:     "/api/admin/secrets",
			body:       `{`,
			service:    &globalSecretsStub{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "duplicate key",
			method:     http.MethodPost,
			target:     "/api/admin/secrets",
			body:       `{"key":"TOKEN","kind":"env","scope":{"all":true}}`,
			service:    &globalSecretsStub{writeErr: serviceglobalsecrets.ErrExists},
			wantStatus: http.StatusConflict,
		},
		{
			name:       "unknown key",
			method:     http.MethodDelete,
			target:     "/api/admin/secrets/TOKEN",
			service:    &globalSecretsStub{writeErr: serviceglobalsecrets.ErrNotFound},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "rejected input",
			method:     http.MethodPost,
			target:     "/api/admin/secrets",
			body:       `{"key":"TOKEN","kind":"env","scope":{"all":true},"value":"a\nb"}`,
			service:    &globalSecretsStub{writeErr: serviceglobalsecrets.ErrMultilineEnvValue},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "no ssh client on the host",
			method:     http.MethodPost,
			target:     "/api/admin/secrets/HESTIA/test",
			service:    &globalSecretsStub{writeErr: serviceglobalsecrets.ErrProbeUnavailable},
			wantStatus: http.StatusServiceUnavailable,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			handler := newGlobalSecretsHandler(test.service, admin)
			recorder := serveSecrets(t, handler, test.method, test.target, test.body)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d (%s)", recorder.Code, test.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestGlobalSecretsHandlerReportsAnUnavailableVault(t *testing.T) {
	handler := newGlobalSecretsHandler(nil, callerStub{email: "admin@example.com", isAdmin: true})
	recorder := serveSecrets(t, handler, http.MethodGet, "/api/admin/secrets", "")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d", recorder.Code)
	}
}

func TestGlobalSecretsListingIsAlwaysAnObjectWithASecretsArray(t *testing.T) {
	service := &globalSecretsStub{list: []serviceglobalsecrets.View{
		{Key: "GITHUB_TOKEN", Kind: serviceglobalsecrets.KindEnv, Masked: "••••••••1234", HasValue: true},
	}}
	handler := newGlobalSecretsHandler(service, callerStub{email: "admin@example.com", isAdmin: true})
	recorder := serveSecrets(t, handler, http.MethodGet, "/api/admin/secrets", "")

	var body struct {
		Secrets []serviceglobalsecrets.View `json:"secrets"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Secrets) != 1 || body.Secrets[0].Masked != "••••••••1234" {
		t.Fatalf("body = %s", recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `"value"`) {
		t.Fatalf("a listing must never carry a value field: %s", recorder.Body.String())
	}
}
