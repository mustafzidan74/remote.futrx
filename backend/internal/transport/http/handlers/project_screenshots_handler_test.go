package httphandlers

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	servicescreenshot "github.com/futrx-com/remote.futrx.com/internal/service/screenshot"
	"github.com/futrx-com/remote.futrx.com/internal/stores/fileproject"
	"github.com/futrx-com/remote.futrx.com/internal/stores/filescreenshot"
)

func TestScreenshotCaptureAndRead(t *testing.T) {
	fixture := newScreenshotFixture(t)
	base := "/api/projects/" + string(fixture.project.ID)

	rec := fixture.request(t, http.MethodPost, base+"/screenshot",
		`{"port":3000,"path":"/pricing"}`, http.StatusOK)
	var created servicescreenshot.CaptureResult
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode capture: %v", err)
	}
	if created.Screenshot.ID == "" || created.Screenshot.Bytes == 0 {
		t.Fatalf("capture = %#v, want a stored record", created.Screenshot)
	}
	wantURL := base + "/screenshots/" + string(created.Screenshot.ID) + ".png"
	if created.Screenshot.URL != wantURL {
		t.Fatalf("url = %q, want %q", created.Screenshot.URL, wantURL)
	}

	list := fixture.request(t, http.MethodGet, base+"/screenshots", "", http.StatusOK)
	var listed struct {
		Screenshots   []servicescreenshot.Screenshot `json:"screenshots"`
		Notifications bool                           `json:"notifications"`
	}
	if err := json.NewDecoder(list.Body).Decode(&listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Screenshots) != 1 || listed.Screenshots[0].ID != created.Screenshot.ID {
		t.Fatalf("list = %#v, want the captured record", listed.Screenshots)
	}

	image := fixture.request(t, http.MethodGet, wantURL, "", http.StatusOK)
	if got := image.Header().Get("Content-Type"); got != "image/png" {
		t.Fatalf("content type = %q, want image/png", got)
	}
	if got := image.Header().Get("Cache-Control"); !strings.Contains(got, "private") {
		t.Fatalf("cache control = %q, want a private cache", got)
	}
	if image.Body.Len() != int(created.Screenshot.Bytes) {
		t.Fatalf("served %d bytes, want %d", image.Body.Len(), created.Screenshot.Bytes)
	}
}

func TestScreenshotRejectsBadRequests(t *testing.T) {
	fixture := newScreenshotFixture(t)
	base := "/api/projects/" + string(fixture.project.ID)

	tests := []struct {
		name       string
		method     string
		path       string
		body       string
		wantStatus int
	}{
		{
			name: "port below the preview range", method: http.MethodPost, path: base + "/screenshot",
			body: `{"port":80}`, wantStatus: http.StatusBadRequest,
		},
		{
			name: "relative path", method: http.MethodPost, path: base + "/screenshot",
			body: `{"port":3000,"path":"pricing"}`, wantStatus: http.StatusBadRequest,
		},
		{
			name: "traversal path", method: http.MethodPost, path: base + "/screenshot",
			body: `{"port":3000,"path":"/../secrets"}`, wantStatus: http.StatusBadRequest,
		},
		{
			name: "viewport out of range", method: http.MethodPost, path: base + "/screenshot",
			body: `{"port":3000,"width":99999,"height":800}`, wantStatus: http.StatusBadRequest,
		},
		{
			name: "malformed body", method: http.MethodPost, path: base + "/screenshot",
			body: `{`, wantStatus: http.StatusBadRequest,
		},
		{
			name: "wrong method on capture", method: http.MethodGet, path: base + "/screenshot",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name: "wrong method on read", method: http.MethodDelete, path: base + "/screenshots",
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name: "unknown capture", method: http.MethodGet,
			path: base + "/screenshots/deadbeefdeadbeef.png", wantStatus: http.StatusNotFound,
		},
		{
			name: "read without the .png suffix", method: http.MethodGet,
			path: base + "/screenshots/deadbeefdeadbeef", wantStatus: http.StatusBadRequest,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture.request(t, test.method, test.path, test.body, test.wantStatus)
		})
	}
}

// TestScreenshotSendWithoutASink covers the resend route: the capture exists,
// but nothing is configured to receive it.
func TestScreenshotSendWithoutASink(t *testing.T) {
	fixture := newScreenshotFixture(t)
	base := "/api/projects/" + string(fixture.project.ID)
	rec := fixture.request(t, http.MethodPost, base+"/screenshot", `{"port":3000}`, http.StatusOK)
	var created servicescreenshot.CaptureResult
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode capture: %v", err)
	}

	send := base + "/screenshots/" + string(created.Screenshot.ID) + "/send"
	fixture.request(t, http.MethodPost, send, "", http.StatusConflict)
	fixture.request(t, http.MethodGet, send, "", http.StatusMethodNotAllowed)
	fixture.request(t, http.MethodPost, base+"/screenshots/deadbeefdeadbeef/send", "", http.StatusNotFound)
}

func TestScreenshotRefusesAStoppedContainer(t *testing.T) {
	fixture := newScreenshotFixture(t)
	fixture.projects.status = serviceproject.StatusStopped

	fixture.request(t, http.MethodPost,
		"/api/projects/"+string(fixture.project.ID)+"/screenshot",
		`{"port":3000}`, http.StatusConflict)
}

func TestScreenshotReportsUnavailableWithoutAService(t *testing.T) {
	fixture := newScreenshotFixture(t)
	handler := NewProjectHandler(fixture.realProjects, nil, nil, screenshotPublicHostname)
	base := "/api/projects/" + string(fixture.project.ID)

	for _, path := range []string{base + "/screenshot", base + "/screenshots"} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"port":3000}`))
		rec := httptest.NewRecorder()
		handler.HandleResource(rec, req)
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("%s = %d, want 503", path, rec.Code)
		}
	}
}

func TestScreenshotPublicLinkRoute(t *testing.T) {
	fixture := newScreenshotFixture(t)
	rec := fixture.request(t, http.MethodPost,
		"/api/projects/"+string(fixture.project.ID)+"/screenshot",
		`{"port":3000}`, http.StatusOK)
	var created servicescreenshot.CaptureResult
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode capture: %v", err)
	}
	link, err := fixture.screenshots.MintLink(
		context.Background(), fixture.project.ID, created.Screenshot.ID,
	)
	if err != nil {
		t.Fatalf("MintLink(): %v", err)
	}
	path := strings.TrimPrefix(link, "https://"+screenshotPublicHostname)

	links := NewScreenshotLinkHandler(fixture.screenshots)
	tests := []struct {
		name       string
		method     string
		path       string
		advance    time.Duration
		wantStatus int
	}{
		{name: "valid token", method: http.MethodGet, path: path, wantStatus: http.StatusOK},
		{
			name: "tampered token", method: http.MethodGet,
			path: strings.Replace(path, ".png", "x.png", 1), wantStatus: http.StatusNotFound,
		},
		{
			name: "missing suffix", method: http.MethodGet,
			path: strings.TrimSuffix(path, ".png"), wantStatus: http.StatusNotFound,
		},
		{
			name: "wrong method", method: http.MethodPost, path: path,
			wantStatus: http.StatusMethodNotAllowed,
		},
		{
			name: "expired", method: http.MethodGet, path: path,
			advance: servicescreenshot.PublicLinkTTL + time.Hour, wantStatus: http.StatusGone,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture.now = fixture.now.Add(test.advance)
			req := httptest.NewRequest(test.method, test.path, nil)
			rec := httptest.NewRecorder()
			links.Handle(rec, req)
			if rec.Code != test.wantStatus {
				t.Fatalf("%s %s = %d, want %d", test.method, test.path, rec.Code, test.wantStatus)
			}
			if test.wantStatus == http.StatusOK && rec.Header().Get("Content-Type") != "image/png" {
				t.Fatalf("content type = %q, want image/png", rec.Header().Get("Content-Type"))
			}
		})
	}
}

/* ------------------------------------------------------------------ *
 * fixture
 * ------------------------------------------------------------------ */

const screenshotPublicHostname = "remote.example.test"

type screenshotFixture struct {
	handler      *ProjectHandler
	screenshots  *servicescreenshot.Service
	realProjects *serviceproject.Service
	projects     *screenshotProjects
	project      serviceproject.Meta
	now          time.Time
}

func (f *screenshotFixture) request(
	t *testing.T,
	method, path, body string,
	wantStatus int,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Host = screenshotPublicHostname
	rec := httptest.NewRecorder()
	f.handler.HandleResource(rec, req)
	if rec.Code != wantStatus {
		t.Fatalf("%s %s = %d, want %d; body=%s", method, path, rec.Code, wantStatus, rec.Body.String())
	}
	return rec
}

func newScreenshotFixture(t *testing.T) *screenshotFixture {
	t.Helper()
	dataDir := t.TempDir()
	repo, err := fileproject.NewWithWorkspaceRoot(dataDir, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	projects := serviceproject.New(repo, serviceproject.ContainerDependencies{}, nil, nil)
	project, err := projects.Create(
		context.Background(), serviceproject.CreateInput{Name: "Screenshot Project"}, "owner@example.com",
	)
	if err != nil {
		t.Fatal(err)
	}
	store, err := filescreenshot.New(dataDir)
	if err != nil {
		t.Fatal(err)
	}

	fixture := &screenshotFixture{
		realProjects: projects,
		project:      project,
		now:          time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}
	// The project service cannot report "running" without a container runtime,
	// so the screenshot service reads project metadata through a stand-in that
	// answers with the state a real running project would have.
	fixture.projects = &screenshotProjects{
		projects: projects,
		status:   serviceproject.StatusRunning,
	}
	fixture.screenshots = servicescreenshot.New(
		store, store, &stubCapturer{}, fixture.projects,
		servicescreenshot.WithBaseURL("https://"+screenshotPublicHostname),
		servicescreenshot.WithClock(func() time.Time { return fixture.now }),
	)
	fixture.handler = NewProjectHandler(projects, nil, nil, screenshotPublicHostname).
		WithScreenshots(fixture.screenshots)
	return fixture
}

type screenshotProjects struct {
	projects *serviceproject.Service
	status   serviceproject.Status
}

func (p *screenshotProjects) Get(
	ctx context.Context,
	id serviceproject.ID,
) (serviceproject.Meta, error) {
	meta, err := p.projects.Get(ctx, id)
	if err != nil {
		return serviceproject.Meta{}, err
	}
	meta.Status = p.status
	if meta.ContainerName == "" {
		meta.ContainerName = meta.Slug
	}
	return meta, nil
}

type stubCapturer struct{}

func (stubCapturer) Available() bool { return true }

func (stubCapturer) Capture(
	context.Context,
	servicescreenshot.CaptureRequest,
) ([]byte, error) {
	out := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n', 0, 0, 0, 13, 'I', 'H', 'D', 'R'}
	out = binary.BigEndian.AppendUint32(out, 1280)
	out = binary.BigEndian.AppendUint32(out, 800)
	return append(out, 8, 6, 0, 0, 0), nil
}
