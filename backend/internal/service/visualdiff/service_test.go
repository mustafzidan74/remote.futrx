package visualdiff

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"strings"
	"sync"
	"testing"
	"time"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	servicescreenshot "github.com/futrx-com/remote.futrx.com/internal/service/screenshot"
)

const testProject = serviceproject.ID("abc123")

type memoryRepo struct {
	mu    sync.Mutex
	state State
}

func (m *memoryRepo) Load(context.Context, serviceproject.ID) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state, nil
}

func (m *memoryRepo) Update(
	_ context.Context,
	_ serviceproject.ID,
	fn func(State) (State, error),
) (State, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	next, err := fn(m.state)
	if err != nil {
		return State{}, err
	}
	m.state = next
	return next, nil
}

type memoryBlobs struct {
	mu      sync.Mutex
	files   map[string][]byte
	removed []string
	failOn  string
}

func newBlobs() *memoryBlobs { return &memoryBlobs{files: map[string][]byte{}} }

func (m *memoryBlobs) Write(_ serviceproject.ID, file string, data []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failOn != "" && strings.Contains(file, m.failOn) {
		return errors.New("disk full")
	}
	m.files[file] = append([]byte(nil), data...)
	return nil
}

func (m *memoryBlobs) Read(_ serviceproject.ID, file string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	data, ok := m.files[file]
	if !ok {
		return nil, errors.New("not found")
	}
	return data, nil
}

func (m *memoryBlobs) Remove(_ serviceproject.ID, file string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.files, file)
	m.removed = append(m.removed, file)
	return nil
}

func (m *memoryBlobs) count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.files)
}

// fakeCapturer answers with whatever page the test has staged for a path, so a
// test can say "this page changed between the two runs" without a browser.
type fakeCapturer struct {
	mu    sync.Mutex
	pages map[string][][]byte // path -> one image per successive call
	calls map[string]int
	fail  map[string]error
}

func (f *fakeCapturer) Available() bool { return true }

func (f *fakeCapturer) Capture(_ context.Context, req servicescreenshot.CaptureRequest) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	path := pathOf(req.URL)
	if err, bad := f.fail[path]; bad {
		return nil, err
	}
	shots := f.pages[path]
	if len(shots) == 0 {
		return nil, errors.New("no page staged")
	}
	index := f.calls[path]
	if index >= len(shots) {
		index = len(shots) - 1
	}
	f.calls[path]++
	return shots[index], nil
}

func pathOf(loopbackURL string) string {
	if index := strings.Index(loopbackURL, "/"); index >= 0 {
		if slash := strings.Index(loopbackURL[len("http://"):], "/"); slash >= 0 {
			return loopbackURL[len("http://")+slash:]
		}
	}
	return "/"
}

type fakeProjects struct {
	meta serviceproject.Meta
	err  error
}

func (f fakeProjects) Get(context.Context, serviceproject.ID) (serviceproject.Meta, error) {
	return f.meta, f.err
}

func runningProject() fakeProjects {
	return fakeProjects{meta: serviceproject.Meta{
		ID:            testProject,
		ContainerName: "project-abc",
		Status:        serviceproject.StatusRunning,
	}}
}

func page(t *testing.T, width, height int, fill color.RGBA) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, fill)
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}

func newService(t *testing.T, capturer *fakeCapturer, projects Projects) (*Service, *memoryRepo, *memoryBlobs) {
	t.Helper()
	repo := &memoryRepo{}
	blobs := newBlobs()
	service := New(repo, blobs, capturer, projects, WithClock(func() time.Time {
		return time.Unix(1_780_000_000, 0)
	}))
	t.Cleanup(service.Wait)
	return service, repo, blobs
}

// settle waits for the background run to finish. The runs are asynchronous by
// design, so every test that asserts on a result waits on the service's own
// wait group rather than sleeping.
func settle(service *Service) { service.Wait() }

func TestBaselineThenComparisonFindsTheChangedPage(t *testing.T) {
	white := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	black := color.RGBA{A: 0xFF}

	capturer := &fakeCapturer{
		calls: map[string]int{},
		pages: map[string][][]byte{
			// The page the operator meant to change.
			"/pricing": {page(t, 100, 100, white), page(t, 100, 100, black)},
			// The page they did not touch, and the whole reason this exists.
			"/": {page(t, 100, 100, white), page(t, 100, 100, white)},
		},
	}
	service, _, blobs := newService(t, capturer, runningProject())

	baseline, err := service.SetBaseline(context.Background(), testProject, BaselineInput{
		Port:  3000,
		Paths: []string{"/", "/pricing"},
	}, "ops@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if baseline.Status != StatusRunning {
		t.Fatalf("a fresh baseline should be running, got %q", baseline.Status)
	}
	settle(service)

	overview, err := service.Overview(context.Background(), testProject)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Baseline == nil || overview.Baseline.Status != StatusReady {
		t.Fatalf("baseline did not finish: %+v", overview.Baseline)
	}
	if len(overview.Baseline.Pages) != 2 {
		t.Fatalf("expected two baseline pages, got %d", len(overview.Baseline.Pages))
	}
	for _, shot := range overview.Baseline.Pages {
		if !shot.Captured() || shot.URL == "" {
			t.Fatalf("baseline page %q was not stored with a read URL: %+v", shot.Path, shot)
		}
	}

	if _, err := service.Compare(context.Background(), testProject, "after the CSS edit", "ops@example.com"); err != nil {
		t.Fatal(err)
	}
	settle(service)

	overview, err = service.Overview(context.Background(), testProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Comparisons) != 1 {
		t.Fatalf("expected one comparison, got %d", len(overview.Comparisons))
	}
	comparison := overview.Comparisons[0]
	if comparison.Status != StatusReady {
		t.Fatalf("comparison did not finish: %+v", comparison)
	}
	if comparison.ChangedPages != 1 {
		t.Fatalf("expected exactly one changed page, got %d: %+v", comparison.ChangedPages, comparison.Pages)
	}
	if comparison.MaxChangedPercent != 100 {
		t.Fatalf("expected the changed page at 100%%, got %v", comparison.MaxChangedPercent)
	}

	for _, diff := range comparison.Pages {
		switch diff.Path {
		case "/":
			if diff.Changed() {
				t.Fatalf("an untouched page was reported as changed: %+v", diff)
			}
		case "/pricing":
			if !diff.Changed() {
				t.Fatalf("the edited page was not reported as changed: %+v", diff)
			}
			if diff.BeforeURL == "" || diff.AfterURL == "" || diff.DiffURL == "" {
				t.Fatalf("a changed page is missing one of its three images: %+v", diff)
			}
		default:
			t.Fatalf("unexpected page %q", diff.Path)
		}
	}
	if blobs.count() == 0 {
		t.Fatal("no images were stored")
	}
}

// A page that grows is the case a percentage alone reads wrong, so it has to
// survive the whole pipeline and not just the diff engine.
func TestAPageThatGrewIsReportedAsChanged(t *testing.T) {
	white := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	capturer := &fakeCapturer{
		calls: map[string]int{},
		pages: map[string][][]byte{"/": {page(t, 80, 80, white), page(t, 80, 200, white)}},
	}
	service, _, _ := newService(t, capturer, runningProject())

	if _, err := service.SetBaseline(context.Background(), testProject, BaselineInput{Port: 3000, Paths: []string{"/"}}, ""); err != nil {
		t.Fatal(err)
	}
	settle(service)
	if _, err := service.Compare(context.Background(), testProject, "", ""); err != nil {
		t.Fatal(err)
	}
	settle(service)

	overview, _ := service.Overview(context.Background(), testProject)
	diff := overview.Comparisons[0].Pages[0]
	if !diff.SizeChanged {
		t.Fatalf("a page that grew was not flagged: %+v", diff)
	}
	if !diff.Changed() {
		t.Fatalf("a resized page must count as changed: %+v", diff)
	}
}

// One bad page must not sink the run: eleven good pages and a named failure is
// a useful answer.
func TestOneUnreachablePageDoesNotFailTheRun(t *testing.T) {
	white := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	capturer := &fakeCapturer{
		calls: map[string]int{},
		pages: map[string][][]byte{"/": {page(t, 60, 60, white)}},
		fail:  map[string]error{"/gone": errors.New("net::ERR_CONNECTION_REFUSED")},
	}
	service, _, _ := newService(t, capturer, runningProject())

	if _, err := service.SetBaseline(context.Background(), testProject, BaselineInput{
		Port:  3000,
		Paths: []string{"/", "/gone"},
	}, ""); err != nil {
		t.Fatal(err)
	}
	settle(service)

	overview, _ := service.Overview(context.Background(), testProject)
	if overview.Baseline.Status != StatusReady {
		t.Fatalf("one failed page failed the whole baseline: %+v", overview.Baseline)
	}
	var failed, captured int
	for _, shot := range overview.Baseline.Pages {
		if shot.Error != "" {
			failed++
			continue
		}
		captured++
	}
	if failed != 1 || captured != 1 {
		t.Fatalf("expected one captured and one failed page, got %d/%d", captured, failed)
	}

	// The comparison skips the page that has no baseline image rather than
	// calling it 100% changed.
	if _, err := service.Compare(context.Background(), testProject, "", ""); err != nil {
		t.Fatal(err)
	}
	settle(service)
	overview, _ = service.Overview(context.Background(), testProject)
	if len(overview.Comparisons[0].Pages) != 1 {
		t.Fatalf("expected only the captured page compared, got %+v", overview.Comparisons[0].Pages)
	}
}

func TestBaselineFailsOnlyWhenNoPageCouldBePhotographed(t *testing.T) {
	capturer := &fakeCapturer{
		calls: map[string]int{},
		pages: map[string][][]byte{},
		fail:  map[string]error{"/": errors.New("net::ERR_CONNECTION_REFUSED")},
	}
	service, _, _ := newService(t, capturer, runningProject())

	if _, err := service.SetBaseline(context.Background(), testProject, BaselineInput{Port: 3000, Paths: []string{"/"}}, ""); err != nil {
		t.Fatal(err)
	}
	settle(service)

	overview, _ := service.Overview(context.Background(), testProject)
	if overview.Baseline.Status != StatusFailed {
		t.Fatalf("expected a failed baseline, got %q", overview.Baseline.Status)
	}
	if overview.Baseline.Error == "" {
		t.Fatal("a failed baseline must say why")
	}
}

func TestComparingWithoutABaselineIsRefused(t *testing.T) {
	service, _, _ := newService(t, &fakeCapturer{calls: map[string]int{}}, runningProject())
	if _, err := service.Compare(context.Background(), testProject, "", ""); !errors.Is(err, ErrNoBaseline) {
		t.Fatalf("expected ErrNoBaseline, got %v", err)
	}
}

func TestAStoppedProjectIsRefusedBeforeAnyBrowserRuns(t *testing.T) {
	projects := fakeProjects{meta: serviceproject.Meta{
		ID:            testProject,
		ContainerName: "project-abc",
		Status:        serviceproject.StatusStopped,
	}}
	service, _, _ := newService(t, &fakeCapturer{calls: map[string]int{}}, projects)
	_, err := service.SetBaseline(context.Background(), testProject, BaselineInput{Port: 3000, Paths: []string{"/"}}, "")
	if !errors.Is(err, ErrNotRunning) {
		t.Fatalf("expected ErrNotRunning, got %v", err)
	}
}

// Re-baselining is the operator saying "this is correct now". The old
// comparisons measured something else and must not survive to be misread.
func TestANewBaselineDiscardsTheOldComparisonsAndTheirImages(t *testing.T) {
	white := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	capturer := &fakeCapturer{
		calls: map[string]int{},
		pages: map[string][][]byte{"/": {page(t, 50, 50, white)}},
	}
	service, _, blobs := newService(t, capturer, runningProject())

	if _, err := service.SetBaseline(context.Background(), testProject, BaselineInput{Port: 3000, Paths: []string{"/"}}, ""); err != nil {
		t.Fatal(err)
	}
	settle(service)
	if _, err := service.Compare(context.Background(), testProject, "", ""); err != nil {
		t.Fatal(err)
	}
	settle(service)
	if blobs.count() == 0 {
		t.Fatal("expected images from the first round")
	}

	if _, err := service.SetBaseline(context.Background(), testProject, BaselineInput{Port: 3000, Paths: []string{"/"}}, ""); err != nil {
		t.Fatal(err)
	}
	settle(service)

	overview, _ := service.Overview(context.Background(), testProject)
	if len(overview.Comparisons) != 0 {
		t.Fatalf("comparisons against the old baseline survived: %+v", overview.Comparisons)
	}
	// Exactly the new baseline's one page should remain on disk.
	if blobs.count() != 1 {
		t.Fatalf("expected only the new baseline image, found %d files", blobs.count())
	}
}

func TestOnlyThisProjectsOwnFilesCanBeRead(t *testing.T) {
	white := color.RGBA{R: 0xFF, G: 0xFF, B: 0xFF, A: 0xFF}
	capturer := &fakeCapturer{
		calls: map[string]int{},
		pages: map[string][][]byte{"/": {page(t, 40, 40, white)}},
	}
	service, _, blobs := newService(t, capturer, runningProject())
	if _, err := service.SetBaseline(context.Background(), testProject, BaselineInput{Port: 3000, Paths: []string{"/"}}, ""); err != nil {
		t.Fatal(err)
	}
	settle(service)

	overview, _ := service.Overview(context.Background(), testProject)
	stored := overview.Baseline.Pages[0].File
	if _, err := service.Image(context.Background(), testProject, stored); err != nil {
		t.Fatalf("a file this project owns could not be read: %v", err)
	}

	// A file that exists on disk but is not referenced by this project's
	// records is not readable through the project's route.
	_ = blobs.Write(testProject, "deadbeef-base-0000000000000000.png", []byte("x"))
	if _, err := service.Image(context.Background(), testProject, "deadbeef-base-0000000000000000.png"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unreferenced file was served: %v", err)
	}
	if _, err := service.Image(context.Background(), testProject, "../../etc/passwd"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a traversal attempt was not refused: %v", err)
	}
}

func TestNormalizeRejectsWhatTheContainerShouldNeverBeAsked(t *testing.T) {
	tests := []struct {
		name string
		in   BaselineInput
		want error
	}{
		{"no port", BaselineInput{Paths: []string{"/"}}, ErrInvalidPort},
		{"no paths", BaselineInput{Port: 3000}, ErrNoPaths},
		{"relative path", BaselineInput{Port: 3000, Paths: []string{"about"}}, ErrInvalidPath},
		{"traversal", BaselineInput{Port: 3000, Paths: []string{"/../etc"}}, ErrInvalidPath},
		{"shell characters", BaselineInput{Port: 3000, Paths: []string{"/a b"}}, ErrInvalidPath},
		{"viewport too small", BaselineInput{Port: 3000, Paths: []string{"/"}, Width: 10, Height: 10}, ErrInvalidSize},
		{"too many pages", BaselineInput{Port: 3000, Paths: manyPaths(MaxPaths + 1)}, ErrTooManyPaths},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tc.in.Normalize(); !errors.Is(err, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, err)
			}
		})
	}
}

func TestNormalizeCollapsesDuplicatesAndFillsDefaults(t *testing.T) {
	out, err := BaselineInput{Port: 3000, Paths: []string{"/", "/", " /about "}}.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Paths) != 2 || out.Paths[0] != "/" || out.Paths[1] != "/about" {
		t.Fatalf("unexpected paths: %#v", out.Paths)
	}
	if out.Width != DefaultWidth || out.Height != DefaultHeight {
		t.Fatalf("defaults were not filled: %dx%d", out.Width, out.Height)
	}
	if out.Threshold != DefaultThreshold {
		t.Fatalf("expected the default threshold, got %v", out.Threshold)
	}
	// A nonsense threshold falls back rather than being honoured.
	loud, _ := BaselineInput{Port: 3000, Paths: []string{"/"}, Threshold: 9}.Normalize()
	if loud.Threshold != DefaultThreshold {
		t.Fatalf("an out-of-range threshold was accepted: %v", loud.Threshold)
	}
}

func manyPaths(count int) []string {
	paths := make([]string, 0, count)
	for index := 0; index < count; index++ {
		paths = append(paths, "/page-"+string(rune('a'+index%26))+strings.Repeat("x", index/26))
	}
	return paths
}
