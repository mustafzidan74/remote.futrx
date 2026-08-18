package dashboard

import (
	"context"
	"errors"
	"testing"
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicehealth "github.com/futrx-com/remote.futrx.com/internal/service/health"
	servicemonitoring "github.com/futrx-com/remote.futrx.com/internal/service/monitoring"
	servicenotify "github.com/futrx-com/remote.futrx.com/internal/service/notify"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	serviceresources "github.com/futrx-com/remote.futrx.com/internal/service/resources"
	serviceschedule "github.com/futrx-com/remote.futrx.com/internal/service/schedule"
	serviceserverinfo "github.com/futrx-com/remote.futrx.com/internal/service/serverinfo"
	servicesnapshot "github.com/futrx-com/remote.futrx.com/internal/service/snapshot"
	serviceusage "github.com/futrx-com/remote.futrx.com/internal/service/usage"
)

// ── fakes ───────────────────────────────────────────────────────────────────
//
// Each fake answers one port with canned data and records the caller identity
// it was asked with, which is how the scoping test proves the aggregator does
// not quietly widen anyone's view.

type fakeProjects struct {
	live    []serviceproject.Meta
	trashed []serviceproject.Meta
	err     error

	sawEmail string
	sawAdmin bool
}

func (p *fakeProjects) ListVisible(
	_ context.Context,
	callerEmail string,
	isAdmin bool,
) ([]serviceproject.Meta, error) {
	p.sawEmail, p.sawAdmin = callerEmail, isAdmin
	return p.live, p.err
}

func (p *fakeProjects) ListTrashed(
	_ context.Context,
	_ string,
	_ bool,
) ([]serviceproject.Meta, error) {
	return p.trashed, nil
}

type fakeChats struct {
	metas    []servicechat.Meta
	err      error
	sawEmail string
}

func (c *fakeChats) List(_ context.Context, callerEmail string, _ bool) ([]servicechat.Meta, error) {
	c.sawEmail = callerEmail
	return c.metas, c.err
}

type fakeUsage struct {
	summaries []serviceusage.Summary
	records   []serviceusage.Record

	queries    []serviceusage.Query
	sawEmail   string
	summaryErr error
}

func (u *fakeUsage) Summary(
	_ context.Context,
	query serviceusage.Query,
	callerEmail string,
	_ bool,
) (serviceusage.Summary, error) {
	u.sawEmail = callerEmail
	u.queries = append(u.queries, query)
	if u.summaryErr != nil {
		return serviceusage.Summary{}, u.summaryErr
	}
	if len(u.summaries) == 0 {
		return serviceusage.Summary{}, nil
	}
	next := u.summaries[0]
	if len(u.summaries) > 1 {
		u.summaries = u.summaries[1:]
	}
	return next, nil
}

func (u *fakeUsage) Records(
	_ context.Context,
	_ serviceusage.RecordQuery,
	_ string,
	_ bool,
) (serviceusage.RecordPage, error) {
	return serviceusage.RecordPage{Records: u.records}, nil
}

type fakeHealth struct {
	enabled bool
	rows    []servicehealth.ProjectHealth
}

func (h *fakeHealth) Enabled() bool { return h.enabled }

func (h *fakeHealth) Snapshot([]serviceproject.ID) []servicehealth.ProjectHealth { return h.rows }

type fakeSchedules struct {
	tasks []serviceschedule.Task
}

func (s *fakeSchedules) List(_ context.Context, _ string, _ bool) ([]serviceschedule.Task, error) {
	return s.tasks, nil
}

type fakeSnapshots struct {
	available bool
	byProject map[serviceproject.ID][]servicesnapshot.Snapshot
	failFor   serviceproject.ID
}

func (s *fakeSnapshots) Available() bool { return s.available }

func (s *fakeSnapshots) List(
	_ context.Context,
	projectID serviceproject.ID,
) ([]servicesnapshot.Snapshot, error) {
	if projectID == s.failFor {
		return nil, errors.New("archive index unreadable")
	}
	return s.byProject[projectID], nil
}

type fakeNotifications struct {
	config servicenotify.Config
}

func (n *fakeNotifications) Config() servicenotify.Config { return n.config }

type fakePlatform struct {
	report servicemonitoring.Report
	calls  int
}

func (p *fakePlatform) Report(context.Context, bool) servicemonitoring.Report {
	p.calls++
	return p.report
}

type fakeCapacity struct {
	view serviceresources.View
}

func (c *fakeCapacity) Get(context.Context) serviceresources.View { return c.view }

type fakeBackups struct {
	info serviceserverinfo.BackupInfo
}

func (b *fakeBackups) Backup(context.Context) serviceserverinfo.BackupInfo { return b.info }

// ── fixtures ────────────────────────────────────────────────────────────────

var testNow = time.Date(2026, 3, 10, 12, 30, 0, 0, time.UTC)

func at(offset time.Duration) int64 { return testNow.Add(offset).UnixMilli() }

func cost(value float64) *float64 { return &value }

// newTestService wires a box with two projects, three chats and a small
// ledger. Individual tests swap one dependency to isolate what they assert.
func newTestService(t *testing.T, mutate func(*Dependencies)) *Service {
	t.Helper()
	deps := Dependencies{
		Projects: &fakeProjects{live: []serviceproject.Meta{
			{ID: "p1", Name: "Shop", Slug: "shop", Status: serviceproject.StatusRunning},
			{ID: "p2", Name: "Blog", Slug: "blog", Status: serviceproject.StatusStopped},
		}},
		Chats: &fakeChats{metas: []servicechat.Meta{
			{ID: "c1", Title: "Checkout", ProjectID: "p1", LastMessageAt: at(-time.Hour)},
			{ID: "c2", Title: "Newer", ProjectID: "p1", LastMessageAt: at(-time.Minute), Running: true},
			{ID: "c3", Title: "Posts", ProjectID: "p2", LastMessageAt: at(-48 * time.Hour)},
		}},
		Usage: &fakeUsage{
			summaries: []serviceusage.Summary{
				{Totals: serviceusage.Totals{Runs: 12, TotalTokens: 5000, CostUSD: 4}},
				{Totals: serviceusage.Totals{Runs: 8, TotalTokens: 3000, CostUSD: 2}},
			},
			records: []serviceusage.Record{{
				At: at(-2 * time.Hour), ChatID: "c1", ProjectID: "p1", RunID: "r1",
				Provider: "claude", Model: "sonnet", DurationMs: 60_000,
				InputTokens: 100, OutputTokens: 50, CostUSD: cost(0.25),
			}},
		},
		Health: &fakeHealth{enabled: true, rows: []servicehealth.ProjectHealth{{
			ProjectID: "p1",
			Status:    servicehealth.StatusOK,
			MemoryPct: 42,
			// 8842 is the IDE proxy and 9222 the debug port: neither is the
			// project's own app, so only 3000 may become a preview chip.
			Listeners: []int{8842, 9222, 3000},
		}}},
		Schedules:     &fakeSchedules{},
		Notifications: &fakeNotifications{config: servicenotify.Config{Enabled: true}},
		Platform: &fakePlatform{report: servicemonitoring.Report{
			Status: servicemonitoring.StatusOK, Version: "1.2.3",
		}},
		Capacity: &fakeCapacity{view: serviceresources.View{
			Host:     serviceresources.HostCapacity{BudgetMemoryBytes: 1000, CommittedBytes: 200, RunningContainers: 1},
			Settings: serviceresources.Settings{MaxRunningContainers: 6},
		}},
		Backups:        &fakeBackups{info: serviceserverinfo.BackupInfo{Readable: true, LastAt: at(-time.Hour)}},
		TrashRetention: 7 * 24 * time.Hour,
	}
	if mutate != nil {
		mutate(&deps)
	}
	return New(deps, WithClock(func() time.Time { return testNow }))
}

func snapshotOf(t *testing.T, service *Service) Snapshot {
	t.Helper()
	snapshot, err := service.Snapshot(context.Background(), "member@example.com", false)
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	return snapshot
}

// ── tests ───────────────────────────────────────────────────────────────────

func TestSnapshotBuildsProjectCards(t *testing.T) {
	snapshot := snapshotOf(t, newTestService(t, nil))

	if len(snapshot.Projects) != 2 {
		t.Fatalf("projects = %d, want 2", len(snapshot.Projects))
	}
	// Shop's newest chat is a minute old, Blog's is two days old, so Shop
	// leads: the project somebody is working in sorts to the top on its own.
	shop := snapshot.Projects[0]
	if shop.ID != "p1" {
		t.Fatalf("first card = %q, want the most recently active project", shop.ID)
	}
	if shop.LatestChatID != "c2" {
		t.Fatalf("latestChatId = %q, want the newest chat c2", shop.LatestChatID)
	}
	if shop.LastActivityAt != at(-time.Minute) {
		t.Fatalf("lastActivityAt = %d, want %d", shop.LastActivityAt, at(-time.Minute))
	}
	if !shop.Running {
		t.Fatal("running = false, want true while a chat in the project has a turn in flight")
	}
	if shop.PreviewPort != 3000 {
		t.Fatalf("previewPort = %d, want 3000 (platform ports are not previews)", shop.PreviewPort)
	}
	if shop.Health != string(servicehealth.StatusOK) {
		t.Fatalf("health = %q, want %q", shop.Health, servicehealth.StatusOK)
	}
	if blog := snapshot.Projects[1]; blog.Health != string(servicehealth.StatusUnknown) {
		t.Fatalf("unmonitored health = %q, want %q", blog.Health, servicehealth.StatusUnknown)
	}
}

func TestSnapshotKPIsSummarizeTheSectionsBelowThem(t *testing.T) {
	snapshot := snapshotOf(t, newTestService(t, nil))
	kpis := snapshot.KPIs

	if kpis.TotalProjects != 2 || kpis.RunningProjects != 1 {
		t.Fatalf("projects = %d total / %d running, want 2 / 1", kpis.TotalProjects, kpis.RunningProjects)
	}
	if kpis.ActiveRuns != 1 {
		t.Fatalf("activeRuns = %d, want 1", kpis.ActiveRuns)
	}
	if kpis.RunsThisWeek != 12 || kpis.RunsLastWeek != 8 {
		t.Fatalf("runs = %d this week / %d last, want 12 / 8", kpis.RunsThisWeek, kpis.RunsLastWeek)
	}
	if kpis.CostThisWeek != 4 || kpis.CostLastWeek != 2 {
		t.Fatalf("cost = %v this week / %v last, want 4 / 2", kpis.CostThisWeek, kpis.CostLastWeek)
	}
	if kpis.Alerts != len(snapshot.Alerts) {
		t.Fatalf("alerts tile = %d, want the %d rows in the Attention list", kpis.Alerts, len(snapshot.Alerts))
	}
}

// The two halves of the comparison must be the same length and must not
// overlap, or the delta the tile draws is meaningless.
func TestSnapshotAsksForTwoAdjacentEqualWeeks(t *testing.T) {
	usage := &fakeUsage{}
	service := newTestService(t, func(deps *Dependencies) { deps.Usage = usage })
	snapshotOf(t, service)

	if len(usage.queries) != 2 {
		t.Fatalf("usage summaries = %d, want 2 (this week and last)", len(usage.queries))
	}
	current, previous := usage.queries[0], usage.queries[1]
	if previous.To >= current.From {
		t.Fatalf("last week ends at %d and this week starts at %d: the windows overlap",
			previous.To, current.From)
	}
	// Seven whole days, less the one millisecond that keeps a run on the
	// boundary from being counted in both halves.
	wantSpan := int64(WindowDays)*24*60*60*1000 - 1
	if got := previous.To - previous.From; got != wantSpan {
		t.Fatalf("last week spans %d ms, want %d (%d whole UTC days)", got, wantSpan, WindowDays)
	}
	if current.GroupBy != serviceusage.GroupByDay {
		t.Fatalf("groupBy = %q, want %q so the chart gets one bar per day",
			current.GroupBy, serviceusage.GroupByDay)
	}
}

func TestSnapshotRecentActivityPutsRunsInFlightFirst(t *testing.T) {
	snapshot := snapshotOf(t, newTestService(t, nil))

	if len(snapshot.Recent) != 2 {
		t.Fatalf("recent = %d rows, want the running chat plus the ledger record", len(snapshot.Recent))
	}
	live := snapshot.Recent[0]
	if live.Status != RunRunning || live.ChatID != "c2" {
		t.Fatalf("first row = %q/%q, want the running chat c2", live.ChatID, live.Status)
	}
	if live.CostUSD != nil {
		t.Fatal("a run in flight has no ledger record yet, so it must carry no cost")
	}

	done := snapshot.Recent[1]
	if done.Status != RunFinished || done.ChatID != "c1" {
		t.Fatalf("second row = %q/%q, want the finished run in c1", done.ChatID, done.Status)
	}
	if done.ChatTitle != "Checkout" || done.ProjectName != "Shop" {
		t.Fatalf("row = %q in %q, want it enriched with the chat title and project name",
			done.ChatTitle, done.ProjectName)
	}
	if done.StartedAt != done.FinishedAt-60_000 {
		t.Fatalf("startedAt = %d, want finishedAt minus the run duration", done.StartedAt)
	}
	if done.CostUSD == nil || *done.CostUSD != 0.25 {
		t.Fatalf("costUsd = %v, want the ledger's 0.25", done.CostUSD)
	}
}

func TestSnapshotUpcomingListsOnlyArmedTasksSoonestFirst(t *testing.T) {
	service := newTestService(t, func(deps *Dependencies) {
		deps.Schedules = &fakeSchedules{tasks: []serviceschedule.Task{
			{ID: "t-late", Name: "Weekly", ProjectID: "p1", Enabled: true, NextRunAt: at(48 * time.Hour)},
			{ID: "t-soon", Name: "Nightly", ProjectID: "p1", Enabled: true, NextRunAt: at(time.Hour),
				LastRunAt: at(-23 * time.Hour), LastStatus: serviceschedule.RunStatusSucceeded},
			{ID: "t-off", Name: "Disarmed", ProjectID: "p1", Enabled: false, NextRunAt: at(time.Minute)},
			{ID: "t-none", Name: "No deadline", ProjectID: "p1", Enabled: true},
		}}
	})

	snapshot := snapshotOf(t, service)

	if len(snapshot.Upcoming) != 2 {
		t.Fatalf("upcoming = %d, want only the two armed tasks with a deadline", len(snapshot.Upcoming))
	}
	if snapshot.Upcoming[0].ID != "t-soon" || snapshot.Upcoming[1].ID != "t-late" {
		t.Fatalf("upcoming = %q then %q, want soonest first",
			snapshot.Upcoming[0].ID, snapshot.Upcoming[1].ID)
	}
	soon := snapshot.Upcoming[0]
	if soon.ProjectName != "Shop" {
		t.Fatalf("projectName = %q, want it resolved to Shop", soon.ProjectName)
	}
	if soon.LastStatus != string(serviceschedule.RunStatusSucceeded) {
		t.Fatalf("lastRunStatus = %q, want the previous outcome carried through", soon.LastStatus)
	}
}

func TestSnapshotUpcomingIsCapped(t *testing.T) {
	tasks := make([]serviceschedule.Task, 0, UpcomingTasks+3)
	for index := 0; index < UpcomingTasks+3; index++ {
		tasks = append(tasks, serviceschedule.Task{
			ID:        serviceschedule.ID("t" + string(rune('a'+index))),
			Enabled:   true,
			NextRunAt: at(time.Duration(index) * time.Hour),
		})
	}
	service := newTestService(t, func(deps *Dependencies) {
		deps.Schedules = &fakeSchedules{tasks: tasks}
	})

	if got := len(snapshotOf(t, service).Upcoming); got != UpcomingTasks {
		t.Fatalf("upcoming = %d, want it capped at %d", got, UpcomingTasks)
	}
}

func TestSnapshotReadsTheLatestReadySnapshotPerProject(t *testing.T) {
	service := newTestService(t, func(deps *Dependencies) {
		deps.Snapshots = &fakeSnapshots{
			available: true,
			byProject: map[serviceproject.ID][]servicesnapshot.Snapshot{
				"p1": {
					{ID: "s-old", Status: servicesnapshot.StatusReady, CreatedAt: at(-72 * time.Hour)},
					{ID: "s-new", Status: servicesnapshot.StatusReady, CreatedAt: at(-2 * time.Hour)},
					// Still packing: it is not a backup of anything yet.
					{ID: "s-busy", Status: servicesnapshot.StatusRunning, CreatedAt: at(-time.Minute)},
				},
			},
		}
	})

	snapshot := snapshotOf(t, service)

	if got := snapshot.Projects[0].LastSnapshotAt; got != at(-2*time.Hour) {
		t.Fatalf("lastSnapshotAt = %d, want the newest ready archive at %d", got, at(-2*time.Hour))
	}
}

// A subsystem that cannot answer must cost its own card, never the page: a
// box with one sick service is exactly when somebody needs this screen.
// A project whose archive index cannot be read must not be reported as
// "never snapshotted": inventing a finding from a failed read is worse than
// staying quiet about it.
func TestSnapshotDoesNotInventFindingsFromUnreadableArchives(t *testing.T) {
	service := newTestService(t, func(deps *Dependencies) {
		deps.Snapshots = &fakeSnapshots{available: true, failFor: "p1"}
	})

	snapshot := snapshotOf(t, service)

	if got := snapshot.Projects[0].LastSnapshotAt; got != 0 {
		t.Fatalf("lastSnapshotAt = %d, want 0 for a project whose archives could not be read", got)
	}
	for _, alert := range snapshot.Alerts {
		if alert.Kind == KindSnapshot && alert.ProjectID == "p1" {
			t.Fatalf("alert %q was raised from an unreadable archive index", alert.ID)
		}
	}
}

func TestSnapshotDegradesOneSectionAtATime(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*Dependencies)
		check  func(*testing.T, Snapshot)
	}{
		{
			name:   "no usage ledger",
			mutate: func(deps *Dependencies) { deps.Usage = nil },
			check: func(t *testing.T, snapshot Snapshot) {
				if snapshot.Usage.Available {
					t.Fatal("usage.available = true, want false with no ledger wired")
				}
				if snapshot.Usage.Daily == nil {
					t.Fatal("usage.daily = nil, want an empty series the chart can render")
				}
				if snapshot.KPIs.CostThisWeek != 0 {
					t.Fatalf("costThisWeek = %v, want 0", snapshot.KPIs.CostThisWeek)
				}
			},
		},
		{
			name: "the ledger refuses to answer",
			mutate: func(deps *Dependencies) {
				deps.Usage = &fakeUsage{summaryErr: errors.New("ledger unreadable")}
			},
			check: func(t *testing.T, snapshot Snapshot) {
				if snapshot.Usage.Available {
					t.Fatal("usage.available = true, want false when the summary failed")
				}
				if len(snapshot.Projects) != 2 {
					t.Fatal("the project cards must survive a broken ledger")
				}
			},
		},
		{
			name:   "no health monitor",
			mutate: func(deps *Dependencies) { deps.Health = nil },
			check: func(t *testing.T, snapshot Snapshot) {
				if snapshot.Platform.HealthMonitorEnabled {
					t.Fatal("healthMonitorEnabled = true, want false with no monitor")
				}
				if snapshot.Projects[0].Health != string(servicehealth.StatusUnknown) {
					t.Fatal("every project must read as unknown with no monitor")
				}
			},
		},
		{
			name:   "no chat listing",
			mutate: func(deps *Dependencies) { deps.Chats = &fakeChats{err: errors.New("nope")} },
			check: func(t *testing.T, snapshot Snapshot) {
				if len(snapshot.Projects) != 2 {
					t.Fatal("the project cards must survive a broken chat listing")
				}
				if snapshot.Projects[0].LatestChatID != "" {
					t.Fatal("latestChatId must be empty rather than guessed at")
				}
			},
		},
		{
			name:   "no snapshots",
			mutate: func(deps *Dependencies) { deps.Snapshots = nil },
			check: func(t *testing.T, snapshot Snapshot) {
				for _, alert := range snapshot.Alerts {
					if alert.Kind == KindSnapshot {
						t.Fatal("a deployment without snapshots must not be nagged to take one")
					}
				}
			},
		},
		{
			name:   "no container runtime",
			mutate: func(deps *Dependencies) { deps.Capacity = nil },
			check: func(t *testing.T, snapshot Snapshot) {
				if snapshot.Platform.MemoryBudgetBytes != 0 {
					t.Fatal("memoryBudgetBytes must stay zero with no runtime to ask")
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.check(t, snapshotOf(t, newTestService(t, test.mutate)))
		})
	}
}

// The project listing decides what the caller may see. If it fails there is
// no safe partial answer, so the request fails rather than rendering a page
// that might show too much.
func TestSnapshotFailsWhenTheScopeCannotBeResolved(t *testing.T) {
	service := newTestService(t, func(deps *Dependencies) {
		deps.Projects = &fakeProjects{err: errors.New("project store unreadable")}
	})

	if _, err := service.Snapshot(context.Background(), "member@example.com", false); err == nil {
		t.Fatal("Snapshot succeeded, want the error from the project listing")
	}
}

func TestSnapshotForwardsTheCallerToEverySource(t *testing.T) {
	projects := &fakeProjects{}
	chats := &fakeChats{}
	usage := &fakeUsage{}
	service := newTestService(t, func(deps *Dependencies) {
		deps.Projects, deps.Chats, deps.Usage = projects, chats, usage
	})

	if _, err := service.Snapshot(context.Background(), "Member@Example.com", true); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}

	const want = "Member@Example.com"
	if projects.sawEmail != want || !projects.sawAdmin {
		t.Fatalf("projects asked for %q admin=%t, want %q admin=true",
			projects.sawEmail, projects.sawAdmin, want)
	}
	if chats.sawEmail != want {
		t.Fatalf("chats asked for %q, want %q", chats.sawEmail, want)
	}
	if usage.sawEmail != want {
		t.Fatalf("usage asked for %q, want %q", usage.sawEmail, want)
	}
}

// The monitoring report probes LXD behind it, so the aggregation must read it
// once even though both the platform card and the alert rules want it.
func TestSnapshotReadsThePlatformReportOnce(t *testing.T) {
	platform := &fakePlatform{report: servicemonitoring.Report{Status: servicemonitoring.StatusOK}}
	service := newTestService(t, func(deps *Dependencies) { deps.Platform = platform })

	snapshotOf(t, service)

	if platform.calls != 1 {
		t.Fatalf("platform report read %d times, want 1", platform.calls)
	}
}
