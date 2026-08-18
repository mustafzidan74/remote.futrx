package httphandlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	serviceagentprefs "github.com/futrx-com/remote.futrx.com/internal/service/agentprefs"
)

type agentPreferencesStub struct {
	prefs      serviceagentprefs.Preferences
	updateErr  error
	updates    []serviceagentprefs.UpdateInput
	updateActs []string
}

func (s *agentPreferencesStub) Get(context.Context) (serviceagentprefs.Preferences, error) {
	return s.prefs, nil
}

func (s *agentPreferencesStub) Update(
	_ context.Context,
	input serviceagentprefs.UpdateInput,
	actor string,
) (serviceagentprefs.Preferences, error) {
	s.updates = append(s.updates, input)
	s.updateActs = append(s.updateActs, actor)
	if s.updateErr != nil {
		return serviceagentprefs.Preferences{}, s.updateErr
	}
	if input.ReplyLanguage != nil {
		s.prefs.ReplyLanguage = *input.ReplyLanguage
	}
	return s.prefs, nil
}

func newAgentPreferencesHandler(
	service AgentPreferencesService,
	caller CallerResolver,
) *AgentPreferencesHandler {
	return &AgentPreferencesHandler{prefs: service, caller: caller}
}

func TestAgentPreferencesHandlerAuthorization(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		body       string
		caller     callerStub
		wantStatus int
	}{
		{
			name:       "anonymous read is refused",
			method:     http.MethodGet,
			caller:     callerStub{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "members cannot read the platform document",
			method:     http.MethodGet,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "admins can read",
			method:     http.MethodGet,
			caller:     callerStub{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusOK,
		},
		{
			name:       "anonymous write is refused before role checks",
			method:     http.MethodPut,
			body:       `{"replyLanguage":"ar-EG"}`,
			caller:     callerStub{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "members cannot write",
			method:     http.MethodPut,
			body:       `{"replyLanguage":"ar-EG"}`,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusForbidden,
		},
		{
			name:       "admins can write",
			method:     http.MethodPut,
			body:       `{"replyLanguage":"ar-EG"}`,
			caller:     callerStub{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusOK,
		},
		{
			name:       "malformed json is a bad request",
			method:     http.MethodPut,
			body:       `{`,
			caller:     callerStub{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "other verbs are refused",
			method:     http.MethodDelete,
			caller:     callerStub{email: "admin@example.com", isAdmin: true},
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &agentPreferencesStub{prefs: serviceagentprefs.Defaults()}
			handler := newAgentPreferencesHandler(service, test.caller)

			var body *strings.Reader
			if test.body != "" {
				body = strings.NewReader(test.body)
			} else {
				body = strings.NewReader("")
			}
			request := httptest.NewRequest(test.method, "/api/admin/agent-preferences", body)
			recorder := httptest.NewRecorder()
			handler.handleDocument(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, test.wantStatus, recorder.Body)
			}
			if test.wantStatus != http.StatusOK && len(service.updates) > 0 {
				t.Errorf("a refused request still reached the service")
			}
		})
	}
}

func TestAgentPreferencesHandlerRoundTrip(t *testing.T) {
	service := &agentPreferencesStub{prefs: serviceagentprefs.Preferences{
		ReplyLanguage: serviceagentprefs.LanguageEgyptianArabic,
		Tone:          serviceagentprefs.ToneConcise,
		ApplyTo:       serviceagentprefs.ApplyToAll,
	}}
	handler := newAgentPreferencesHandler(
		service,
		callerStub{email: "Admin@example.com", isAdmin: true},
	)

	recorder := httptest.NewRecorder()
	handler.handleDocument(
		recorder,
		httptest.NewRequest(http.MethodGet, "/api/admin/agent-preferences", nil),
	)
	var got serviceagentprefs.Preferences
	if err := json.Unmarshal(recorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ReplyLanguage != serviceagentprefs.LanguageEgyptianArabic || got.Tone != serviceagentprefs.ToneConcise {
		t.Fatalf("GET body = %+v", got)
	}

	recorder = httptest.NewRecorder()
	handler.handleDocument(recorder, httptest.NewRequest(
		http.MethodPut,
		"/api/admin/agent-preferences",
		strings.NewReader(`{"replyLanguage":"en","extraInstructions":"Never force-push."}`),
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("PUT status = %d (body %s)", recorder.Code, recorder.Body)
	}
	if len(service.updates) != 1 {
		t.Fatalf("service saw %d updates, want 1", len(service.updates))
	}
	if service.updates[0].ReplyLanguage == nil || *service.updates[0].ReplyLanguage != "en" {
		t.Errorf("update reply language = %v", service.updates[0].ReplyLanguage)
	}
	if service.updates[0].ExtraInstructions == nil {
		t.Error("extra instructions did not reach the service")
	}
	if service.updates[0].Tone != nil {
		t.Error("an absent field was sent as an edit")
	}
	if service.updateActs[0] != "Admin@example.com" {
		t.Errorf("actor = %q, want the caller's email", service.updateActs[0])
	}
}

func TestAgentPreferencesHandlerWithoutServiceIsUnavailable(t *testing.T) {
	handler := newAgentPreferencesHandler(nil, callerStub{email: "admin@example.com", isAdmin: true})
	recorder := httptest.NewRecorder()
	handler.handleDocument(
		recorder,
		httptest.NewRequest(http.MethodGet, "/api/admin/agent-preferences", nil),
	)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}
