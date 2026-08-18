package health

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

type fakeProjects struct {
	metas []serviceproject.Meta
	err   error
}

func (f *fakeProjects) List(context.Context) ([]serviceproject.Meta, error) {
	return f.metas, f.err
}

type fakeVitals struct {
	byName map[string]serviceproject.ContainerInspect
	err    error
	calls  int
}

func (f *fakeVitals) Vitals(_ context.Context, name string) (serviceproject.ContainerInspect, error) {
	f.calls++
	if f.err != nil {
		return serviceproject.ContainerInspect{}, f.err
	}
	return f.byName[name], nil
}

type fakeListeners struct {
	byName map[string][]serviceproject.ContainerApp
}

func (f *fakeListeners) List(_ context.Context, name string) ([]serviceproject.ContainerApp, error) {
	return f.byName[name], nil
}

type fakeProber struct {
	result Probe
	slugs  []string
	ports  []int
}

func (f *fakeProber) Probe(_ context.Context, slug string, port int) Probe {
	f.slugs = append(f.slugs, slug)
	f.ports = append(f.ports, port)
	result := f.result
	result.Attempted = true
	result.Port = port
	return result
}

type recordingPublisher struct {
	mu   sync.Mutex
	rows []publication
}

type publication struct {
	id     serviceproject.ID
	health *ProjectHealth
}

func (r *recordingPublisher) PublishProjectHealth(id serviceproject.ID, health *ProjectHealth) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, publication{id: id, health: health})
}

func (r *recordingPublisher) last() publication {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.rows) == 0 {
		return publication{}
	}
	return r.rows[len(r.rows)-1]
}

type recordingAlerter struct {
	alerts []ProjectHealth
	names  []string
}

func (r *recordingAlerter) ProjectHealthChanged(
	_ context.Context,
	project serviceproject.Meta,
	health ProjectHealth,
) {
	r.names = append(r.names, project.Slug)
	r.alerts = append(r.alerts, health)
}

func runningProject(id, slug string) serviceproject.Meta {
	return serviceproject.Meta{
		ID:            serviceproject.ID(id),
		Name:          slug,
		Slug:          slug,
		ContainerName: slug + "-container",
		Status:        serviceproject.StatusRunning,
	}
}

func inspect(used, limit int64) serviceproject.ContainerInspect {
	return serviceproject.ContainerInspect{
		State: serviceproject.ContainerStateRunning,
		Resources: &serviceproject.ResourceInfo{
			MemoryCurrentBytes: used,
			MemoryTotalBytes:   limit,
		},
		Limits: &serviceproject.ContainerLimits{CPU: "2", Memory: "1GiB"},
	}
}

// testClock is a hand-wound clock: sweeps only move time when the test says
// so, which is what makes the CPU delta assertable.
type testClock struct {
	at time.Time
}

func newTestClock() *testClock {
	return &testClock{at: time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)}
}

func (c *testClock) now() time.Time { return c.at }

func (c *testClock) advance(d time.Duration) { c.at = c.at.Add(d) }

// newTestService wires a monitor over fakes and a stopped clock.
func newTestService(deps Dependencies, options ...Option) (*Service, *testClock) {
	clock := newTestClock()
	return New(deps, append([]Option{WithClock(clock.now)}, options...)...), clock
}

func TestCheckPublishesAndAlertsOnSettledTransition(t *testing.T) {
	project := runningProject("aaaa", "wp-project")
	vitals := &fakeVitals{byName: map[string]serviceproject.ContainerInspect{
		project.ContainerName: inspect(950*gib/1000, gib),
	}}
	publisher := &recordingPublisher{}
	alerter := &recordingAlerter{}
	service, _ := newTestService(Dependencies{
		Projects: &fakeProjects{metas: []serviceproject.Meta{project}},
		Vitals:   vitals,
		Listeners: &fakeListeners{byName: map[string][]serviceproject.ContainerApp{
			project.ContainerName: {{Port: 8842}, {Port: 6080}},
		}},
		Publisher: publisher,
		Alerter:   alerter,
	}, WithProber(&fakeProber{}))

	service.Check(context.Background())

	got := publisher.last()
	if got.id != project.ID || got.health == nil {
		t.Fatalf("published = %+v, want a row for %q", got, project.ID)
	}
	if got.health.Status != StatusCrit {
		t.Fatalf("status = %q, want %q", got.health.Status, StatusCrit)
	}
	if !reflect.DeepEqual(got.health.Listeners, []int{6080, 8842}) {
		t.Fatalf("listeners = %v, want ascending [6080 8842]", got.health.Listeners)
	}
	if got.health.PreviewOK != nil {
		t.Fatalf("previewOk = %v, want nil: every listener is platform plumbing", *got.health.PreviewOK)
	}
	if got.health.LastCheckedAt == 0 {
		t.Fatal("lastCheckedAt was not stamped")
	}
	if len(alerter.alerts) != 1 || alerter.names[0] != "wp-project" {
		t.Fatalf("alerts = %d for %v, want one for wp-project", len(alerter.alerts), alerter.names)
	}

	// A second identical sweep must not ping again.
	service.Check(context.Background())
	if len(alerter.alerts) != 1 {
		t.Fatalf("alerts = %d after a steady sweep, want 1", len(alerter.alerts))
	}
}

func TestCheckProbesTheFirstApplicationPort(t *testing.T) {
	project := runningProject("bbbb", "shop")
	prober := &fakeProber{result: Probe{StatusCode: 200}}
	service, _ := newTestService(Dependencies{
		Projects: &fakeProjects{metas: []serviceproject.Meta{project}},
		Vitals: &fakeVitals{byName: map[string]serviceproject.ContainerInspect{
			project.ContainerName: inspect(gib/4, gib),
		}},
		Listeners: &fakeListeners{byName: map[string][]serviceproject.ContainerApp{
			project.ContainerName: {{Port: 8842}, {Port: 5173}, {Port: 3000}},
		}},
	}, WithProber(prober))

	service.Check(context.Background())

	if !reflect.DeepEqual(prober.slugs, []string{"shop"}) || !reflect.DeepEqual(prober.ports, []int{3000}) {
		t.Fatalf("probed %v on %v, want shop:3000 once", prober.slugs, prober.ports)
	}
	rows := service.Snapshot([]serviceproject.ID{project.ID})
	if len(rows) != 1 || rows[0].PreviewOK == nil || !*rows[0].PreviewOK {
		t.Fatalf("snapshot = %+v, want one row with previewOk true", rows)
	}
	if rows[0].Status != StatusOK {
		t.Fatalf("status = %q, want %q", rows[0].Status, StatusOK)
	}
}

func TestCheckSkipsProjectsThatAreNotRunning(t *testing.T) {
	stopped := runningProject("cccc", "idle")
	stopped.Status = serviceproject.StatusStopped
	vitals := &fakeVitals{byName: map[string]serviceproject.ContainerInspect{}}
	service, _ := newTestService(Dependencies{
		Projects: &fakeProjects{metas: []serviceproject.Meta{stopped}},
		Vitals:   vitals,
	}, WithProber(&fakeProber{}))

	service.Check(context.Background())

	if vitals.calls != 0 {
		t.Fatalf("vitals called %d times for a stopped project, want 0", vitals.calls)
	}
	if rows := service.Snapshot([]serviceproject.ID{stopped.ID}); len(rows) != 0 {
		t.Fatalf("snapshot = %+v, want nothing cached", rows)
	}
}

func TestCheckForgetsProjectsThatStop(t *testing.T) {
	project := runningProject("dddd", "shop")
	catalog := &fakeProjects{metas: []serviceproject.Meta{project}}
	publisher := &recordingPublisher{}
	service, _ := newTestService(Dependencies{
		Projects: catalog,
		Vitals: &fakeVitals{byName: map[string]serviceproject.ContainerInspect{
			project.ContainerName: inspect(gib/4, gib),
		}},
		Publisher: publisher,
	}, WithProber(&fakeProber{}))

	service.Check(context.Background())
	if rows := service.Snapshot([]serviceproject.ID{project.ID}); len(rows) != 1 {
		t.Fatalf("snapshot = %+v, want one cached row", rows)
	}

	stopped := project
	stopped.Status = serviceproject.StatusStopped
	catalog.metas = []serviceproject.Meta{stopped}
	service.Check(context.Background())

	if rows := service.Snapshot([]serviceproject.ID{project.ID}); len(rows) != 0 {
		t.Fatalf("snapshot = %+v, want the row dropped", rows)
	}
	if last := publisher.last(); last.id != project.ID || last.health != nil {
		t.Fatalf("last publication = %+v, want a cleared row for %q", last, project.ID)
	}
}

func TestCheckReportsUnknownWhenLXDIsUnreachable(t *testing.T) {
	project := runningProject("eeee", "shop")
	alerter := &recordingAlerter{}
	service, _ := newTestService(Dependencies{
		Projects: &fakeProjects{metas: []serviceproject.Meta{project}},
		Vitals:   &fakeVitals{err: errors.New("lxd is not running")},
		Alerter:  alerter,
	}, WithProber(&fakeProber{}))

	service.Check(context.Background())
	service.Check(context.Background())

	rows := service.Snapshot([]serviceproject.ID{project.ID})
	if len(rows) != 1 || rows[0].Status != StatusUnknown {
		t.Fatalf("snapshot = %+v, want a single unknown row", rows)
	}
	if len(alerter.alerts) != 0 {
		t.Fatalf("alerts = %d, want none: an unreachable daemon is not a project event", len(alerter.alerts))
	}
}

func TestCPUPercentNeedsTwoSamples(t *testing.T) {
	project := runningProject("ffff", "busy")
	snapshot := inspect(gib/4, gib)
	snapshot.Resources.CPUUsageSeconds = 100
	vitals := &fakeVitals{byName: map[string]serviceproject.ContainerInspect{
		project.ContainerName: snapshot,
	}}
	service, clock := newTestService(Dependencies{
		Projects: &fakeProjects{metas: []serviceproject.Meta{project}},
		Vitals:   vitals,
	}, WithProber(&fakeProber{}))

	service.Check(context.Background())
	if rows := service.Snapshot([]serviceproject.ID{project.ID}); rows[0].CPUPct != nil {
		t.Fatalf("cpuPct = %v on the first sweep, want nil", *rows[0].CPUPct)
	}

	// 60 CPU seconds over the next minute, across the two cores the limits
	// declare, is half of one core: 50%.
	next := snapshot
	next.Resources = &serviceproject.ResourceInfo{
		MemoryCurrentBytes: snapshot.Resources.MemoryCurrentBytes,
		MemoryTotalBytes:   snapshot.Resources.MemoryTotalBytes,
		CPUUsageSeconds:    160,
	}
	vitals.byName[project.ContainerName] = next
	clock.advance(time.Minute)
	service.Check(context.Background())

	rows := service.Snapshot([]serviceproject.ID{project.ID})
	if rows[0].CPUPct == nil || *rows[0].CPUPct != 50 {
		t.Fatalf("cpuPct = %v, want 50", rows[0].CPUPct)
	}
}

func TestDiskPercentNeedsAQuota(t *testing.T) {
	withQuota := inspect(gib/4, gib)
	withQuota.Resources.DiskUsageBytes = 5 * gib
	withQuota.Limits.Disk = "20GiB"
	if got := diskPercent(withQuota); got == nil || *got != 25 {
		t.Fatalf("diskUsedPct = %v, want 25", got)
	}

	unquotaed := inspect(gib/4, gib)
	unquotaed.Resources.DiskUsageBytes = 5 * gib
	if got := diskPercent(unquotaed); got != nil {
		t.Fatalf("diskUsedPct = %v, want nil without a quota", *got)
	}
}

func TestMemoryUsageFallsBackToTheConfiguredLimit(t *testing.T) {
	info := serviceproject.ContainerInspect{
		State: serviceproject.ContainerStateRunning,
		Resources: &serviceproject.ResourceInfo{
			MemoryCurrentBytes: gib / 2,
		},
		Limits: &serviceproject.ContainerLimits{Memory: "1536MiB"},
	}
	used, limit := memoryUsage(info)
	if used != gib/2 || limit != 1536*(1<<20) {
		t.Fatalf("memoryUsage = (%d, %d), want (%d, %d)", used, limit, gib/2, 1536*(1<<20))
	}
}

func TestDisabledMonitorNeverSweeps(t *testing.T) {
	vitals := &fakeVitals{byName: map[string]serviceproject.ContainerInspect{}}
	service, _ := newTestService(Dependencies{
		Projects: &fakeProjects{metas: []serviceproject.Meta{runningProject("1111", "shop")}},
		Vitals:   vitals,
		Interval: -1,
	})
	if service.Enabled() {
		t.Fatal("a negative interval must disable the monitor")
	}
	service.Start(context.Background())
	if vitals.calls != 0 {
		t.Fatalf("vitals called %d times, want 0", vitals.calls)
	}
}
