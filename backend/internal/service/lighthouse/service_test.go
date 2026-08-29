package lighthouse

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
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

type fakeRunner struct {
	mu           sync.Mutex
	installed    bool
	installCalls int
	reports      map[string][]byte
	failures     map[string]error
	requests     []AuditRequest
}

func (f *fakeRunner) Available() bool { return true }

func (f *fakeRunner) Installed(context.Context, string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.installed, nil
}

func (f *fakeRunner) Install(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.installCalls++
	f.installed = true
	return nil
}

func (f *fakeRunner) Audit(_ context.Context, req AuditRequest) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, req)
	path := pathOf(req.URL)
	if err, bad := f.failures[path]; bad {
		return nil, err
	}
	report, ok := f.reports[path]
	if !ok {
		return nil, errors.New("no report staged")
	}
	return report, nil
}

func pathOf(loopbackURL string) string {
	trimmed := strings.TrimPrefix(loopbackURL, "http://")
	if slash := strings.Index(trimmed, "/"); slash >= 0 {
		return trimmed[slash:]
	}
	return "/"
}

type fakeProjects struct{ meta serviceproject.Meta }

func (f fakeProjects) Get(context.Context, serviceproject.ID) (serviceproject.Meta, error) {
	return f.meta, nil
}

func runningProject() fakeProjects {
	return fakeProjects{meta: serviceproject.Meta{
		ID:            testProject,
		ContainerName: "project-abc",
		Status:        serviceproject.StatusRunning,
	}}
}

func report(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/report.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func newService(t *testing.T, runner *fakeRunner, projects Projects) (*Service, *memoryRepo) {
	t.Helper()
	repo := &memoryRepo{}
	service := New(repo, runner, projects, WithClock(func() time.Time {
		return time.Unix(1_780_000_000, 0)
	}))
	t.Cleanup(service.Wait)
	return service, repo
}

func TestARunAuditsEveryPageAndKeepsTheNumbers(t *testing.T) {
	runner := &fakeRunner{
		installed: true,
		reports:   map[string][]byte{"/": report(t), "/pricing": report(t)},
	}
	service, _ := newService(t, runner, runningProject())

	run, err := service.Start(context.Background(), testProject, RunInput{
		Port:  3000,
		Paths: []string{"/", "/pricing"},
		Label: "before the image swap",
	}, "ops@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if run.Status != StatusRunning {
		t.Fatalf("a fresh run should be running, got %q", run.Status)
	}
	service.Wait()

	overview, err := service.Overview(context.Background(), testProject)
	if err != nil {
		t.Fatal(err)
	}
	if len(overview.Runs) != 1 {
		t.Fatalf("expected one run, got %d", len(overview.Runs))
	}
	stored := overview.Runs[0]
	if stored.Status != StatusReady {
		t.Fatalf("run did not finish: %+v", stored)
	}
	if stored.Label != "before the image swap" {
		t.Fatalf("the label was lost: %q", stored.Label)
	}
	if len(stored.Reports) != 2 {
		t.Fatalf("expected two reports, got %d", len(stored.Reports))
	}
	for _, page := range stored.Reports {
		if !page.Measured() {
			t.Fatalf("page %q produced no numbers: %+v", page.Path, page)
		}
		if len(page.Metrics) == 0 {
			t.Fatalf("page %q has no metrics", page.Path)
		}
	}
}

// Mobile is what Google ranks on, so it is what an unspecified run measures,
// and the choice has to survive all the way to the CLI.
func TestTheFormFactorReachesTheRunner(t *testing.T) {
	runner := &fakeRunner{installed: true, reports: map[string][]byte{"/": report(t)}}
	service, _ := newService(t, runner, runningProject())

	if _, err := service.Start(context.Background(), testProject, RunInput{
		Port: 3000, Paths: []string{"/"}, FormFactor: "desktop",
	}, ""); err != nil {
		t.Fatal(err)
	}
	service.Wait()

	if len(runner.requests) != 1 {
		t.Fatalf("expected one audit request, got %d", len(runner.requests))
	}
	if runner.requests[0].FormFactor != FormFactorDesktop {
		t.Fatalf("desktop did not reach the runner: %q", runner.requests[0].FormFactor)
	}
	if runner.requests[0].URL != "http://127.0.0.1:3000/" {
		t.Fatalf("unexpected URL: %q", runner.requests[0].URL)
	}
}

// Six pages failing the same way is one problem with one fix, and saying so
// once beats saying it six times.
func TestAMissingCLIIsRefusedBeforeAnyPageRuns(t *testing.T) {
	runner := &fakeRunner{installed: false}
	service, _ := newService(t, runner, runningProject())

	_, err := service.Start(context.Background(), testProject, RunInput{Port: 3000, Paths: []string{"/"}}, "")
	if !errors.Is(err, ErrToolingMissing) {
		t.Fatalf("expected ErrToolingMissing, got %v", err)
	}
	if len(runner.requests) != 0 {
		t.Fatal("a page was audited despite the CLI being absent")
	}
}

func TestInstallAddsTheCLIAndTheOverviewThenSaysSo(t *testing.T) {
	runner := &fakeRunner{installed: false}
	service, _ := newService(t, runner, runningProject())

	before, err := service.Overview(context.Background(), testProject)
	if err != nil {
		t.Fatal(err)
	}
	if before.Installed == nil || *before.Installed {
		t.Fatalf("expected the overview to report the CLI missing: %+v", before.Installed)
	}

	if err := service.Install(context.Background(), testProject, "ops@example.com"); err != nil {
		t.Fatal(err)
	}
	if runner.installCalls != 1 {
		t.Fatalf("expected one install, got %d", runner.installCalls)
	}

	after, _ := service.Overview(context.Background(), testProject)
	if after.Installed == nil || !*after.Installed {
		t.Fatalf("the overview did not notice the install: %+v", after.Installed)
	}
}

// A stopped project's container has no CLI because it has no processes. Saying
// "not installed" would put an install button in front of an operator whose
// actual problem is that the project is off.
func TestAStoppedProjectReportsNothingAboutItsTooling(t *testing.T) {
	projects := fakeProjects{meta: serviceproject.Meta{
		ID:            testProject,
		ContainerName: "project-abc",
		Status:        serviceproject.StatusStopped,
	}}
	service, _ := newService(t, &fakeRunner{}, projects)

	overview, err := service.Overview(context.Background(), testProject)
	if err != nil {
		t.Fatal(err)
	}
	if overview.Installed != nil {
		t.Fatalf("expected no answer about tooling, got %v", *overview.Installed)
	}
	if _, err := service.Start(context.Background(), testProject, RunInput{Port: 3000, Paths: []string{"/"}}, ""); !errors.Is(err, ErrNotRunning) {
		t.Fatalf("expected ErrNotRunning, got %v", err)
	}
}

// One bad page must not throw away five good measurements.
func TestOnePageFailingDoesNotSinkTheRun(t *testing.T) {
	runner := &fakeRunner{
		installed: true,
		reports:   map[string][]byte{"/": report(t)},
		failures:  map[string]error{"/gone": errors.New("net::ERR_CONNECTION_REFUSED")},
	}
	service, _ := newService(t, runner, runningProject())

	if _, err := service.Start(context.Background(), testProject, RunInput{
		Port: 3000, Paths: []string{"/", "/gone"},
	}, ""); err != nil {
		t.Fatal(err)
	}
	service.Wait()

	overview, _ := service.Overview(context.Background(), testProject)
	stored := overview.Runs[0]
	if stored.Status != StatusReady {
		t.Fatalf("one failed page failed the whole run: %+v", stored)
	}
	if !stored.Reports[0].Measured() || stored.Reports[1].Error == "" {
		t.Fatalf("expected one measured page and one named failure: %+v", stored.Reports)
	}
}

func TestARunWhereNothingCouldBeMeasuredIsAFailure(t *testing.T) {
	runner := &fakeRunner{
		installed: true,
		failures:  map[string]error{"/": errors.New("net::ERR_CONNECTION_REFUSED")},
	}
	service, _ := newService(t, runner, runningProject())

	if _, err := service.Start(context.Background(), testProject, RunInput{Port: 3000, Paths: []string{"/"}}, ""); err != nil {
		t.Fatal(err)
	}
	service.Wait()

	overview, _ := service.Overview(context.Background(), testProject)
	if overview.Runs[0].Status != StatusFailed {
		t.Fatalf("expected a failed run, got %q", overview.Runs[0].Status)
	}
	if overview.Runs[0].Error == "" {
		t.Fatal("a failed run must say why")
	}
}

// Two runs in one container measure each other: the second one's numbers would
// include the first one's browser competing for the CPU.
func TestASecondRunIsRefusedWhileOneIsInFlight(t *testing.T) {
	release := make(chan struct{})
	runner := &blockingRunner{
		fakeRunner: fakeRunner{installed: true, reports: map[string][]byte{"/": report(t)}},
		gate:       release,
	}
	service := New(&memoryRepo{}, runner, runningProject(), WithClock(time.Now))
	t.Cleanup(func() {
		close(release)
		service.Wait()
	})

	if _, err := service.Start(context.Background(), testProject, RunInput{Port: 3000, Paths: []string{"/"}}, ""); err != nil {
		t.Fatal(err)
	}
	// The first run is parked inside Audit, so the project is busy.
	if _, err := service.Start(context.Background(), testProject, RunInput{Port: 3000, Paths: []string{"/"}}, ""); !errors.Is(err, ErrBusy) {
		t.Fatalf("expected ErrBusy, got %v", err)
	}
}

type blockingRunner struct {
	fakeRunner
	gate chan struct{}
}

func (b *blockingRunner) Audit(ctx context.Context, req AuditRequest) ([]byte, error) {
	select {
	case <-b.gate:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return b.fakeRunner.Audit(ctx, req)
}

func TestRunsAreListedNewestFirstAndCanBeDeleted(t *testing.T) {
	runner := &fakeRunner{installed: true, reports: map[string][]byte{"/": report(t)}}
	repo := &memoryRepo{}
	clock := int64(1_780_000_000)
	service := New(repo, runner, runningProject(), WithClock(func() time.Time {
		clock++
		return time.Unix(clock, 0)
	}))
	t.Cleanup(service.Wait)

	for index := 0; index < 3; index++ {
		if _, err := service.Start(context.Background(), testProject, RunInput{Port: 3000, Paths: []string{"/"}}, ""); err != nil {
			t.Fatal(err)
		}
		service.Wait()
	}

	overview, _ := service.Overview(context.Background(), testProject)
	if len(overview.Runs) != 3 {
		t.Fatalf("expected three runs, got %d", len(overview.Runs))
	}
	for index := 1; index < len(overview.Runs); index++ {
		if overview.Runs[index-1].CreatedAt < overview.Runs[index].CreatedAt {
			t.Fatal("runs are not newest first")
		}
	}

	target := overview.Runs[1].ID
	if err := service.Delete(context.Background(), testProject, target); err != nil {
		t.Fatal(err)
	}
	after, _ := service.Overview(context.Background(), testProject)
	if len(after.Runs) != 2 {
		t.Fatalf("delete did not remove the run: %d left", len(after.Runs))
	}
	if err := service.Delete(context.Background(), testProject, target); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on a second delete, got %v", err)
	}
}
