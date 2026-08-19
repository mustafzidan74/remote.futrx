package httphandlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	serviceaudit "github.com/futrx-com/remote.futrx.com/internal/service/audit"
	servicegithub "github.com/futrx-com/remote.futrx.com/internal/service/github"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// githubTestService answers with whatever the test set up, and records what
// the handler asked it to do.
type githubTestService struct {
	available    bool
	status       servicegithub.Status
	statusErr    error
	settings     servicegithub.PublicSettings
	saveErr      error
	savedInput   servicegithub.SettingsInput
	savedActor   string
	saves        int
	prResult     servicegithub.CreatePRResult
	prErr        error
	prInput      servicegithub.CreatePRInput
	pulls        []servicegithub.PullRequest
	importResult servicegithub.ImportResult
	importErr    error
	importNumber int
	linkInput    servicegithub.LinkInput
	linkErr      error
	unlinks      int
	clones       int
	deliveries   int
	deliveryOut  servicegithub.DeliveryOutcome
	deliveryErr  error
	lastDelivery servicegithub.DeliveryRequest
	suggestion   servicegithub.CommitMessageSuggestion
	suggestErr   error
	suggestions  int
}

func (s *githubTestService) Available() bool { return s.available }

func (s *githubTestService) SuggestCommitMessage(
	context.Context,
	serviceproject.ID,
) (servicegithub.CommitMessageSuggestion, error) {
	s.suggestions++
	return s.suggestion, s.suggestErr
}

func (s *githubTestService) Status(context.Context, serviceproject.ID) (servicegithub.Status, error) {
	return s.status, s.statusErr
}

func (s *githubTestService) Link(
	_ context.Context,
	_ serviceproject.ID,
	in servicegithub.LinkInput,
	_ string,
) (serviceproject.Meta, error) {
	s.linkInput = in
	return serviceproject.Meta{}, s.linkErr
}

func (s *githubTestService) Unlink(context.Context, serviceproject.ID) error {
	s.unlinks++
	return nil
}

func (s *githubTestService) Clone(context.Context, serviceproject.ID) error {
	s.clones++
	return nil
}

func (s *githubTestService) CreatePR(
	_ context.Context,
	_ serviceproject.ID,
	in servicegithub.CreatePRInput,
) (servicegithub.CreatePRResult, error) {
	s.prInput = in
	return s.prResult, s.prErr
}

func (s *githubTestService) ListPullRequests(
	context.Context,
	serviceproject.ID,
) ([]servicegithub.PullRequest, error) {
	return s.pulls, nil
}

func (s *githubTestService) ImportComments(
	_ context.Context,
	_ serviceproject.ID,
	number int,
	_ servicegithub.ImportInput,
	_ string,
) (servicegithub.ImportResult, error) {
	s.importNumber = number
	return s.importResult, s.importErr
}

func (s *githubTestService) Settings(
	context.Context,
	serviceproject.ID,
) (servicegithub.PublicSettings, error) {
	return s.settings, nil
}

func (s *githubTestService) SaveSettings(
	_ context.Context,
	_ serviceproject.ID,
	in servicegithub.SettingsInput,
	actor string,
) (servicegithub.PublicSettings, error) {
	s.saves++
	s.savedInput = in
	s.savedActor = actor
	if s.saveErr != nil {
		return servicegithub.PublicSettings{}, s.saveErr
	}
	return s.settings, nil
}

func (s *githubTestService) HandleDelivery(
	_ context.Context,
	_ serviceproject.ID,
	req servicegithub.DeliveryRequest,
) (servicegithub.DeliveryOutcome, error) {
	s.deliveries++
	s.lastDelivery = req
	return s.deliveryOut, s.deliveryErr
}

func newGitHubTestHandler(service *githubTestService) (*http.ServeMux, *GitHubHandler) {
	handler := NewGitHubHandler(service, nil).WithAudit(serviceaudit.Nop{})
	mux := http.NewServeMux()
	handler.RegisterRoutes(mux)
	return mux, handler
}

const githubTestProjectID = serviceproject.ID("abcd1234")

/* ------------------------------------------------------------------ *
 * Panel authorization
 * ------------------------------------------------------------------ */

func TestGitHubSettingsAutoRunIsAdminOnly(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		isAdmin    bool
		wantStatus int
		wantSaved  bool
	}{
		{
			name: "a member may not arm automatic runs",
			body: `{"autoRun":true}`, wantStatus: http.StatusForbidden,
		},
		{
			name: "an admin may arm automatic runs",
			body: `{"autoRun":true}`, isAdmin: true,
			wantStatus: http.StatusOK, wantSaved: true,
		},
		{
			// Turning it *off* is safety, not privilege: any member who sees
			// something wrong must be able to stop it.
			name:       "a member may disarm automatic runs",
			body:       `{"autoRun":false}`,
			wantStatus: http.StatusOK, wantSaved: true,
		},
		{
			name:       "a member may change the trigger label",
			body:       `{"label":"agent"}`,
			wantStatus: http.StatusOK, wantSaved: true,
		},
		{
			name:       "a member may rotate the secret",
			body:       `{"rotate":true}`,
			wantStatus: http.StatusOK, wantSaved: true,
		},
		{
			name:       "a member may disable the webhook",
			body:       `{"disable":true}`,
			wantStatus: http.StatusOK, wantSaved: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &githubTestService{available: true}
			_, handler := newGitHubTestHandler(service)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(
				http.MethodPut,
				"/api/projects/abcd1234/github/settings",
				strings.NewReader(test.body),
			)

			handler.HandleProjectResource(
				recorder, request, githubTestProjectID, "settings", "member@example.test", test.isAdmin,
			)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)",
					recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if saved := service.saves > 0; saved != test.wantSaved {
				t.Fatalf("saved = %v, want %v", saved, test.wantSaved)
			}
			if test.wantSaved && service.savedActor != "member@example.test" {
				t.Fatalf("actor = %q, want the caller", service.savedActor)
			}
		})
	}
}

func TestGitHubPanelRouting(t *testing.T) {
	tests := []struct {
		name       string
		method     string
		rest       string
		body       string
		wantStatus int
		check      func(*testing.T, *githubTestService)
	}{
		{
			name: "status", method: http.MethodGet, rest: "", wantStatus: http.StatusOK,
		},
		{
			name: "link", method: http.MethodPut, rest: "", body: `{"repo":"o/r"}`,
			wantStatus: http.StatusOK,
			check: func(t *testing.T, s *githubTestService) {
				if s.linkInput.Repo != "o/r" {
					t.Fatalf("link input = %+v", s.linkInput)
				}
			},
		},
		{
			name: "unlink", method: http.MethodDelete, rest: "", wantStatus: http.StatusOK,
			check: func(t *testing.T, s *githubTestService) {
				if s.unlinks != 1 {
					t.Fatalf("unlinks = %d, want 1", s.unlinks)
				}
			},
		},
		{
			name: "clone", method: http.MethodPost, rest: "clone", wantStatus: http.StatusOK,
			check: func(t *testing.T, s *githubTestService) {
				if s.clones != 1 {
					t.Fatalf("clones = %d, want 1", s.clones)
				}
			},
		},
		{
			name: "create pull request", method: http.MethodPost, rest: "pr",
			body: `{"title":"T","commit":true}`, wantStatus: http.StatusCreated,
			check: func(t *testing.T, s *githubTestService) {
				if s.prInput.Title != "T" || !s.prInput.Commit {
					t.Fatalf("pr input = %+v", s.prInput)
				}
			},
		},
		{
			name: "list pull requests", method: http.MethodGet, rest: "prs",
			wantStatus: http.StatusOK,
		},
		{
			name: "import comments", method: http.MethodPost, rest: "prs/12/import-comments",
			body: `{"chatId":"c1"}`, wantStatus: http.StatusOK,
			check: func(t *testing.T, s *githubTestService) {
				if s.importNumber != 12 {
					t.Fatalf("import number = %d, want 12", s.importNumber)
				}
			},
		},
		{
			name:   "import comments with a non-numeric number",
			method: http.MethodPost, rest: "prs/abc/import-comments",
			wantStatus: http.StatusBadRequest,
		},
		{
			name:   "import comments with a negative number",
			method: http.MethodPost, rest: "prs/-1/import-comments",
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "unknown action", method: http.MethodGet, rest: "nonsense",
			wantStatus: http.StatusNotFound,
		},
		{
			name: "wrong method on the pull request route", method: http.MethodGet, rest: "pr",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name: "wrong method on clone", method: http.MethodGet, rest: "clone",
			wantStatus: http.StatusMethodNotAllowed,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &githubTestService{available: true}
			_, handler := newGitHubTestHandler(service)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, "/api/projects/abcd1234/github/"+test.rest,
				strings.NewReader(test.body))

			handler.HandleProjectResource(
				recorder, request, githubTestProjectID, test.rest, "member@example.test", false,
			)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)",
					recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if test.check != nil {
				test.check(t, service)
			}
		})
	}
}

func TestGitHubDirtyWorkspaceAnswersWithTheCommitDialogPayload(t *testing.T) {
	service := &githubTestService{
		available: true,
		prErr:     servicegithub.ErrDirtyWorkspace,
		status: servicegithub.Status{
			DefaultCommitMessage: "Changes from Remote — 2026-08-18",
			DirtyCount:           4,
		},
	}
	_, handler := newGitHubTestHandler(service)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/projects/abcd1234/github/pr",
		strings.NewReader(`{"title":"T"}`))

	handler.HandleProjectResource(recorder, request, githubTestProjectID, "pr", "m@e.test", false)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", recorder.Code)
	}
	var body dirtyWorkspaceResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !body.Dirty || body.DefaultCommitMessage != "Changes from Remote — 2026-08-18" ||
		body.DirtyCount != 4 {
		t.Fatalf("body = %+v, want the commit dialog's payload", body)
	}
}

func TestGitHubUnavailableServiceReports503(t *testing.T) {
	handler := &GitHubHandler{audit: serviceaudit.Nop{}}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/projects/abcd1234/github", nil)

	handler.HandleProjectResource(recorder, request, githubTestProjectID, "", "m@e.test", true)

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", recorder.Code)
	}
}

/* ------------------------------------------------------------------ *
 * Webhook route
 * ------------------------------------------------------------------ */

func TestGitHubWebhookGuards(t *testing.T) {
	tests := []struct {
		name        string
		method      string
		path        string
		available   bool
		deliveryErr error
		body        string
		wantStatus  int
		wantHandled bool
	}{
		{
			name: "accepted", method: http.MethodPost, path: "/hooks/github/abcd1234",
			available: true, body: `{}`, wantStatus: http.StatusAccepted, wantHandled: true,
		},
		{
			name: "GET is refused", method: http.MethodGet, path: "/hooks/github/abcd1234",
			available: true, wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name: "missing project id", method: http.MethodPost, path: "/hooks/github/",
			available: true, body: `{}`, wantStatus: http.StatusNotFound,
		},
		{
			name: "path traversal in the id", method: http.MethodPost,
			path: "/hooks/github/..%2F..%2Fetc", available: true, body: `{}`,
			wantStatus: http.StatusNotFound,
		},
		{
			name: "non-hex id", method: http.MethodPost, path: "/hooks/github/zzzz",
			available: true, body: `{}`, wantStatus: http.StatusNotFound,
		},
		{
			name: "service unavailable", method: http.MethodPost, path: "/hooks/github/abcd1234",
			body: `{}`, wantStatus: http.StatusServiceUnavailable,
		},
		{
			// Every rejection past the id check answers 401 and says nothing
			// about whether the project exists or is configured.
			name: "bad signature", method: http.MethodPost, path: "/hooks/github/abcd1234",
			available: true, deliveryErr: servicegithub.ErrBadSignature, body: `{}`,
			wantStatus: http.StatusUnauthorized, wantHandled: true,
		},
		{
			name: "no secret configured", method: http.MethodPost, path: "/hooks/github/abcd1234",
			available: true, deliveryErr: servicegithub.ErrWebhookDisabled, body: `{}`,
			wantStatus: http.StatusUnauthorized, wantHandled: true,
		},
		{
			name: "project not linked", method: http.MethodPost, path: "/hooks/github/abcd1234",
			available: true, deliveryErr: servicegithub.ErrNotLinked, body: `{}`,
			wantStatus: http.StatusUnauthorized, wantHandled: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service := &githubTestService{available: test.available, deliveryErr: test.deliveryErr}
			mux, _ := newGitHubTestHandler(service)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(test.method, test.path, strings.NewReader(test.body))
			mux.ServeHTTP(recorder, request)

			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)",
					recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if handled := service.deliveries > 0; handled != test.wantHandled {
				t.Fatalf("service saw the delivery = %v, want %v", handled, test.wantHandled)
			}
		})
	}
}

func TestGitHubWebhookForwardsTheRawBodyAndHeaders(t *testing.T) {
	service := &githubTestService{available: true}
	mux, _ := newGitHubTestHandler(service)
	body := `{ "action" : "opened" }`

	request := httptest.NewRequest(http.MethodPost, "/hooks/github/abcd1234", strings.NewReader(body))
	request.Header.Set(servicegithub.EventHeader, "issues")
	request.Header.Set(servicegithub.DeliveryHeader, "guid-1")
	request.Header.Set(servicegithub.SignatureHeader, "sha256=abc")
	mux.ServeHTTP(httptest.NewRecorder(), request)

	got := service.lastDelivery
	// The exact bytes matter: the signature was computed over them, so any
	// re-encoding on the way in would break verification.
	if string(got.Body) != body {
		t.Fatalf("body = %q, want the raw bytes %q", got.Body, body)
	}
	if got.Event != "issues" || got.ID != "guid-1" || got.Signature != "sha256=abc" {
		t.Fatalf("delivery = %+v, want the headers forwarded verbatim", got)
	}
}

func TestGitHubWebhookRejectsAnOversizedBody(t *testing.T) {
	service := &githubTestService{available: true}
	mux, _ := newGitHubTestHandler(service)
	oversized := strings.Repeat("x", servicegithub.MaxPayloadBytes+10)

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/hooks/github/abcd1234",
		strings.NewReader(oversized))
	mux.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 413", recorder.Code)
	}
	if service.deliveries != 0 {
		t.Fatal("an oversized body must never reach the service")
	}
}

func TestGitHubWebhookRateLimit(t *testing.T) {
	service := &githubTestService{available: true}
	mux, handler := newGitHubTestHandler(service)
	moment := time.Now()
	handler.WithClock(func() time.Time { return moment })

	send := func(remoteAddr string) int {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/hooks/github/abcd1234",
			strings.NewReader(`{}`))
		request.RemoteAddr = remoteAddr
		mux.ServeHTTP(recorder, request)
		return recorder.Code
	}

	for i := 0; i < webhookRateLimit; i++ {
		if code := send("203.0.113.9:1234"); code != http.StatusAccepted {
			t.Fatalf("request %d: status = %d, want 202", i, code)
		}
	}
	if code := send("203.0.113.9:1234"); code != http.StatusTooManyRequests {
		t.Fatalf("over-budget request: status = %d, want 429", code)
	}
	// The budget is per client address, so a second repository's deliveries
	// are unaffected by the first one's flood.
	if code := send("198.51.100.4:1234"); code != http.StatusAccepted {
		t.Fatalf("another address: status = %d, want 202", code)
	}
	// A new window resets the budget.
	moment = moment.Add(webhookRateWindow + time.Second)
	if code := send("203.0.113.9:1234"); code != http.StatusAccepted {
		t.Fatalf("after the window: status = %d, want 202", code)
	}
}

func TestWebhookRateKeyIgnoresASpoofedForwardedFor(t *testing.T) {
	// Caddy is not configured with trusted_proxies, so it *appends* the real
	// peer to X-Forwarded-For. The left-most value is whatever the caller
	// sent, which is why the budget must not key on it.
	tests := []struct {
		name       string
		forwarded  string
		remoteAddr string
		want       string
	}{
		{
			name:      "spoofed prefix is ignored in favour of the appended hop",
			forwarded: "1.1.1.1, 203.0.113.9", remoteAddr: "127.0.0.1:5000",
			want: "203.0.113.9",
		},
		{
			name: "single hop", forwarded: "203.0.113.9", remoteAddr: "127.0.0.1:5000",
			want: "203.0.113.9",
		},
		{
			name:       "no header falls back to the socket peer",
			remoteAddr: "203.0.113.9:5000", want: "203.0.113.9",
		},
		{
			name:      "blank header falls back to the socket peer",
			forwarded: "  ", remoteAddr: "203.0.113.9:5000", want: "203.0.113.9",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/hooks/github/abcd1234", nil)
			request.RemoteAddr = test.remoteAddr
			if test.forwarded != "" {
				request.Header.Set("X-Forwarded-For", test.forwarded)
			}
			if got := webhookRateKey(request); got != test.want {
				t.Fatalf("webhookRateKey = %q, want %q", got, test.want)
			}
		})
	}
}

func TestWebhookRateLimitCannotBeEscapedByRotatingForwardedFor(t *testing.T) {
	service := &githubTestService{available: true}
	mux, handler := newGitHubTestHandler(service)
	moment := time.Now()
	handler.WithClock(func() time.Time { return moment })

	send := func(spoofed string) int {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/hooks/github/abcd1234",
			strings.NewReader(`{}`))
		request.RemoteAddr = "127.0.0.1:5000"
		// One attacker, a fresh forged prefix on every request, one real peer.
		request.Header.Set("X-Forwarded-For", spoofed+", 203.0.113.9")
		mux.ServeHTTP(recorder, request)
		return recorder.Code
	}

	for i := 0; i < webhookRateLimit; i++ {
		if code := send("10.0.0." + strconv.Itoa(i)); code != http.StatusAccepted {
			t.Fatalf("request %d: status = %d, want 202", i, code)
		}
	}
	if code := send("10.0.0.250"); code != http.StatusTooManyRequests {
		t.Fatalf("a rotated X-Forwarded-For escaped the budget: status = %d, want 429", code)
	}
}
