package httphandlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	serviceportal "github.com/futrx-com/remote.futrx.com/internal/service/portal"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

type portalServiceStub struct {
	page      serviceportal.Page
	viewErr   error
	seenID    serviceproject.ID
	seenToken string
	seenKey   string

	settings serviceportal.Settings
	saved    *serviceportal.UpdateInput
	saveErr  error
}

func (s *portalServiceStub) Get(
	context.Context, serviceproject.ID,
) (serviceportal.Settings, error) {
	return s.settings, nil
}

func (s *portalServiceStub) Save(
	_ context.Context,
	_ serviceproject.ID,
	input serviceportal.UpdateInput,
) (serviceportal.Settings, error) {
	s.saved = &input
	if s.saveErr != nil {
		return serviceportal.Settings{}, s.saveErr
	}
	return s.settings, nil
}

func (s *portalServiceStub) View(
	_ context.Context,
	projectID serviceproject.ID,
	token string,
	clientKey string,
) (serviceportal.Page, error) {
	s.seenID, s.seenToken, s.seenKey = projectID, token, clientKey
	if s.viewErr != nil {
		return serviceportal.Page{}, s.viewErr
	}
	return s.page, nil
}

func samplePage() serviceportal.Page {
	return serviceportal.Page{
		Title:          "Acme Shop",
		Direction:      "ltr",
		StatusLabel:    "Running",
		Running:        true,
		Previews:       []serviceportal.PreviewLink{{Port: 3000, URL: "https://acme--3000.dev.example.com/"}},
		Changelog:      []serviceportal.ChangeDay{{Label: "Monday, 17 August 2026", Commits: []serviceportal.ChangeCommit{{ShortSHA: "abc1234", Subject: "Add checkout", Author: "Mostafa", Time: "15:04"}}}},
		UpdatedAtLabel: "18 Aug 2026, 12:00 UTC",
	}
}

func servePortal(t *testing.T, service PortalService, target string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	NewPortalHandler(service).RegisterRoutes(mux)
	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, target, nil))
	return recorder
}

func TestPortalPageRendersTheProjectSummary(t *testing.T) {
	service := &portalServiceStub{page: samplePage()}

	recorder := servePortal(t, service, "/portal/9f2a1c04?t=secret-token")

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", recorder.Code, recorder.Body)
	}
	if got := recorder.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("content type = %q", got)
	}
	if recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("a bearer-token page must not be cached")
	}
	if recorder.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("the token in the URL must not leak through Referer")
	}
	body := recorder.Body.String()
	for _, want := range []string{
		`dir="ltr"`,
		"Acme Shop",
		"Running",
		"https://acme--3000.dev.example.com/",
		"Monday, 17 August 2026",
		"Add checkout",
		"18 Aug 2026, 12:00 UTC",
		"noindex",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("page is missing %q:\n%s", want, body)
		}
	}
	if service.seenID != serviceproject.ID("9f2a1c04") || service.seenToken != "secret-token" {
		t.Fatalf("handler passed id=%q token=%q", service.seenID, service.seenToken)
	}
}

func TestPortalPageEscapesTheOperatorNote(t *testing.T) {
	page := samplePage()
	page.Title = `Acme <script>alert(1)</script>`
	page.Note = []string{
		`<img src=x onerror="alert(1)">`,
		`line two & "quoted"`,
	}
	page.Changelog[0].Commits[0].Subject = `Fix <b>bold</b> & things`

	recorder := servePortal(t, &portalServiceStub{page: page}, "/portal/9f2a1c04?t=t")
	body := recorder.Body.String()

	for _, forbidden := range []string{
		"<script>alert(1)</script>",
		`<img src=x onerror="alert(1)">`,
		"<b>bold</b>",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("page rendered %q as markup:\n%s", forbidden, body)
		}
	}
	for _, want := range []string{
		"&lt;script&gt;alert(1)&lt;/script&gt;",
		"&lt;img src=x onerror=",
		"&amp;",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("page is missing the escaped form %q:\n%s", want, body)
		}
	}
}

func TestPortalPageNeverLinksTheLoginGatedApplication(t *testing.T) {
	page := samplePage()
	page.Previews = nil
	page.PreviewsNote = "No public preview is available right now."

	recorder := servePortal(t, &portalServiceStub{page: page}, "/portal/9f2a1c04?t=t")
	body := recorder.Body.String()

	// The page composes its own content: no links back into the platform, no
	// IDE host, no agent browser, no session-gated preview.
	for _, forbidden := range []string{
		"/auth/", "/api/", ".code.", "--6080.", "--8842.", "?share=",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("page leaked a login-gated link %q:\n%s", forbidden, body)
		}
	}
	if !strings.Contains(body, "No public preview is available right now.") {
		t.Fatalf("expected the friendly note:\n%s", body)
	}
}

func TestPortalPageRendersRightToLeft(t *testing.T) {
	page := samplePage()
	page.Direction = "rtl"
	page.Title = "متجر أكمي"

	recorder := servePortal(t, &portalServiceStub{page: page}, "/portal/9f2a1c04?t=t")
	body := recorder.Body.String()

	if !strings.Contains(body, `dir="rtl"`) {
		t.Fatalf("expected an RTL document:\n%s", body)
	}
	if !strings.Contains(body, "متجر أكمي") {
		t.Fatalf("expected the Arabic title to survive:\n%s", body)
	}
}

func TestPortalPageIsSelfContained(t *testing.T) {
	recorder := servePortal(t, &portalServiceStub{page: samplePage()}, "/portal/9f2a1c04?t=t")
	body := recorder.Body.String()

	// No external asset may be fetched: the page has to render for a client
	// behind any network policy, and must not phone home.
	for _, forbidden := range []string{"<script", "<link", "@import", "src=\"http", "fonts.googleapis"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("page pulls an external asset (%q):\n%s", forbidden, body)
		}
	}
}

func TestPortalPageFailuresLookIdentical(t *testing.T) {
	tests := []struct {
		name       string
		service    PortalService
		target     string
		wantStatus int
	}{
		{
			name:       "unknown portal",
			service:    &portalServiceStub{viewErr: serviceportal.ErrNotFound},
			target:     "/portal/9f2a1c04?t=nope",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "no token at all",
			service:    &portalServiceStub{viewErr: serviceportal.ErrNotFound},
			target:     "/portal/9f2a1c04",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "nested path",
			service:    &portalServiceStub{page: samplePage()},
			target:     "/portal/9f2a1c04/extra?t=t",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "portals not configured",
			service:    nil,
			target:     "/portal/9f2a1c04?t=t",
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "rate limited",
			service:    &portalServiceStub{viewErr: serviceportal.ErrRateLimited},
			target:     "/portal/9f2a1c04?t=t",
			wantStatus: http.StatusTooManyRequests,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := servePortal(t, test.service, test.target)
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", recorder.Code, test.wantStatus, recorder.Body)
			}
			if strings.Contains(recorder.Body.String(), "<html") {
				t.Fatalf("a failed lookup rendered a page: %s", recorder.Body)
			}
		})
	}
}

func TestPortalPageRejectsWrites(t *testing.T) {
	mux := http.NewServeMux()
	NewPortalHandler(&portalServiceStub{page: samplePage()}).RegisterRoutes(mux)

	recorder := httptest.NewRecorder()
	mux.ServeHTTP(recorder, httptest.NewRequest(http.MethodPost, "/portal/9f2a1c04?t=t", nil))

	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
}

func TestPortalPagePassesTheClientAddressToTheLimiter(t *testing.T) {
	service := &portalServiceStub{page: samplePage()}
	mux := http.NewServeMux()
	NewPortalHandler(service).RegisterRoutes(mux)

	request := httptest.NewRequest(http.MethodGet, "/portal/9f2a1c04?t=t", nil)
	request.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	mux.ServeHTTP(httptest.NewRecorder(), request)

	if service.seenKey != "203.0.113.7" {
		t.Fatalf("limiter key = %q, want the left-most forwarded address", service.seenKey)
	}
}

func TestProjectPortalSettingsRoundTrip(t *testing.T) {
	service := &portalServiceStub{settings: serviceportal.Settings{
		Enabled: true, ShowPreview: true, URL: "https://remote.example.com/portal/9f2a1c04?t=x",
	}}
	handler := &ProjectHandler{portal: service}

	recorder := httptest.NewRecorder()
	handler.handleProjectPortal(
		recorder,
		httptest.NewRequest(
			http.MethodPut,
			"/api/projects/9f2a1c04/portal",
			strings.NewReader(`{"enabled":true,"rotate":true,"showPreview":true,"note":"hi"}`),
		),
		serviceproject.ID("9f2a1c04"),
	)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d (body %s)", recorder.Code, recorder.Body)
	}
	if service.saved == nil || !service.saved.Rotate || service.saved.Note != "hi" {
		t.Fatalf("saved input = %+v", service.saved)
	}
	if !strings.Contains(recorder.Body.String(), "/portal/9f2a1c04?t=x") {
		t.Fatalf("the one-time URL did not reach the member: %s", recorder.Body)
	}
}

func TestProjectPortalWithoutAServiceIsUnavailable(t *testing.T) {
	handler := &ProjectHandler{}
	recorder := httptest.NewRecorder()
	handler.handleProjectPortal(
		recorder,
		httptest.NewRequest(http.MethodGet, "/api/projects/9f2a1c04/portal", nil),
		serviceproject.ID("9f2a1c04"),
	)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

func TestProjectPortalRejectsUnsupportedMethods(t *testing.T) {
	handler := &ProjectHandler{portal: &portalServiceStub{}}
	recorder := httptest.NewRecorder()
	handler.handleProjectPortal(
		recorder,
		httptest.NewRequest(
			http.MethodPatch, "/api/projects/9f2a1c04/portal", strings.NewReader("{}"),
		),
		serviceproject.ID("9f2a1c04"),
	)
	if recorder.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", recorder.Code)
	}
}
