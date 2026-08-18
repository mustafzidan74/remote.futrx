package httphandlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	servicenotify "github.com/futrx-com/remote.futrx.com/internal/service/notify"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileproject"
)

type clientMessageStub struct {
	configured bool
	sent       []servicenotify.Event
}

func (s *clientMessageStub) SinksConfigured() bool { return s.configured }

func (s *clientMessageStub) SendMessage(
	_ context.Context,
	event servicenotify.Event,
) []servicenotify.SinkResult {
	s.sent = append(s.sent, event)
	return []servicenotify.SinkResult{{Sink: "telegram", Configured: true, Delivered: true}}
}

func newClientMessageHandler(t *testing.T, sender ClientMessageService) (*ProjectHandler, serviceproject.Meta) {
	t.Helper()
	dataDir := t.TempDir()
	repo, err := fileproject.NewWithWorkspaceRoot(dataDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	projects := serviceproject.New(repo, serviceproject.ContainerDependencies{}, nil, nil)
	project, err := projects.Create(
		context.Background(), serviceproject.CreateInput{Name: "Acme Shop"}, "owner@example.com",
	)
	if err != nil {
		t.Fatal(err)
	}
	handler := NewProjectHandler(projects, nil, nil, "remote.example.com").WithClientMessages(sender)
	return handler, project
}

func clientMessageRequest(
	handler *ProjectHandler,
	method, path, body string,
) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	recorder := httptest.NewRecorder()
	handler.HandleResource(recorder, request)
	return recorder
}

func TestClientMessageSendsResolvedText(t *testing.T) {
	sender := &clientMessageStub{configured: true}
	handler, project := newClientMessageHandler(t, sender)
	path := "/api/projects/" + string(project.ID) + "/client-message"

	recorder := clientMessageRequest(handler, http.MethodPost, path,
		`{"text":"Hello Acme, the site is live.","url":"https://remote.example.com/portal/x?t=y"}`)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body.String())
	}
	var response struct {
		Configured bool                       `json:"configured"`
		Delivered  []servicenotify.SinkResult `json:"delivered"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !response.Configured || len(response.Delivered) != 1 || !response.Delivered[0].Delivered {
		t.Fatalf("response = %+v, want one delivered sink", response)
	}
	if len(sender.sent) != 1 {
		t.Fatalf("sent %d events, want 1", len(sender.sent))
	}
	event := sender.sent[0]
	switch {
	case event.Summary != "Hello Acme, the site is live.":
		t.Fatalf("summary = %q, want the text unchanged", event.Summary)
	case event.ProjectName != "Acme Shop":
		t.Fatalf("project name = %q", event.ProjectName)
	case event.URL == "":
		t.Fatal("the portal link was dropped")
	}
}

func TestClientMessageRefusesWhatItCannotSend(t *testing.T) {
	tests := []struct {
		name       string
		sender     ClientMessageService
		method     string
		body       string
		wantStatus int
	}{
		{
			name:       "no notification service at all",
			sender:     nil,
			method:     http.MethodPost,
			body:       `{"text":"hello"}`,
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "no sink configured",
			sender:     &clientMessageStub{},
			method:     http.MethodPost,
			body:       `{"text":"hello"}`,
			wantStatus: http.StatusServiceUnavailable,
		},
		{
			name:       "an empty message",
			sender:     &clientMessageStub{configured: true},
			method:     http.MethodPost,
			body:       `{"text":"   "}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "a message past the cap",
			sender:     &clientMessageStub{configured: true},
			method:     http.MethodPost,
			body:       `{"text":"` + strings.Repeat("x", maxClientMessageLength+1) + `"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "malformed json",
			sender:     &clientMessageStub{configured: true},
			method:     http.MethodPost,
			body:       `{`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "an unknown verb",
			sender:     &clientMessageStub{configured: true},
			method:     http.MethodDelete,
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, project := newClientMessageHandler(t, tt.sender)
			path := "/api/projects/" + string(project.ID) + "/client-message"
			recorder := clientMessageRequest(handler, tt.method, path, tt.body)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if stub, ok := tt.sender.(*clientMessageStub); ok && len(stub.sent) != 0 {
				t.Fatalf("a refused request still sent %d messages", len(stub.sent))
			}
		})
	}
}

func TestClientMessageReportsWhetherASinkExists(t *testing.T) {
	for _, configured := range []bool{true, false} {
		handler, project := newClientMessageHandler(t, &clientMessageStub{configured: configured})
		path := "/api/projects/" + string(project.ID) + "/client-message"
		recorder := clientMessageRequest(handler, http.MethodGet, path, "")
		if recorder.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", recorder.Code)
		}
		var response struct {
			Configured bool `json:"configured"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if response.Configured != configured {
			t.Fatalf("configured = %v, want %v", response.Configured, configured)
		}
	}
}
