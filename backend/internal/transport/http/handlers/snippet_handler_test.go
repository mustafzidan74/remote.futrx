package httphandlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	serviceauth "github.com/futrx-com/remote.futrx.com/internal/service/auth"
	servicesnippets "github.com/futrx-com/remote.futrx.com/internal/service/snippets"
)

// snippetRepoStub is the in-memory library behind the real service, so these
// tests exercise the handler against the same ownership rules production uses.
type snippetRepoStub struct {
	documents map[servicesnippets.Owner][]servicesnippets.Snippet
}

func (r *snippetRepoStub) Load(
	_ context.Context,
	owner servicesnippets.Owner,
) ([]servicesnippets.Snippet, bool, error) {
	list, found := r.documents[owner]
	return append([]servicesnippets.Snippet(nil), list...), found, nil
}

func (r *snippetRepoStub) Save(
	_ context.Context,
	owner servicesnippets.Owner,
	list []servicesnippets.Snippet,
) error {
	r.documents[owner] = append([]servicesnippets.Snippet(nil), list...)
	return nil
}

// snippetSessionStub answers with one fixed session, or with no session at all.
type snippetSessionStub struct {
	session *serviceauth.Session
}

func (s snippetSessionStub) Session(*http.Request) (*serviceauth.Session, error) {
	if s.session == nil {
		return nil, errors.New("no session")
	}
	return s.session, nil
}

func newSnippetHandler(session *serviceauth.Session) (*SnippetHandler, *snippetRepoStub) {
	repo := &snippetRepoStub{documents: map[servicesnippets.Owner][]servicesnippets.Snippet{}}
	return &SnippetHandler{
		snippets: servicesnippets.New(repo),
		sessions: snippetSessionStub{session: session},
	}, repo
}

func serveSnippet(
	handler *SnippetHandler,
	method, target, body string,
) *httptest.ResponseRecorder {
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, target, reader)
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, request)
	return recorder
}

func TestSnippetRoutesRequireASession(t *testing.T) {
	tests := []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "list", method: http.MethodGet, target: "/api/me/snippets"},
		{name: "create", method: http.MethodPost, target: "/api/me/snippets", body: `{"title":"T","body":"b"}`},
		{name: "update", method: http.MethodPut, target: "/api/me/snippets/x", body: `{"title":"T","body":"b"}`},
		{name: "delete", method: http.MethodDelete, target: "/api/me/snippets/x"},
		{name: "use", method: http.MethodPost, target: "/api/me/snippets/x/use"},
		{name: "import", method: http.MethodPost, target: "/api/me/snippets/import", body: `{"snippets":[]}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, repo := newSnippetHandler(nil)
			recorder := serveSnippet(handler, tt.method, tt.target, tt.body)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", recorder.Code)
			}
			if len(repo.documents) != 0 {
				t.Fatal("an anonymous request wrote a library")
			}
		})
	}
}

func TestSnippetsBelongToTheCallerOnly(t *testing.T) {
	owner := &serviceauth.Session{Email: "owner@example.com", Sub: "sub-owner"}
	stranger := &serviceauth.Session{Email: "stranger@example.com", Sub: "sub-stranger"}

	handler, repo := newSnippetHandler(owner)
	created := serveSnippet(handler, http.MethodPost, "/api/me/snippets", `{"title":"Mine","body":"secret prompt"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201: %s", created.Code, created.Body.String())
	}
	var item servicesnippets.Snippet
	if err := json.Unmarshal(created.Body.Bytes(), &item); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// The same store, a different session: every route must behave as though
	// the snippet does not exist.
	intruder := &SnippetHandler{
		snippets: servicesnippets.New(repo),
		sessions: snippetSessionStub{session: stranger},
	}
	for _, attempt := range []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "update", method: http.MethodPut, target: "/api/me/snippets/" + item.ID, body: `{"title":"X","body":"y"}`},
		{name: "delete", method: http.MethodDelete, target: "/api/me/snippets/" + item.ID},
		{name: "use", method: http.MethodPost, target: "/api/me/snippets/" + item.ID + "/use"},
	} {
		t.Run(attempt.name, func(t *testing.T) {
			recorder := serveSnippet(intruder, attempt.method, attempt.target, attempt.body)
			if recorder.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want 404", recorder.Code)
			}
		})
	}

	listed := serveSnippet(intruder, http.MethodGet, "/api/me/snippets", "")
	if listed.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", listed.Code)
	}
	if strings.Contains(listed.Body.String(), "secret prompt") {
		t.Fatal("one user's library leaked into another's listing")
	}

	mine := serveSnippet(handler, http.MethodGet, "/api/me/snippets", "")
	if !strings.Contains(mine.Body.String(), "secret prompt") {
		t.Fatal("the owner lost their own snippet")
	}
}

func TestSnippetCollectionLifecycle(t *testing.T) {
	session := &serviceauth.Session{Email: "me@example.com", Sub: "sub-me"}
	handler, _ := newSnippetHandler(session)

	// The first read seeds the bilingual client templates.
	first := serveSnippet(handler, http.MethodGet, "/api/me/snippets", "")
	if first.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200", first.Code)
	}
	var collection struct {
		Snippets []servicesnippets.Snippet `json:"snippets"`
	}
	if err := json.Unmarshal(first.Body.Bytes(), &collection); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(collection.Snippets) == 0 {
		t.Fatal("the first read seeded nothing")
	}
	seeded := collection.Snippets[0]

	used := serveSnippet(handler, http.MethodPost, "/api/me/snippets/"+seeded.ID+"/use", "")
	if used.Code != http.StatusOK {
		t.Fatalf("use status = %d, want 200", used.Code)
	}
	var counted servicesnippets.Snippet
	if err := json.Unmarshal(used.Body.Bytes(), &counted); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if counted.Uses != 1 {
		t.Fatalf("uses = %d, want 1", counted.Uses)
	}

	imported := serveSnippet(
		handler,
		http.MethodPost,
		"/api/me/snippets/import",
		`{"snippets":[{"title":"From a file","body":"text"}]}`,
	)
	if imported.Code != http.StatusOK {
		t.Fatalf("import status = %d, want 200: %s", imported.Code, imported.Body.String())
	}
	if !strings.Contains(imported.Body.String(), "From a file") {
		t.Fatal("the imported snippet is missing from the response")
	}

	deleted := serveSnippet(handler, http.MethodDelete, "/api/me/snippets/"+seeded.ID, "")
	if deleted.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200", deleted.Code)
	}
}

func TestSnippetRoutesRejectBadRequests(t *testing.T) {
	session := &serviceauth.Session{Email: "me@example.com", Sub: "sub-me"}

	tests := []struct {
		name       string
		method     string
		target     string
		body       string
		wantStatus int
	}{
		{
			name:       "a snippet needs a title",
			method:     http.MethodPost,
			target:     "/api/me/snippets",
			body:       `{"body":"only a body"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "a snippet needs some text",
			method:     http.MethodPost,
			target:     "/api/me/snippets",
			body:       `{"title":"Empty"}`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "malformed json is refused",
			method:     http.MethodPost,
			target:     "/api/me/snippets",
			body:       `{`,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "the collection refuses unknown verbs",
			method:     http.MethodDelete,
			target:     "/api/me/snippets",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "the item route refuses unknown verbs",
			method:     http.MethodPatch,
			target:     "/api/me/snippets/anything",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "the use route is POST only",
			method:     http.MethodGet,
			target:     "/api/me/snippets/anything/use",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name:       "unknown sub-routes are not found",
			method:     http.MethodPost,
			target:     "/api/me/snippets/anything/rename",
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, _ := newSnippetHandler(session)
			recorder := serveSnippet(handler, tt.method, tt.target, tt.body)
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
		})
	}
}

func TestSnippetHandlerWithoutAServiceIsUnavailable(t *testing.T) {
	handler := &SnippetHandler{
		sessions: snippetSessionStub{session: &serviceauth.Session{Email: "me@example.com"}},
	}
	recorder := serveSnippet(handler, http.MethodGet, "/api/me/snippets", "")
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}
