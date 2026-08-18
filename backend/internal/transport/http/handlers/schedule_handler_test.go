package httphandlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	serviceschedule "github.com/futrx-com/remote.futrx.com/internal/service/schedule"
	serviceschedulecapability "github.com/futrx-com/remote.futrx.com/internal/service/schedulecapability"
)

const (
	scheduleIDOne   = serviceschedule.ID("000000000000000000000001")
	scheduleIDTwo   = serviceschedule.ID("000000000000000000000002")
	scheduleIDThree = serviceschedule.ID("000000000000000000000003")
)

type scheduleServiceStub struct {
	tasks []serviceschedule.Task

	listErr     error
	getErr      error
	createErr   error
	updateErr   error
	deleteErr   error
	runNowErr   error
	completeErr error

	createInput   serviceschedule.CreateInput
	createOwner   string
	createIsAdmin bool
	updateID      serviceschedule.ID
	updateInput   serviceschedule.UpdateInput
	updateOwner   string
	updateIsAdmin bool
	deleteID      serviceschedule.ID
	runNowID      serviceschedule.ID
	completeID    serviceschedule.ID
	completeRunID string

	history     []serviceschedule.RunRecord
	historyErr  error
	historyID   serviceschedule.ID
	runDiff     serviceschedule.RunDiff
	runDiffErr  error
	runDiffID   serviceschedule.ID
	runDiffRun  string
	callerEmail string
	callerAdmin bool
}

func (s *scheduleServiceStub) History(
	_ context.Context,
	id serviceschedule.ID,
	callerEmail string,
	isAdmin bool,
) ([]serviceschedule.RunRecord, error) {
	s.historyID = id
	s.callerEmail = callerEmail
	s.callerAdmin = isAdmin
	if s.historyErr != nil {
		return nil, s.historyErr
	}
	return append([]serviceschedule.RunRecord(nil), s.history...), nil
}

func (s *scheduleServiceStub) RunDiff(
	_ context.Context,
	id serviceschedule.ID,
	runID string,
	callerEmail string,
	isAdmin bool,
) (serviceschedule.RunDiff, error) {
	s.runDiffID = id
	s.runDiffRun = runID
	s.callerEmail = callerEmail
	s.callerAdmin = isAdmin
	if s.runDiffErr != nil {
		return serviceschedule.RunDiff{}, s.runDiffErr
	}
	return s.runDiff, nil
}

func (s *scheduleServiceStub) List(
	context.Context,
	string,
	bool,
) ([]serviceschedule.Task, error) {
	return append([]serviceschedule.Task(nil), s.tasks...), s.listErr
}

func (s *scheduleServiceStub) Get(
	_ context.Context,
	id serviceschedule.ID,
	_ string,
	_ bool,
) (serviceschedule.Task, error) {
	if s.getErr != nil {
		return serviceschedule.Task{}, s.getErr
	}
	for _, task := range s.tasks {
		if task.ID == id {
			return task, nil
		}
	}
	return serviceschedule.Task{}, serviceschedule.ErrNotFound
}

func (s *scheduleServiceStub) Create(
	_ context.Context,
	input serviceschedule.CreateInput,
	owner string,
	isAdmin bool,
) (serviceschedule.Task, error) {
	s.createInput = input
	s.createOwner = owner
	s.createIsAdmin = isAdmin
	if s.createErr != nil {
		return serviceschedule.Task{}, s.createErr
	}
	return serviceschedule.Task{
		ID:         scheduleIDOne,
		Name:       input.Name,
		OwnerEmail: owner,
		ProjectID:  input.ProjectID,
		ChatID:     input.ChatID,
		Prompt:     input.Prompt,
		Kind:       input.Kind,
		At:         input.At,
		Cron:       input.Cron,
		Timezone:   input.Timezone,
		Enabled:    true,
		Status:     serviceschedule.StatusActive,
		MaxRuns:    input.MaxRuns,
		CreatedAt:  10,
		UpdatedAt:  10,
	}, nil
}

func (s *scheduleServiceStub) Update(
	_ context.Context,
	id serviceschedule.ID,
	input serviceschedule.UpdateInput,
	owner string,
	isAdmin bool,
) (serviceschedule.Task, error) {
	s.updateID = id
	s.updateInput = input
	s.updateOwner = owner
	s.updateIsAdmin = isAdmin
	if s.updateErr != nil {
		return serviceschedule.Task{}, s.updateErr
	}
	task, err := s.Get(context.Background(), id, owner, isAdmin)
	if err != nil {
		return serviceschedule.Task{}, err
	}
	if input.Enabled != nil {
		task.Enabled = *input.Enabled
	}
	return task, nil
}

func (s *scheduleServiceStub) Delete(
	_ context.Context,
	id serviceschedule.ID,
	_ string,
	_ bool,
) error {
	s.deleteID = id
	return s.deleteErr
}

func (s *scheduleServiceStub) RunNow(
	_ context.Context,
	id serviceschedule.ID,
	owner string,
	isAdmin bool,
) (serviceschedule.Task, error) {
	s.runNowID = id
	if s.runNowErr != nil {
		return serviceschedule.Task{}, s.runNowErr
	}
	return s.Get(context.Background(), id, owner, isAdmin)
}

func (s *scheduleServiceStub) CompleteClaim(
	_ context.Context,
	id serviceschedule.ID,
	activeRunID string,
) (serviceschedule.Task, error) {
	s.completeID = id
	s.completeRunID = activeRunID
	if s.completeErr != nil {
		return serviceschedule.Task{}, s.completeErr
	}
	task, err := s.Get(context.Background(), id, "", true)
	if err != nil {
		return serviceschedule.Task{}, err
	}
	task.Enabled = false
	task.Status = serviceschedule.StatusCompleted
	return task, nil
}

type scheduleCapabilityStub map[string]serviceschedulecapability.Grant

func (s scheduleCapabilityStub) Resolve(
	token string,
) (serviceschedulecapability.Grant, error) {
	grant, ok := s[token]
	if !ok {
		return serviceschedulecapability.Grant{},
			serviceschedulecapability.ErrInvalidCapability
	}
	return grant, nil
}

func TestScheduleChatCollectionDerivesScopeAndParsesAt(t *testing.T) {
	t.Parallel()
	service := &scheduleServiceStub{}
	handler := NewScheduleHandler(service, nil, nil)
	at := "2032-01-02T03:04:05.678Z"
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/chats/cafe/schedules",
		strings.NewReader(`{
			"name":"watch deploy",
			"prompt":"Check the deploy and report failures.",
			"kind":"once",
			"at":"`+at+`",
			"timezone":"America/Toronto",
			"maxRuns":12
		}`),
	)
	response := httptest.NewRecorder()

	handler.HandleChatCollection(response, request, servicechat.Meta{
		ID:        "cafe",
		ProjectID: "project-a",
	}, "OWNER@Example.COM", false)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	wantAt, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		t.Fatal(err)
	}
	if service.createInput.ChatID != "cafe" {
		t.Fatalf("ChatID = %q, want cafe", service.createInput.ChatID)
	}
	if service.createInput.ProjectID != "project-a" {
		t.Fatalf("ProjectID = %q, want project-a", service.createInput.ProjectID)
	}
	if service.createInput.At != wantAt.UnixMilli() {
		t.Fatalf("At = %d, want %d", service.createInput.At, wantAt.UnixMilli())
	}
	if service.createOwner != "OWNER@Example.COM" || service.createIsAdmin {
		t.Fatalf("caller = (%q, %v)", service.createOwner, service.createIsAdmin)
	}
}

func TestScheduleChatCollectionRejectsClientScopeAndLooseChats(t *testing.T) {
	t.Parallel()
	handler := NewScheduleHandler(&scheduleServiceStub{}, nil, nil)

	t.Run("client project is rejected", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/chats/cafe/schedules",
			strings.NewReader(`{
				"name":"watch",
				"prompt":"check",
				"kind":"once",
				"at":2000000000000,
				"projectId":"attacker-project"
			}`),
		)
		response := httptest.NewRecorder()
		handler.HandleChatCollection(response, request, servicechat.Meta{
			ID:        "cafe",
			ProjectID: "trusted-project",
		}, "owner@example.com", false)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", response.Code)
		}
	})

	t.Run("loose chat is rejected", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodGet,
			"/api/chats/cafe/schedules",
			nil,
		)
		response := httptest.NewRecorder()
		handler.HandleChatCollection(
			response,
			request,
			servicechat.Meta{ID: "cafe"},
			"owner@example.com",
			false,
		)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", response.Code)
		}
	})
}

func TestScheduleChatCollectionReturnsOnlyTheChatAndPublicFields(t *testing.T) {
	t.Parallel()
	service := &scheduleServiceStub{tasks: []serviceschedule.Task{
		testSchedule(scheduleIDOne, "owner@example.com", "project-a", "cafe"),
		testSchedule(scheduleIDTwo, "owner@example.com", "project-a", "beef"),
	}}
	service.tasks[0].LastError = "deploy failed"
	service.tasks[0].ActiveRunID = "internal-claim"
	handler := NewScheduleHandler(service, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/chats/cafe/schedules", nil)
	response := httptest.NewRecorder()

	handler.HandleChatCollection(response, request, servicechat.Meta{
		ID:        "cafe",
		ProjectID: "project-a",
	}, "owner@example.com", false)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var body []map[string]any
	decodeScheduleTestJSON(t, response, &body)
	if len(body) != 1 || body[0]["id"] != string(scheduleIDOne) {
		t.Fatalf("body = %#v", body)
	}
	if body[0]["lastError"] != "deploy failed" {
		t.Fatalf("lastError = %#v", body[0]["lastError"])
	}
	if _, exists := body[0]["activeRunId"]; exists {
		t.Fatal("public response exposed activeRunId")
	}
	if _, exists := body[0]["lastRunError"]; exists {
		t.Fatal("public response used legacy lastRunError")
	}
}

func TestScheduleChatCollectionEmptyListIsArray(t *testing.T) {
	t.Parallel()
	handler := NewScheduleHandler(&scheduleServiceStub{}, nil, nil)
	request := httptest.NewRequest(http.MethodGet, "/api/chats/cafe/schedules", nil)
	response := httptest.NewRecorder()
	handler.HandleChatCollection(response, request, servicechat.Meta{
		ID:        "cafe",
		ProjectID: "project-a",
	}, "owner@example.com", false)
	if got := strings.TrimSpace(response.Body.String()); got != "[]" {
		t.Fatalf("body = %q, want []", got)
	}
}

func TestScheduleAgentAPIRequiresBearerAndCorrectScope(t *testing.T) {
	t.Parallel()
	service := &scheduleServiceStub{tasks: []serviceschedule.Task{
		testSchedule(scheduleIDOne, "owner@example.com", "project-a", "cafe"),
	}}
	grants := scheduleCapabilityStub{
		"manage": {
			OwnerEmail: "owner@example.com",
			IsAdmin:    true,
			ProjectID:  "project-a",
			ChatID:     "cafe",
			Scope:      serviceschedulecapability.ScopeManage,
		},
		"complete": {
			OwnerEmail:      "owner@example.com",
			ProjectID:       "project-a",
			ChatID:          "cafe",
			ScheduledTaskID: string(scheduleIDOne),
			ScheduledRunID:  "run-one",
			Scope:           serviceschedulecapability.ScopeCompleteSelf,
		},
	}
	server := scheduleTestServer(service, grants)

	tests := []struct {
		name   string
		method string
		path   string
		token  string
		status int
	}{
		{"missing bearer", http.MethodGet, "/agent-api/schedules", "", http.StatusUnauthorized},
		{"unknown bearer", http.MethodGet, "/agent-api/schedules", "unknown", http.StatusUnauthorized},
		{"completion grant cannot list", http.MethodGet, "/agent-api/schedules", "complete", http.StatusForbidden},
		{"management grant cannot complete", http.MethodPost, "/agent-api/schedules/current/complete", "manage", http.StatusForbidden},
		{"wrong collection suffix", http.MethodGet, "/agent-api/schedules-extra", "manage", http.StatusNotFound},
	}
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			if test.token != "" {
				request.Header.Set("Authorization", "Bearer "+test.token)
			}
			response := httptest.NewRecorder()
			server.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf(
					"status = %d, want %d; body = %s",
					response.Code,
					test.status,
					response.Body.String(),
				)
			}
		})
	}
}

func TestScheduleAgentAPIConfinesEveryOperationToGrant(t *testing.T) {
	t.Parallel()
	allowed := testSchedule(scheduleIDOne, "owner@example.com", "project-a", "cafe")
	otherChat := testSchedule(scheduleIDTwo, "owner@example.com", "project-a", "beef")
	otherOwner := testSchedule(scheduleIDThree, "other@example.com", "project-a", "cafe")
	service := &scheduleServiceStub{
		tasks: []serviceschedule.Task{allowed, otherChat, otherOwner},
	}
	grants := scheduleCapabilityStub{
		"manage": {
			OwnerEmail: "owner@example.com",
			IsAdmin:    true,
			ProjectID:  "project-a",
			ChatID:     "cafe",
			Scope:      serviceschedulecapability.ScopeManage,
		},
	}
	server := scheduleTestServer(service, grants)

	request := scheduleAgentRequest(http.MethodGet, "/agent-api/schedules", "manage", "")
	response := httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", response.Code, response.Body.String())
	}
	var listed []scheduleResponse
	decodeScheduleTestJSON(t, response, &listed)
	if len(listed) != 1 || listed[0].ID != scheduleIDOne {
		t.Fatalf("listed = %#v", listed)
	}

	request = scheduleAgentRequest(
		http.MethodPatch,
		"/agent-api/schedules/"+string(scheduleIDTwo),
		"manage",
		`{"enabled":false}`,
	)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("cross-chat status = %d, want 403", response.Code)
	}
	if service.updateID != "" {
		t.Fatalf("cross-chat update reached service for %q", service.updateID)
	}

	request = scheduleAgentRequest(
		http.MethodPost,
		"/agent-api/schedules",
		"manage",
		`{
			"name":"watch",
			"prompt":"check deploy",
			"kind":"once",
			"at":2000000000000
		}`,
	)
	response = httptest.NewRecorder()
	server.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", response.Code, response.Body.String())
	}
	if service.createInput.ChatID != "cafe" ||
		service.createInput.ProjectID != "project-a" ||
		service.createOwner != "owner@example.com" ||
		!service.createIsAdmin {
		t.Fatalf(
			"create scope = chat %q, project %q, owner %q, admin %v",
			service.createInput.ChatID,
			service.createInput.ProjectID,
			service.createOwner,
			service.createIsAdmin,
		)
	}
}

func TestScheduleAgentCompleteCurrentUsesGrantTask(t *testing.T) {
	t.Parallel()
	service := &scheduleServiceStub{tasks: []serviceschedule.Task{
		testSchedule(scheduleIDOne, "owner@example.com", "project-a", "cafe"),
	}}
	grants := scheduleCapabilityStub{
		"complete": {
			OwnerEmail:      "owner@example.com",
			ProjectID:       "project-a",
			ChatID:          "cafe",
			ScheduledTaskID: string(scheduleIDOne),
			ScheduledRunID:  "run-one",
			Scope:           serviceschedulecapability.ScopeCompleteSelf,
		},
	}
	server := scheduleTestServer(service, grants)
	request := scheduleAgentRequest(
		http.MethodPost,
		"/agent-api/schedules/current/complete",
		"complete",
		"",
	)
	response := httptest.NewRecorder()

	server.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	if service.completeID != scheduleIDOne {
		t.Fatalf("completed ID = %q, want %q", service.completeID, scheduleIDOne)
	}
	if service.completeRunID != "run-one" {
		t.Fatalf("completed run ID = %q, want run-one", service.completeRunID)
	}
	var body scheduleResponse
	decodeScheduleTestJSON(t, response, &body)
	if body.Status != serviceschedule.StatusCompleted || body.Enabled {
		t.Fatalf("completion response = %#v", body)
	}
}

func TestScheduleUserResourceMethodsAndValidation(t *testing.T) {
	t.Parallel()
	service := &scheduleServiceStub{tasks: []serviceschedule.Task{
		testSchedule(scheduleIDOne, "owner@example.com", "project-a", "cafe"),
	}}
	handler := NewScheduleHandler(service, nil, nil)

	t.Run("patch requires enabled", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPatch,
			"/api/schedules/"+string(scheduleIDOne),
			strings.NewReader(`{}`),
		)
		response := httptest.NewRecorder()
		handler.HandleUserResource(response, request)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", response.Code)
		}
	})

	t.Run("patch", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPatch,
			"/api/schedules/"+string(scheduleIDOne),
			strings.NewReader(`{"enabled":false}`),
		)
		response := httptest.NewRecorder()
		handler.HandleUserResource(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
		}
		if service.updateID != scheduleIDOne ||
			service.updateInput.Enabled == nil ||
			*service.updateInput.Enabled {
			t.Fatalf("update = %#v", service.updateInput)
		}
	})

	t.Run("run now", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodPost,
			"/api/schedules/"+string(scheduleIDOne)+"/run",
			nil,
		)
		response := httptest.NewRecorder()
		handler.HandleUserResource(response, request)
		if response.Code != http.StatusOK || service.runNowID != scheduleIDOne {
			t.Fatalf("status = %d, run ID = %q", response.Code, service.runNowID)
		}
	})

	t.Run("delete", func(t *testing.T) {
		request := httptest.NewRequest(
			http.MethodDelete,
			"/api/schedules/"+string(scheduleIDOne),
			nil,
		)
		response := httptest.NewRecorder()
		handler.HandleUserResource(response, request)
		if response.Code != http.StatusOK || service.deleteID != scheduleIDOne {
			t.Fatalf("status = %d, delete ID = %q", response.Code, service.deleteID)
		}
	})
}

func TestScheduleErrorStatusMapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err    error
		status int
	}{
		{serviceschedule.ErrInvalidCron, http.StatusBadRequest},
		{serviceschedule.ErrProjectMismatch, http.StatusBadRequest},
		{serviceschedule.ErrNotFound, http.StatusNotFound},
		{serviceschedule.ErrChatNotFound, http.StatusNotFound},
		{serviceschedule.ErrAccessDenied, http.StatusForbidden},
		{serviceschedule.ErrOwnerUnregistered, http.StatusForbidden},
		{serviceschedule.ErrAlreadyExists, http.StatusConflict},
		{serviceschedule.ErrExecutorBusy, http.StatusConflict},
		{errors.New("storage unavailable"), http.StatusInternalServerError},
	}
	for _, test := range tests {
		response := httptest.NewRecorder()
		sendScheduleError(response, test.err)
		if response.Code != test.status {
			t.Fatalf("%v: status = %d, want %d", test.err, response.Code, test.status)
		}
	}
}

func testSchedule(
	id serviceschedule.ID,
	owner string,
	projectID serviceproject.ID,
	chatID servicechat.ID,
) serviceschedule.Task {
	return serviceschedule.Task{
		ID:         id,
		Name:       "watch deploy",
		OwnerEmail: owner,
		ProjectID:  projectID,
		ChatID:     chatID,
		Prompt:     "Check the deploy.",
		Kind:       serviceschedule.KindCron,
		Cron:       "*/5 * * * *",
		Timezone:   "UTC",
		Enabled:    true,
		Status:     serviceschedule.StatusActive,
		CreatedAt:  10,
		UpdatedAt:  10,
	}
}

func scheduleTestServer(
	service ScheduleService,
	capabilities ScheduleCapabilityResolver,
) http.Handler {
	mux := http.NewServeMux()
	NewScheduleHandler(service, capabilities, nil).RegisterRoutes(mux)
	return mux
}

func scheduleAgentRequest(
	method string,
	path string,
	token string,
	body string,
) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}

func decodeScheduleTestJSON(t *testing.T, response *httptest.ResponseRecorder, target any) {
	t.Helper()
	if err := json.Unmarshal(response.Body.Bytes(), target); err != nil {
		t.Fatalf("decode %q: %v", response.Body.String(), err)
	}
}
