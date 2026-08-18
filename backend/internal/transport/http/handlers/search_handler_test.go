package httphandlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	servicesearch "github.com/futrx-com/remote.futrx.com/internal/service/search"
)

type searchServiceStub struct {
	results []servicesearch.Result
	err     error
	stats   servicesearch.Stats
	queries []servicesearch.Query
}

func (s *searchServiceStub) Search(
	_ context.Context,
	query servicesearch.Query,
) ([]servicesearch.Result, error) {
	s.queries = append(s.queries, query)
	if s.err != nil {
		return nil, s.err
	}
	return s.results, nil
}

func (s *searchServiceStub) Stats() servicesearch.Stats { return s.stats }

func newSearchHandler(service SearchService, caller CallerResolver) *SearchHandler {
	return &SearchHandler{search: service, caller: caller}
}

func TestSearchHandlerAuthorization(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		caller     callerStub
		wantStatus int
		wantQuery  bool
	}{
		{
			name:       "anonymous callers are refused",
			method:     http.MethodGet,
			caller:     callerStub{},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "a session error is refused",
			method:     http.MethodGet,
			caller:     callerStub{email: "member@example.com", err: context.Canceled},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name:       "any signed-in member may search",
			method:     http.MethodGet,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusOK,
			wantQuery:  true,
		},
		{
			name:       "other verbs are refused",
			method:     http.MethodPost,
			caller:     callerStub{email: "member@example.com"},
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &searchServiceStub{}
			handler := newSearchHandler(service, test.caller)
			recorder := httptest.NewRecorder()
			handler.handleSearch(
				recorder,
				httptest.NewRequest(test.method, "/api/search?q=caddy", nil),
			)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, test.wantStatus, recorder.Body)
			}
			if got := len(service.queries) > 0; got != test.wantQuery {
				t.Errorf("service was queried = %t, want %t", got, test.wantQuery)
			}
		})
	}
}

func TestSearchHandlerPassesCallerIdentityAndFilters(t *testing.T) {
	service := &searchServiceStub{
		results: []servicesearch.Result{{ChatID: "chat-1", ChatTitle: "Deploy", Snippet: "hit"}},
		stats:   servicesearch.Stats{Evicted: 3},
	}
	handler := newSearchHandler(service, callerStub{email: "admin@example.com", isAdmin: true})

	recorder := httptest.NewRecorder()
	handler.handleSearch(recorder, httptest.NewRequest(
		http.MethodGet,
		"/api/search?q=caddy&projectId=project-1&limit=5",
		nil,
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", recorder.Code, recorder.Body)
	}

	if len(service.queries) != 1 {
		t.Fatalf("service saw %d queries, want 1", len(service.queries))
	}
	query := service.queries[0]
	if query.Text != "caddy" || query.ProjectID != "project-1" || query.Limit != 5 {
		t.Errorf("query = %+v, want the request parameters forwarded", query)
	}
	if query.Email != "admin@example.com" || !query.IsAdmin {
		t.Errorf("query identity = %q admin=%t, want the caller's", query.Email, query.IsAdmin)
	}

	var payload struct {
		Results   []servicesearch.Result `json:"results"`
		Truncated bool                   `json:"truncated"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Results) != 1 || payload.Results[0].ChatID != "chat-1" {
		t.Errorf("payload results = %+v", payload.Results)
	}
	if !payload.Truncated {
		t.Error("truncated = false, want true when the index evicted history")
	}
}

func TestSearchHandlerAnswersShortQueriesWithAnEmptyList(t *testing.T) {
	service := &searchServiceStub{err: servicesearch.ErrQueryTooShort}
	handler := newSearchHandler(service, callerStub{email: "member@example.com"})

	recorder := httptest.NewRecorder()
	handler.handleSearch(recorder, httptest.NewRequest(http.MethodGet, "/api/search?q=c", nil))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", recorder.Code, recorder.Body)
	}

	var payload struct {
		Results []servicesearch.Result `json:"results"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Results == nil || len(payload.Results) != 0 {
		t.Errorf("results = %+v, want an empty list rather than null", payload.Results)
	}
}

func TestSearchHandlerWithoutServiceIsUnavailable(t *testing.T) {
	handler := newSearchHandler(nil, callerStub{email: "member@example.com"})
	recorder := httptest.NewRecorder()
	handler.handleSearch(recorder, httptest.NewRequest(http.MethodGet, "/api/search?q=caddy", nil))
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}
