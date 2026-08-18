package screenshot

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

const testProjectID = serviceproject.ID("abcd1234")

func TestCaptureInputNormalize(t *testing.T) {
	tests := []struct {
		name    string
		in      CaptureInput
		want    CaptureInput
		wantErr error
	}{
		{
			name: "defaults are filled in",
			in:   CaptureInput{Port: 3000},
			want: CaptureInput{Port: 3000, Path: "/", Width: DefaultWidth, Height: DefaultHeight},
		},
		{
			name: "a path with a query survives",
			in:   CaptureInput{Port: 5173, Path: "/checkout?step=2", Width: 800, Height: 600},
			want: CaptureInput{Port: 5173, Path: "/checkout?step=2", Width: 800, Height: 600},
		},
		{
			name:    "port below the preview range",
			in:      CaptureInput{Port: 80},
			wantErr: ErrInvalidPort,
		},
		{
			name:    "port above the preview range",
			in:      CaptureInput{Port: 70000},
			wantErr: ErrInvalidPort,
		},
		{
			name:    "relative path",
			in:      CaptureInput{Port: 3000, Path: "checkout"},
			wantErr: ErrInvalidPath,
		},
		{
			name:    "traversal in the path",
			in:      CaptureInput{Port: 3000, Path: "/../../etc/passwd"},
			wantErr: ErrInvalidPath,
		},
		{
			name:    "shell metacharacters in the path",
			in:      CaptureInput{Port: 3000, Path: `/a" ; rm -rf /`},
			wantErr: ErrInvalidPath,
		},
		{
			name:    "viewport too wide",
			in:      CaptureInput{Port: 3000, Width: MaxWidth + 1},
			wantErr: ErrInvalidSize,
		},
		{
			name:    "viewport too short",
			in:      CaptureInput{Port: 3000, Height: 1},
			wantErr: ErrInvalidSize,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := test.in.Normalize()
			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("Normalize() error = %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Normalize(): %v", err)
			}
			if got != test.want {
				t.Fatalf("Normalize() = %#v, want %#v", got, test.want)
			}
		})
	}
}

func TestDecodePNGSize(t *testing.T) {
	tests := []struct {
		name          string
		data          []byte
		width, height int
		wantErr       bool
	}{
		{name: "a real header", data: pngBytes(1280, 800), width: 1280, height: 800},
		{name: "empty", data: nil, wantErr: true},
		{name: "a shell error", data: []byte("bash: npx: command not found\n"), wantErr: true},
		{name: "truncated", data: pngBytes(10, 10)[:12], wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			width, height, err := DecodePNGSize(test.data)
			if test.wantErr {
				if err == nil {
					t.Fatalf("DecodePNGSize() = (%d, %d), want an error", width, height)
				}
				return
			}
			if err != nil {
				t.Fatalf("DecodePNGSize(): %v", err)
			}
			if width != test.width || height != test.height {
				t.Fatalf("DecodePNGSize() = (%d, %d), want (%d, %d)",
					width, height, test.width, test.height)
			}
		})
	}
}

func TestCaptureStoresARecordAndItsImage(t *testing.T) {
	service, fixture := newTestService(t)

	result, err := service.Capture(context.Background(), testProjectID, CaptureInput{
		Port: 3000, Path: "/pricing", FullPage: true,
	}, "Owner@Example.com")
	if err != nil {
		t.Fatalf("Capture(): %v", err)
	}
	record := result.Screenshot
	if record.Width != 1280 || record.Height != 800 {
		t.Fatalf("dimensions = %dx%d, want 1280x800 read from the PNG header",
			record.Width, record.Height)
	}
	if record.CreatedBy != "owner@example.com" {
		t.Fatalf("createdBy = %q, want the lowercased actor", record.CreatedBy)
	}
	if record.URL != ReadPath(testProjectID, record.ID) {
		t.Fatalf("url = %q, want the session-gated read route", record.URL)
	}
	if record.LinkTokenHash != "" {
		t.Fatalf("record leaked a link digest: %q", record.LinkTokenHash)
	}
	if fixture.capturer.last.URL != "http://127.0.0.1:3000/pricing" {
		t.Fatalf("browser URL = %q, want in-container loopback", fixture.capturer.last.URL)
	}
	if !fixture.capturer.last.FullPage {
		t.Fatal("fullPage was not passed through to the browser")
	}

	stored, err := fixture.blobs.Read(testProjectID, record.File)
	if err != nil {
		t.Fatalf("stored image: %v", err)
	}
	if !bytes.Equal(stored, pngBytes(1280, 800)) {
		t.Fatal("stored bytes are not the captured PNG")
	}
	if len(result.Delivered) != 0 {
		t.Fatalf("delivered = %#v, want nothing without notify", result.Delivered)
	}
}

func TestCaptureRefusesAStoppedProject(t *testing.T) {
	service, fixture := newTestService(t)
	fixture.projects.meta.Status = serviceproject.StatusStopped

	_, err := service.Capture(context.Background(), testProjectID, CaptureInput{Port: 3000}, "u@e.com")
	if !errors.Is(err, ErrNotRunning) {
		t.Fatalf("Capture() error = %v, want ErrNotRunning", err)
	}
}

func TestCaptureRejectsANonImageFromTheContainer(t *testing.T) {
	service, fixture := newTestService(t)
	fixture.capturer.data = []byte("Error: page.goto: net::ERR_CONNECTION_REFUSED")

	_, err := service.Capture(context.Background(), testProjectID, CaptureInput{Port: 3000}, "u@e.com")
	if !errors.Is(err, ErrNotAnImage) {
		t.Fatalf("Capture() error = %v, want ErrNotAnImage", err)
	}
	records, _ := service.List(context.Background(), testProjectID)
	if len(records) != 0 {
		t.Fatalf("records = %#v, want nothing stored for a failed capture", records)
	}
}

func TestCapturePrunesToTheRetentionCount(t *testing.T) {
	service, fixture := newTestService(t)

	for i := 0; i < RetentionCount+5; i++ {
		fixture.advance(time.Second)
		if _, err := service.Capture(
			context.Background(), testProjectID, CaptureInput{Port: 3000}, "u@e.com",
		); err != nil {
			t.Fatalf("capture %d: %v", i, err)
		}
	}

	records, err := service.List(context.Background(), testProjectID)
	if err != nil {
		t.Fatalf("List(): %v", err)
	}
	if len(records) != RetentionCount {
		t.Fatalf("kept %d records, want %d", len(records), RetentionCount)
	}
	if records[0].CreatedAt < records[len(records)-1].CreatedAt {
		t.Fatal("List() is not newest-first")
	}
	// Every evicted PNG must go with its record: the index is what makes a
	// file reachable, so an orphan would never be listed, served, or pruned.
	if got := fixture.blobs.count(); got != RetentionCount {
		t.Fatalf("blobs on disk = %d, want %d", got, RetentionCount)
	}
	for _, record := range records {
		if _, err := fixture.blobs.Read(testProjectID, record.File); err != nil {
			t.Fatalf("kept record %s has no image: %v", record.ID, err)
		}
	}
}

func TestPublicLinkResolvesUntilItExpires(t *testing.T) {
	service, fixture := newTestService(t)
	result, err := service.Capture(
		context.Background(), testProjectID, CaptureInput{Port: 3000}, "u@e.com",
	)
	if err != nil {
		t.Fatalf("Capture(): %v", err)
	}

	link, err := service.MintLink(context.Background(), testProjectID, result.Screenshot.ID)
	if err != nil {
		t.Fatalf("MintLink(): %v", err)
	}
	const prefix = "https://remote.example.test/s/screenshot/"
	if !strings.HasPrefix(link, prefix) || !strings.HasSuffix(link, ".png") {
		t.Fatalf("link = %q, want %s<token>.png", link, prefix)
	}
	token := strings.TrimSuffix(strings.TrimPrefix(link, prefix), ".png")
	if !strings.HasPrefix(token, string(testProjectID)+"-") {
		t.Fatalf("token = %q, want the project id as its first segment", token)
	}

	data, record, err := service.ResolveLink(context.Background(), token)
	if err != nil {
		t.Fatalf("ResolveLink(): %v", err)
	}
	if !bytes.Equal(data, pngBytes(1280, 800)) || record.ID != result.Screenshot.ID {
		t.Fatal("ResolveLink() returned the wrong image")
	}

	if _, _, err := service.ResolveLink(context.Background(), token+"x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a tampered token resolved: %v", err)
	}
	if _, _, err := service.ResolveLink(context.Background(), "not-a-token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a malformed token resolved: %v", err)
	}

	fixture.advance(PublicLinkTTL + time.Minute)
	if _, _, err := service.ResolveLink(context.Background(), token); !errors.Is(err, ErrLinkExpired) {
		t.Fatalf("ResolveLink() after expiry = %v, want ErrLinkExpired", err)
	}
}

func TestNotifyFansOutAndMintsALinkOnlyWhenASinkNeedsOne(t *testing.T) {
	tests := []struct {
		name            string
		needsLink       bool
		wantPublicURL   bool
		wantLinkInImage bool
	}{
		{name: "picture-capable sinks only", needsLink: false},
		{name: "a text-only sink is configured", needsLink: true, wantPublicURL: true, wantLinkInImage: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, fixture := newTestService(t)
			fixture.notifier.needsLink = test.needsLink

			result, err := service.Capture(context.Background(), testProjectID, CaptureInput{
				Port: 3000, Notify: true,
			}, "u@e.com")
			if err != nil {
				t.Fatalf("Capture(): %v", err)
			}
			if len(result.Delivered) != 2 {
				t.Fatalf("delivered = %#v, want one row per sink", result.Delivered)
			}
			if !result.Delivered[0].Delivered || result.Delivered[1].Error == "" {
				t.Fatalf("delivered = %#v, want the fake sinks' outcomes verbatim", result.Delivered)
			}
			if got := result.PublicURL != ""; got != test.wantPublicURL {
				t.Fatalf("publicUrl set = %t, want %t (%q)", got, test.wantPublicURL, result.PublicURL)
			}
			sent := fixture.notifier.last
			if got := sent.LinkURL != ""; got != test.wantLinkInImage {
				t.Fatalf("image link set = %t, want %t", got, test.wantLinkInImage)
			}
			if !bytes.Equal(sent.Data, pngBytes(1280, 800)) {
				t.Fatal("the sink did not receive the captured bytes")
			}
			if !strings.Contains(sent.Caption, "Screenshot Project") ||
				!strings.Contains(sent.Caption, ":3000/") {
				t.Fatalf("caption = %q, want the project and the exact address", sent.Caption)
			}
		})
	}
}

func TestNotifyWithoutASinkReportsTheCaptureAnyway(t *testing.T) {
	service, fixture := newTestService(t)
	fixture.notifier.configured = false

	result, err := service.Capture(context.Background(), testProjectID, CaptureInput{
		Port: 3000, Notify: true,
	}, "u@e.com")
	if !errors.Is(err, ErrNoNotification) {
		t.Fatalf("Capture() error = %v, want ErrNoNotification", err)
	}
	if result.Screenshot.ID == "" {
		t.Fatal("the capture itself must still be reported: it succeeded and is stored")
	}
}

/* ------------------------------------------------------------------ *
 * fixtures
 * ------------------------------------------------------------------ */

type testFixture struct {
	repo     *fakeRepository
	blobs    *fakeBlobs
	capturer *fakeCapturer
	projects *fakeProjects
	notifier *fakeNotifier
	now      time.Time
	mu       sync.Mutex
}

func (f *testFixture) advance(d time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.now = f.now.Add(d)
}

func (f *testFixture) clock() time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.now
}

func newTestService(t *testing.T) (*Service, *testFixture) {
	t.Helper()
	fixture := &testFixture{
		repo:     &fakeRepository{},
		blobs:    &fakeBlobs{files: map[string][]byte{}},
		capturer: &fakeCapturer{data: pngBytes(1280, 800)},
		projects: &fakeProjects{meta: serviceproject.Meta{
			ID:            testProjectID,
			Name:          "Screenshot Project",
			Slug:          "screenshot-project",
			ContainerName: "screenshot-project",
			Status:        serviceproject.StatusRunning,
		}},
		notifier: &fakeNotifier{configured: true},
		now:      time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC),
	}
	service := New(
		fixture.repo, fixture.blobs, fixture.capturer, fixture.projects,
		WithClock(fixture.clock),
		WithBaseURL("https://remote.example.test/"),
		WithNotifier(fixture.notifier),
	)
	return service, fixture
}

type fakeRepository struct {
	mu      sync.Mutex
	records []Screenshot
}

func (r *fakeRepository) List(context.Context, serviceproject.ID) ([]Screenshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]Screenshot{}, r.records...), nil
}

func (r *fakeRepository) Update(
	_ context.Context,
	_ serviceproject.ID,
	fn func([]Screenshot) ([]Screenshot, error),
) ([]Screenshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	next, err := fn(append([]Screenshot{}, r.records...))
	if err != nil {
		return nil, err
	}
	r.records = next
	return next, nil
}

type fakeBlobs struct {
	mu    sync.Mutex
	files map[string][]byte
}

func (b *fakeBlobs) Write(_ serviceproject.ID, file string, data []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.files[file] = append([]byte{}, data...)
	return nil
}

func (b *fakeBlobs) Read(_ serviceproject.ID, file string) ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, ok := b.files[file]
	if !ok {
		return nil, errors.New("no such file")
	}
	return data, nil
}

func (b *fakeBlobs) Remove(_ serviceproject.ID, file string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.files, file)
	return nil
}

func (b *fakeBlobs) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.files)
}

type fakeCapturer struct {
	data []byte
	err  error
	last CaptureRequest
}

func (c *fakeCapturer) Available() bool { return true }

func (c *fakeCapturer) Capture(_ context.Context, req CaptureRequest) ([]byte, error) {
	c.last = req
	if c.err != nil {
		return nil, c.err
	}
	return c.data, nil
}

type fakeProjects struct {
	meta serviceproject.Meta
}

func (p *fakeProjects) Get(context.Context, serviceproject.ID) (serviceproject.Meta, error) {
	return p.meta, nil
}

type fakeNotifier struct {
	configured bool
	needsLink  bool
	last       Image
}

func (n *fakeNotifier) Configured() bool      { return n.configured }
func (n *fakeNotifier) NeedsPublicLink() bool { return n.needsLink }

func (n *fakeNotifier) SendImage(_ context.Context, image Image) []DeliveryResult {
	n.last = image
	return []DeliveryResult{
		{Sink: "telegram", Delivered: true},
		{Sink: "webhook", Error: "not configured"},
	}
}

// pngBytes builds the smallest byte sequence that satisfies DecodePNGSize:
// the signature plus an IHDR chunk carrying the dimensions.
func pngBytes(width, height int) []byte {
	out := make([]byte, 0, 33)
	out = append(out, pngSignature...)
	out = append(out, 0, 0, 0, 13)
	out = append(out, 'I', 'H', 'D', 'R')
	out = binary.BigEndian.AppendUint32(out, uint32(width))
	out = binary.BigEndian.AppendUint32(out, uint32(height))
	return append(out, 8, 6, 0, 0, 0)
}
