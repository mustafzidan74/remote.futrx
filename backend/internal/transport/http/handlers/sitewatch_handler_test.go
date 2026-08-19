package httphandlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	servicesitewatch "github.com/futrx-com/remote.futrx.com/internal/service/sitewatch"
)

const (
	watchedSiteID = "aaaaaaaaaaaaaaaaaaaaaaaa"
	hiddenSiteID  = "bbbbbbbbbbbbbbbbbbbbbbbb"
)

// stubSiteWatch answers from a fixed catalog and records every write, so the
// handler's auth and visibility behaviour can be asserted without a scheduler.
type stubSiteWatch struct {
	unavailable bool
	visible     map[string]bool
	created     []servicesitewatch.Input
	updated     []servicesitewatch.Input
	deleted     []servicesitewatch.ID
	imported    []servicesitewatch.ImportInput
	checked     []servicesitewatch.ID
	err         error
}

func (s *stubSiteWatch) Available() bool { return !s.unavailable }

func (s *stubSiteWatch) List(_ context.Context, _ string, isAdmin bool) ([]servicesitewatch.View, error) {
	if s.err != nil {
		return nil, s.err
	}
	out := []servicesitewatch.View{{Site: servicesitewatch.Site{ID: watchedSiteID, URL: "https://shop.example.com/"}}}
	if isAdmin {
		out = append(out, servicesitewatch.View{
			Site: servicesitewatch.Site{ID: hiddenSiteID, URL: "https://internal.example.com/"},
		})
	}
	return out, nil
}

func (s *stubSiteWatch) resolve(id servicesitewatch.ID) error {
	if s.err != nil {
		return s.err
	}
	if s.visible != nil && !s.visible[string(id)] {
		return servicesitewatch.ErrNotFound
	}
	return nil
}

func (s *stubSiteWatch) Get(_ context.Context, id servicesitewatch.ID, _ string, _ bool) (servicesitewatch.View, error) {
	if err := s.resolve(id); err != nil {
		return servicesitewatch.View{}, err
	}
	return servicesitewatch.View{Site: servicesitewatch.Site{ID: id}}, nil
}

func (s *stubSiteWatch) History(_ context.Context, id servicesitewatch.ID, _ string, _ bool) ([]servicesitewatch.Record, error) {
	if err := s.resolve(id); err != nil {
		return nil, err
	}
	return []servicesitewatch.Record{{At: 1, Status: servicesitewatch.StatusUp}}, nil
}

func (s *stubSiteWatch) CheckNow(_ context.Context, id servicesitewatch.ID, _ string, _ bool) (servicesitewatch.Report, error) {
	if err := s.resolve(id); err != nil {
		return servicesitewatch.Report{}, err
	}
	s.checked = append(s.checked, id)
	return servicesitewatch.Report{
		Site:      servicesitewatch.View{Site: servicesitewatch.Site{ID: id}},
		Endpoints: []servicesitewatch.EndpointResult{{URL: "https://shop.example.com/", Status: servicesitewatch.StatusUp}},
	}, nil
}

func (s *stubSiteWatch) Create(_ context.Context, input servicesitewatch.Input) (servicesitewatch.View, error) {
	if s.err != nil {
		return servicesitewatch.View{}, s.err
	}
	s.created = append(s.created, input)
	return servicesitewatch.View{Site: servicesitewatch.Site{ID: watchedSiteID, URL: input.URL}}, nil
}

func (s *stubSiteWatch) Update(_ context.Context, id servicesitewatch.ID, input servicesitewatch.Input) (servicesitewatch.View, error) {
	if s.err != nil {
		return servicesitewatch.View{}, s.err
	}
	s.updated = append(s.updated, input)
	return servicesitewatch.View{Site: servicesitewatch.Site{ID: id, URL: input.URL}}, nil
}

func (s *stubSiteWatch) Delete(_ context.Context, id servicesitewatch.ID) error {
	if s.err != nil {
		return s.err
	}
	s.deleted = append(s.deleted, id)
	return nil
}

func (s *stubSiteWatch) Import(_ context.Context, input servicesitewatch.ImportInput) (servicesitewatch.ImportResult, error) {
	if s.err != nil {
		return servicesitewatch.ImportResult{}, s.err
	}
	s.imported = append(s.imported, input)
	return servicesitewatch.ImportResult{Created: []servicesitewatch.View{}}, nil
}

func (s *stubSiteWatch) Candidates(context.Context) ([]servicesitewatch.Candidate, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []servicesitewatch.Candidate{{ProjectID: "proj-1", Domain: "https://acme.example.com/", SecretKey: "HESTIA_DOMAIN"}}, nil
}

func newSiteWatchMux(sites SiteWatchService, caller CallerResolver) *http.ServeMux {
	mux := http.NewServeMux()
	(&SiteWatchHandler{sites: sites, caller: caller}).RegisterRoutes(mux)
	return mux
}

func siteWatchRequest(
	t *testing.T,
	sites SiteWatchService,
	caller CallerResolver,
	method, path, body string,
) *httptest.ResponseRecorder {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	request := httptest.NewRequest(method, path, reader)
	recorder := httptest.NewRecorder()
	newSiteWatchMux(sites, caller).ServeHTTP(recorder, request)
	return recorder
}

func TestSiteWatchWritesAreAdminOnly(t *testing.T) {
	const validBody = `{"url":"https://shop.example.com/","enabled":true,"intervalMinutes":5}`
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		caller CallerResolver
		want   int
	}{
		{
			name: "anonymous list", method: http.MethodGet, path: "/api/sitewatch/sites",
			caller: stubCaller{}, want: http.StatusUnauthorized,
		},
		{
			name: "broken session", method: http.MethodGet, path: "/api/sitewatch/sites",
			caller: stubCaller{email: "member@example.com", err: errors.New("bad cookie")},
			want:   http.StatusUnauthorized,
		},
		{
			name: "member list", method: http.MethodGet, path: "/api/sitewatch/sites",
			caller: stubCaller{email: "member@example.com"}, want: http.StatusOK,
		},
		{
			name: "member create", method: http.MethodPost, path: "/api/sitewatch/sites", body: validBody,
			caller: stubCaller{email: "member@example.com"}, want: http.StatusForbidden,
		},
		{
			name: "admin create", method: http.MethodPost, path: "/api/sitewatch/sites", body: validBody,
			caller: stubCaller{email: "admin@example.com", isAdmin: true}, want: http.StatusCreated,
		},
		{
			name: "member update", method: http.MethodPut, path: "/api/sitewatch/sites/" + watchedSiteID, body: validBody,
			caller: stubCaller{email: "member@example.com"}, want: http.StatusForbidden,
		},
		{
			name: "member delete", method: http.MethodDelete, path: "/api/sitewatch/sites/" + watchedSiteID,
			caller: stubCaller{email: "member@example.com"}, want: http.StatusForbidden,
		},
		{
			name: "admin delete", method: http.MethodDelete, path: "/api/sitewatch/sites/" + watchedSiteID,
			caller: stubCaller{email: "admin@example.com", isAdmin: true}, want: http.StatusNoContent,
		},
		{
			name: "member import", method: http.MethodPost, path: "/api/admin/sitewatch/import", body: `{"urls":"a.example.com"}`,
			caller: stubCaller{email: "member@example.com"}, want: http.StatusForbidden,
		},
		{
			name: "admin import", method: http.MethodPost, path: "/api/admin/sitewatch/import", body: `{"urls":"a.example.com"}`,
			caller: stubCaller{email: "admin@example.com", isAdmin: true}, want: http.StatusOK,
		},
		{
			name: "member candidates", method: http.MethodGet, path: "/api/admin/sitewatch/candidates",
			caller: stubCaller{email: "member@example.com"}, want: http.StatusForbidden,
		},
		{
			name: "admin candidates", method: http.MethodGet, path: "/api/admin/sitewatch/candidates",
			caller: stubCaller{email: "admin@example.com", isAdmin: true}, want: http.StatusOK,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sites := &stubSiteWatch{visible: map[string]bool{watchedSiteID: true}}
			got := siteWatchRequest(t, sites, test.caller, test.method, test.path, test.body)
			if got.Code != test.want {
				t.Fatalf("status = %d, want %d (%s)", got.Code, test.want, got.Body.String())
			}
		})
	}
}

func TestSiteWatchNeverWritesForANonAdmin(t *testing.T) {
	sites := &stubSiteWatch{visible: map[string]bool{watchedSiteID: true}}
	member := stubCaller{email: "member@example.com"}
	body := `{"url":"https://evil.example.com/","intervalMinutes":1}`

	siteWatchRequest(t, sites, member, http.MethodPost, "/api/sitewatch/sites", body)
	siteWatchRequest(t, sites, member, http.MethodPut, "/api/sitewatch/sites/"+watchedSiteID, body)
	siteWatchRequest(t, sites, member, http.MethodDelete, "/api/sitewatch/sites/"+watchedSiteID, "")
	siteWatchRequest(t, sites, member, http.MethodPost, "/api/admin/sitewatch/import", `{"urls":"x.example.com"}`)

	if len(sites.created)+len(sites.updated)+len(sites.deleted)+len(sites.imported) != 0 {
		t.Fatalf("a member reached the write paths: %+v", sites)
	}
}

func TestSiteWatchListIsScopedToTheCaller(t *testing.T) {
	sites := &stubSiteWatch{visible: map[string]bool{watchedSiteID: true}}

	member := siteWatchRequest(t, sites, stubCaller{email: "member@example.com"}, http.MethodGet, "/api/sitewatch/sites", "")
	var payload struct {
		Sites    []servicesitewatch.View `json:"sites"`
		MaxSites int                     `json:"maxSites"`
	}
	if err := json.Unmarshal(member.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode member list: %v", err)
	}
	if len(payload.Sites) != 1 || string(payload.Sites[0].ID) != watchedSiteID {
		t.Fatalf("member list = %+v, want only the site they may see", payload.Sites)
	}
	if payload.MaxSites != servicesitewatch.MaxSites {
		t.Fatalf("maxSites = %d, want the server's cap echoed", payload.MaxSites)
	}

	admin := siteWatchRequest(t, sites, stubCaller{email: "admin@example.com", isAdmin: true}, http.MethodGet, "/api/sitewatch/sites", "")
	if err := json.Unmarshal(admin.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode admin list: %v", err)
	}
	if len(payload.Sites) != 2 {
		t.Fatalf("admin list = %+v, want every site", payload.Sites)
	}
}

func TestSiteWatchReadsAndChecksHideAnInvisibleSite(t *testing.T) {
	sites := &stubSiteWatch{visible: map[string]bool{watchedSiteID: true}}
	member := stubCaller{email: "member@example.com"}

	for _, path := range []struct {
		method string
		url    string
	}{
		{http.MethodGet, "/api/sitewatch/sites/" + hiddenSiteID},
		{http.MethodGet, "/api/sitewatch/sites/" + hiddenSiteID + "/history"},
		{http.MethodPost, "/api/sitewatch/sites/" + hiddenSiteID + "/check"},
	} {
		got := siteWatchRequest(t, sites, member, path.method, path.url, "")
		// Not 403: a member must not be able to learn that this id exists.
		if got.Code != http.StatusNotFound {
			t.Fatalf("%s %s = %d, want 404", path.method, path.url, got.Code)
		}
	}
	if len(sites.checked) != 0 {
		t.Fatalf("an invisible site was checked: %v", sites.checked)
	}

	// The same member may check the site they can see.
	got := siteWatchRequest(t, sites, member, http.MethodPost, "/api/sitewatch/sites/"+watchedSiteID+"/check", "")
	if got.Code != http.StatusOK {
		t.Fatalf("check on a visible site = %d, want 200 (%s)", got.Code, got.Body.String())
	}
	if len(sites.checked) != 1 {
		t.Fatalf("checked = %v, want the visible site", sites.checked)
	}
}

func TestSiteWatchRejectsAMalformedID(t *testing.T) {
	sites := &stubSiteWatch{visible: map[string]bool{watchedSiteID: true}}
	admin := stubCaller{email: "admin@example.com", isAdmin: true}
	// Traversal segments never reach the handler — the mux normalizes the
	// path first — so the cases that matter here are the well-formed-looking
	// ids the store must still refuse as file names.
	for _, id := range []string{"AAAAAAAAAAAAAAAAAAAAAAAA", "short", "zzzzzzzzzzzzzzzzzzzzzzzz", "aaaaaaaaaaaaaaaaaaaaaaaaa"} {
		got := siteWatchRequest(t, sites, admin, http.MethodGet, "/api/sitewatch/sites/"+id, "")
		if got.Code != http.StatusNotFound {
			t.Fatalf("GET id %q = %d, want 404", id, got.Code)
		}
	}
}

func TestSiteWatchMapsServiceErrorsOntoStatusCodes(t *testing.T) {
	admin := stubCaller{email: "admin@example.com", isAdmin: true}
	tests := []struct {
		name string
		err  error
		want int
	}{
		{name: "invalid", err: servicesitewatch.ErrInvalidSite, want: http.StatusBadRequest},
		{name: "limit", err: servicesitewatch.ErrTooManySites, want: http.StatusConflict},
		{name: "not found", err: servicesitewatch.ErrNotFound, want: http.StatusNotFound},
		{name: "other", err: errors.New("disk on fire"), want: http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sites := &stubSiteWatch{err: test.err}
			got := siteWatchRequest(t, sites, admin, http.MethodPost, "/api/sitewatch/sites",
				`{"url":"https://shop.example.com/","intervalMinutes":5}`)
			if got.Code != test.want {
				t.Fatalf("status = %d, want %d", got.Code, test.want)
			}
		})
	}
}

func TestSiteWatchWithoutAStoreReports503(t *testing.T) {
	sites := &stubSiteWatch{unavailable: true}
	admin := stubCaller{email: "admin@example.com", isAdmin: true}
	for _, path := range []string{
		"/api/sitewatch/sites",
		"/api/sitewatch/sites/" + watchedSiteID,
		"/api/admin/sitewatch/candidates",
	} {
		got := siteWatchRequest(t, sites, admin, http.MethodGet, path, "")
		if got.Code != http.StatusServiceUnavailable {
			t.Fatalf("GET %s = %d, want 503", path, got.Code)
		}
	}
}

func TestSiteWatchRefusesTheWrongMethod(t *testing.T) {
	sites := &stubSiteWatch{visible: map[string]bool{watchedSiteID: true}}
	admin := stubCaller{email: "admin@example.com", isAdmin: true}
	for _, path := range []struct {
		method string
		url    string
	}{
		{http.MethodPatch, "/api/sitewatch/sites"},
		{http.MethodGet, "/api/sitewatch/sites/" + watchedSiteID + "/check"},
		{http.MethodPost, "/api/sitewatch/sites/" + watchedSiteID + "/history"},
		{http.MethodGet, "/api/admin/sitewatch/import"},
	} {
		got := siteWatchRequest(t, sites, admin, path.method, path.url, "")
		if got.Code != http.StatusMethodNotAllowed {
			t.Fatalf("%s %s = %d, want 405", path.method, path.url, got.Code)
		}
	}
}
