package httphandlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	serviceschedule "github.com/futrx-com/remote.futrx.com/internal/service/schedule"
	serviceschedulecapability "github.com/futrx-com/remote.futrx.com/internal/service/schedulecapability"
)

const historyTaskID serviceschedule.ID = "0123456789abcdef01234567"

func TestScheduleHistoryRoutesReturnRecordsAndDiffs(t *testing.T) {
	t.Parallel()
	service := &scheduleServiceStub{
		tasks: []serviceschedule.Task{{ID: historyTaskID, OwnerEmail: "owner@example.com"}},
		history: []serviceschedule.RunRecord{{
			RunID:      "run-1",
			StartedAt:  1000,
			FinishedAt: 4500,
			Status:     serviceschedule.HistoryOK,
			Summary:    "all good",
			Result:     "SCORE=94",
		}},
		runDiff: serviceschedule.RunDiff{
			RunID:      "run-1",
			DiffStat:   " a.ts | 2 +-",
			CommitStat: " a.ts | 2 +-",
			CommitSHA:  "abcdef1234567",
		},
	}
	server := scheduleTestServer(service, nil)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet, "/api/schedules/"+string(historyTaskID)+"/history", nil,
	))
	if response.Code != http.StatusOK {
		t.Fatalf("history status = %d: %s", response.Code, response.Body.String())
	}
	var records []struct {
		RunID      string `json:"runId"`
		Status     string `json:"status"`
		Result     string `json:"result"`
		DurationMs int64  `json:"durationMs"`
	}
	decodeScheduleTestJSON(t, response, &records)
	if len(records) != 1 || records[0].RunID != "run-1" || records[0].Result != "SCORE=94" {
		t.Fatalf("records = %#v", records)
	}
	if records[0].DurationMs != 3500 {
		t.Fatalf("duration = %d, want 3500", records[0].DurationMs)
	}
	if service.historyID != historyTaskID {
		t.Fatalf("history was asked for %q", service.historyID)
	}

	response = httptest.NewRecorder()
	server.ServeHTTP(response, httptest.NewRequest(
		http.MethodGet,
		"/api/schedules/"+string(historyTaskID)+"/history/run-1/diff",
		nil,
	))
	if response.Code != http.StatusOK {
		t.Fatalf("diff status = %d: %s", response.Code, response.Body.String())
	}
	var diff struct {
		RunID      string `json:"runId"`
		CommitStat string `json:"commitStat"`
	}
	decodeScheduleTestJSON(t, response, &diff)
	if diff.RunID != "run-1" || diff.CommitStat == "" {
		t.Fatalf("diff = %#v", diff)
	}
	if service.runDiffRun != "run-1" {
		t.Fatalf("diff was asked for run %q", service.runDiffRun)
	}
}

func TestScheduleHistoryRejectsBadShapes(t *testing.T) {
	t.Parallel()
	service := &scheduleServiceStub{
		tasks:      []serviceschedule.Task{{ID: historyTaskID}},
		runDiffErr: serviceschedule.ErrRunNotFound,
	}
	server := scheduleTestServer(service, nil)

	tests := []struct {
		name   string
		method string
		path   string
		want   int
	}{
		{
			name:   "history is read-only",
			method: http.MethodPost,
			path:   "/api/schedules/" + string(historyTaskID) + "/history",
			want:   http.StatusMethodNotAllowed,
		},
		{
			name:   "unknown sub-resource",
			method: http.MethodGet,
			path:   "/api/schedules/" + string(historyTaskID) + "/history/run-1/patch",
			want:   http.StatusNotFound,
		},
		{
			name:   "history without a run id but with a diff suffix",
			method: http.MethodGet,
			path:   "/api/schedules/" + string(historyTaskID) + "/history/diff",
			want:   http.StatusNotFound,
		},
		{
			name:   "unknown run",
			method: http.MethodGet,
			path:   "/api/schedules/" + string(historyTaskID) + "/history/nope/diff",
			want:   http.StatusNotFound,
		},
		{
			name:   "path deeper than the router allows",
			method: http.MethodGet,
			path:   "/api/schedules/" + string(historyTaskID) + "/history/run-1/diff/extra",
			want:   http.StatusNotFound,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			response := httptest.NewRecorder()
			server.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
			if response.Code != test.want {
				t.Fatalf("status = %d, want %d: %s", response.Code, test.want, response.Body.String())
			}
		})
	}
}

func TestScheduleHistoryIsNotReachableThroughAnAgentGrant(t *testing.T) {
	t.Parallel()
	service := &scheduleServiceStub{
		tasks: []serviceschedule.Task{{
			ID:         historyTaskID,
			OwnerEmail: "owner@example.com",
			ChatID:     "cafe",
			ProjectID:  "project-a",
		}},
		history: []serviceschedule.RunRecord{{RunID: "run-1"}},
	}
	capabilities := scheduleCapabilityStub{"token": {
		OwnerEmail: "owner@example.com",
		ChatID:     "cafe",
		ProjectID:  "project-a",
		Scope:      serviceschedulecapability.ScopeManage,
	}}
	server := scheduleTestServer(service, capabilities)

	for _, path := range []string{
		"/agent-api/schedules/" + string(historyTaskID) + "/history",
		"/agent-api/schedules/" + string(historyTaskID) + "/history/run-1/diff",
	} {
		response := httptest.NewRecorder()
		server.ServeHTTP(response, scheduleAgentRequest(http.MethodGet, path, "token", ""))
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s status = %d, want 404: %s", path, response.Code, response.Body.String())
		}
	}
	if len(service.history) == 0 || service.historyID != "" {
		t.Fatalf("an agent grant reached the history service (%q)", service.historyID)
	}
}

func TestScheduleCreateAcceptsChainAndCondition(t *testing.T) {
	t.Parallel()
	service := &scheduleServiceStub{}
	handler := NewScheduleHandler(service, nil, nil)
	request := httptest.NewRequest(
		http.MethodPost,
		"/api/chats/cafe/schedules",
		strings.NewReader(`{
			"name":"weekly site audit",
			"prompt":"Audit the site.",
			"kind":"cron",
			"cron":"0 6 * * 1",
			"timezone":"UTC",
			"next":[{"taskId":"0123456789abcdef01234567","when":"failure","delayMin":15}],
			"condition":{"kind":"weekdays","weekdays":[1]}
		}`),
	)
	response := httptest.NewRecorder()

	handler.HandleChatCollection(response, request, servicechat.Meta{
		ID:        "cafe",
		ProjectID: "project-a",
	}, "owner@example.com", false)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if len(service.createInput.Next) != 1 ||
		service.createInput.Next[0].When != serviceschedule.ChainWhenFailure ||
		service.createInput.Next[0].DelayMin != 15 {
		t.Fatalf("chain not decoded: %#v", service.createInput.Next)
	}
	if service.createInput.Condition == nil ||
		service.createInput.Condition.Kind != serviceschedule.ConditionWeekdays {
		t.Fatalf("condition not decoded: %#v", service.createInput.Condition)
	}
}

func TestScheduleAgentUpdateStillRefusesChainEdits(t *testing.T) {
	t.Parallel()
	service := &scheduleServiceStub{
		tasks: []serviceschedule.Task{{
			ID:         historyTaskID,
			OwnerEmail: "owner@example.com",
			ChatID:     "cafe",
			ProjectID:  "project-a",
		}},
	}
	capabilities := scheduleCapabilityStub{"token": {
		OwnerEmail: "owner@example.com",
		ChatID:     "cafe",
		ProjectID:  "project-a",
		Scope:      serviceschedulecapability.ScopeManage,
	}}
	server := scheduleTestServer(service, capabilities)

	response := httptest.NewRecorder()
	server.ServeHTTP(response, scheduleAgentRequest(
		http.MethodPatch,
		"/agent-api/schedules/"+string(historyTaskID),
		"token",
		`{"enabled":false,"next":[]}`,
	))
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: %s", response.Code, response.Body.String())
	}
	if service.updateInput.Next != nil {
		t.Fatal("an agent chain edit reached the service")
	}
}
