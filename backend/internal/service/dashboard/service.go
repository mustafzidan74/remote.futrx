package dashboard

import (
	"context"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	servicehealth "github.com/futrx-com/remote.futrx.com/internal/service/health"
	servicemonitoring "github.com/futrx-com/remote.futrx.com/internal/service/monitoring"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	serviceschedule "github.com/futrx-com/remote.futrx.com/internal/service/schedule"
	servicesitewatch "github.com/futrx-com/remote.futrx.com/internal/service/sitewatch"
	servicesnapshot "github.com/futrx-com/remote.futrx.com/internal/service/snapshot"
	serviceusage "github.com/futrx-com/remote.futrx.com/internal/service/usage"
)

const (
	// WindowDays is the length of both halves of the week-over-week
	// comparison and of the usage chart. Seven whole UTC days, today
	// included, so the series is always exactly seven bars wide.
	WindowDays = 7

	// RecentRuns is how many rows the Recent activity list holds.
	RecentRuns = 10

	// UpcomingTasks is how many scheduled tasks the Upcoming list holds.
	UpcomingTasks = 5

	// recentLedgerWindow is how far back the ledger is read for the Recent
	// activity list. It is generous rather than exact: a quiet box should
	// still show its last few runs instead of an empty card.
	recentLedgerWindow = 30 * 24 * time.Hour
)

// Dependencies are the services the snapshot is assembled from. Every one
// except Projects is optional: a deployment missing a subsystem gets a home
// screen with that section marked unavailable rather than no home screen.
type Dependencies struct {
	Projects      Projects
	Chats         Chats
	Usage         UsageLedger
	Health        Health
	Schedules     Schedules
	Snapshots     Snapshots
	Notifications Notifications
	Platform      PlatformHealth
	Capacity      Capacity
	Backups       Backups
	ClientSites   ClientSites

	// TrashRetention is how long a soft-deleted project survives. It is the
	// same duration the Trash page reports expiries with, passed in because
	// only the composition root knows the configured value.
	TrashRetention time.Duration
}

// Option customizes the service.
type Option func(*Service)

// WithClock replaces the wall clock, so tests can pin "now" and assert on
// every age-based rule without sleeping.
func WithClock(now func() time.Time) Option {
	return func(s *Service) {
		if now != nil {
			s.now = now
		}
	}
}

// Service assembles the home dashboard. It holds no state of its own.
type Service struct {
	deps Dependencies
	now  func() time.Time
}

func New(deps Dependencies, options ...Option) *Service {
	service := &Service{deps: deps, now: time.Now}
	for _, option := range options {
		option(service)
	}
	return service
}

// Snapshot assembles the whole home screen for one caller.
//
// Only the project listing is allowed to fail the request: it decides what
// the caller may see, so a broken scope must not be answered with a partial
// page that might show too much. Every other source degrades on its own — a
// usage ledger that cannot be read leaves the usage card unavailable, not the
// dashboard blank — because a box with one sick subsystem is exactly when
// somebody most needs to look at this screen.
func (s *Service) Snapshot(
	ctx context.Context,
	callerEmail string,
	isAdmin bool,
) (Snapshot, error) {
	now := s.now()
	metas, err := s.deps.Projects.ListVisible(ctx, callerEmail, isAdmin)
	if err != nil {
		return Snapshot{}, err
	}

	chats := s.chats(ctx, callerEmail, isAdmin)
	health := s.health(metas)
	lastSnapshots, snapshotsKnown := s.lastSnapshots(ctx, metas)
	projects := buildProjects(metas, chats, health, lastSnapshots)

	thisWeek, lastWeek := weekWindows(now)
	usage := s.usage(ctx, callerEmail, isAdmin, thisWeek, lastWeek)
	// The platform report is read once and reused: it backs both the platform
	// card and the "the box is degraded" alert, and the monitoring service
	// probes LXD behind it.
	report := s.platformReport(ctx)
	platform := s.platform(ctx, report, health != nil)

	sites := s.clientSites(ctx, callerEmail, isAdmin)

	snapshot := Snapshot{
		GeneratedAt: now.UnixMilli(),
		WindowDays:  WindowDays,
		Projects:    projects,
		Sites:       sites,
		Recent:      s.recent(ctx, callerEmail, isAdmin, now, projects, chats),
		Upcoming:    s.upcoming(ctx, callerEmail, isAdmin, now, projects),
		Usage:       usage,
		Platform:    platform,
	}
	snapshot.Alerts = DeriveAlerts(AlertInput{
		Now:                     now,
		Projects:                projects,
		Sites:                   sites.Sites,
		Chats:                   chatStates(chats),
		Trash:                   s.trash(ctx, callerEmail, isAdmin),
		SnapshotsAvailable:      s.deps.Snapshots != nil && s.deps.Snapshots.Available(),
		SnapshotsKnown:          snapshotsKnown,
		NotificationsEnabled:    platform.NotificationsEnabled,
		NotificationsConfigured: s.notificationsConfigured(),
		Backup:                  BackupState{Readable: platform.BackupReadable, LastAt: platform.BackupAt},
		Platform:                report,
		Capacity: CapacityState{
			BudgetBytes:          platform.MemoryBudgetBytes,
			CommittedBytes:       platform.MemoryCommittedBytes,
			RunningContainers:    platform.RunningContainers,
			MaxRunningContainers: platform.MaxRunningContainers,
		},
	})
	snapshot.KPIs = buildKPIs(projects, snapshot.Alerts, usage)
	return snapshot, nil
}

// chats reads the caller's chats, tolerating a failure: the sidebar is the
// authority on chats, and losing them here costs the dashboard a "last
// activity" line rather than the whole page.
func (s *Service) chats(ctx context.Context, callerEmail string, isAdmin bool) []chatRow {
	if s.deps.Chats == nil {
		return nil
	}
	metas, err := s.deps.Chats.List(ctx, callerEmail, isAdmin)
	if err != nil {
		log.Printf("dashboard: list chats: %v", err)
		return nil
	}
	rows := make([]chatRow, 0, len(metas))
	for _, meta := range metas {
		rows = append(rows, chatRow{
			ID:             string(meta.ID),
			Title:          meta.Title,
			Summary:        meta.Summary,
			ProjectID:      string(meta.ProjectID),
			Provider:       string(meta.Provider),
			Model:          meta.Model,
			Running:        meta.Running,
			LastMessageAt:  meta.LastMessageAt,
			Autopilot:      meta.Autopilot.Enabled,
			RoundsUsed:     meta.Autopilot.RoundsUsed,
			MaxRounds:      meta.Autopilot.MaxRounds,
			AutopilotSince: meta.Autopilot.StartedAt,
		})
	}
	return rows
}

// clientSites reads the site watcher for the caller's unwell sites. A watcher
// that cannot answer costs the home screen one card, never the page: the
// sites are on somebody else's servers, and this box's own report about them
// failing is the least important thing on the screen.
func (s *Service) clientSites(ctx context.Context, callerEmail string, isAdmin bool) ClientSiteBoard {
	if s.deps.ClientSites == nil || !s.deps.ClientSites.Available() {
		return ClientSiteBoard{Sites: []ClientSiteRow{}}
	}
	board := ClientSiteBoard{Available: true, Sites: []ClientSiteRow{}}
	views, err := s.deps.ClientSites.NotGreen(ctx, callerEmail, isAdmin)
	if err != nil {
		log.Printf("dashboard: list client sites: %v", err)
		return board
	}
	for _, view := range views {
		board.Sites = append(board.Sites, ClientSiteRow{
			ID:            string(view.ID),
			Label:         view.Name(),
			URL:           view.URL,
			Status:        string(view.Status),
			Detail:        clientSiteDetail(view),
			Since:         view.ChangedAt,
			LastCheckedAt: view.LastCheckedAt,
			ProjectID:     view.ProjectID,
		})
	}
	return board
}

// clientSiteDetail is the one line the card shows under a site's name: what
// the last check found, trimmed to a card row.
func clientSiteDetail(view servicesitewatch.View) string {
	detail := strings.TrimSpace(view.LastError)
	if detail == "" && view.LastCode > 0 {
		detail = "HTTP " + strconv.Itoa(view.LastCode)
	}
	const limit = 160
	runes := []rune(detail)
	if len(runes) <= limit {
		return detail
	}
	return string(runes[:limit]) + "…"
}

// health reads the monitor's cache for the visible projects. A nil map means
// there is no monitor at all, which the platform card reports separately from
// "every project happens to be unknown".
func (s *Service) health(metas []serviceproject.Meta) map[string]servicehealth.ProjectHealth {
	if s.deps.Health == nil {
		return nil
	}
	ids := make([]serviceproject.ID, 0, len(metas))
	for _, meta := range metas {
		ids = append(ids, meta.ID)
	}
	rows := s.deps.Health.Snapshot(ids)
	out := make(map[string]servicehealth.ProjectHealth, len(rows))
	for _, row := range rows {
		out[row.ProjectID] = row
	}
	return out
}

// lastSnapshots reads the newest ready archive per project. The second return
// records which projects were actually answered for, so a project whose
// archives could not be read is never reported as "never snapshotted".
func (s *Service) lastSnapshots(
	ctx context.Context,
	metas []serviceproject.Meta,
) (map[string]int64, map[string]bool) {
	if s.deps.Snapshots == nil || !s.deps.Snapshots.Available() {
		return nil, nil
	}
	latest := make(map[string]int64, len(metas))
	known := make(map[string]bool, len(metas))
	for _, meta := range metas {
		records, err := s.deps.Snapshots.List(ctx, meta.ID)
		if err != nil {
			log.Printf("dashboard: list snapshots for %s: %v", meta.ID, err)
			continue
		}
		known[string(meta.ID)] = true
		for _, record := range records {
			if record.Status != servicesnapshot.StatusReady {
				continue
			}
			if record.CreatedAt > latest[string(meta.ID)] {
				latest[string(meta.ID)] = record.CreatedAt
			}
		}
	}
	return latest, known
}

// trash lists the caller's trashed projects with their computed purge
// instants, which is the same arithmetic the Trash page shows.
func (s *Service) trash(ctx context.Context, callerEmail string, isAdmin bool) []TrashedProject {
	metas, err := s.deps.Projects.ListTrashed(ctx, callerEmail, isAdmin)
	if err != nil {
		log.Printf("dashboard: list trashed projects: %v", err)
		return nil
	}
	out := make([]TrashedProject, 0, len(metas))
	for _, meta := range metas {
		out = append(out, TrashedProject{
			ID:        string(meta.ID),
			Name:      meta.Name,
			ExpiresAt: meta.TrashExpiresAt(s.deps.TrashRetention),
		})
	}
	return out
}

// usage reads both halves of the week-over-week comparison. The current week
// is asked for grouped by day so its Daily series is the chart, and the prior
// week only needs its totals.
func (s *Service) usage(
	ctx context.Context,
	callerEmail string,
	isAdmin bool,
	thisWeek, lastWeek window,
) Usage {
	if s.deps.Usage == nil {
		return Usage{Daily: []serviceusage.DayPoint{}}
	}
	current, err := s.deps.Usage.Summary(ctx, serviceusage.Query{
		From:    thisWeek.from,
		To:      thisWeek.to,
		GroupBy: serviceusage.GroupByDay,
	}, callerEmail, isAdmin)
	if err != nil {
		log.Printf("dashboard: usage summary: %v", err)
		return Usage{Daily: []serviceusage.DayPoint{}}
	}
	previous, err := s.deps.Usage.Summary(ctx, serviceusage.Query{
		From:    lastWeek.from,
		To:      lastWeek.to,
		GroupBy: serviceusage.GroupByDay,
	}, callerEmail, isAdmin)
	if err != nil {
		log.Printf("dashboard: previous usage summary: %v", err)
	}
	daily := current.Daily
	if daily == nil {
		daily = []serviceusage.DayPoint{}
	}
	return Usage{
		Available: true,
		Daily:     daily,
		ThisWeek:  current.Totals,
		LastWeek:  previous.Totals,
	}
}

// recent builds the activity list: turns in flight first — they are the only
// rows that can still change — then the newest completed runs from the
// ledger. The ledger records completed runs only, which is why a failed turn
// never appears here.
func (s *Service) recent(
	ctx context.Context,
	callerEmail string,
	isAdmin bool,
	now time.Time,
	projects []Project,
	chats []chatRow,
) []Run {
	names := projectNames(projects)
	titles := chatTitles(chats)
	summaries := chatSummaries(chats)
	runs := make([]Run, 0, RecentRuns)

	live := make([]chatRow, 0, 4)
	for _, chat := range chats {
		if chat.Running {
			live = append(live, chat)
		}
	}
	sort.SliceStable(live, func(i, j int) bool {
		return live[i].LastMessageAt > live[j].LastMessageAt
	})
	for _, chat := range live {
		runs = append(runs, Run{
			ID:          "live:" + chat.ID,
			ChatID:      chat.ID,
			ChatTitle:   chat.Title,
			ChatSummary: chat.Summary,
			ProjectID:   chat.ProjectID,
			ProjectName: names[chat.ProjectID],
			Provider:    chat.Provider,
			Model:       chat.Model,
			Status:      RunRunning,
			StartedAt:   chat.LastMessageAt,
		})
	}

	if s.deps.Usage != nil && len(runs) < RecentRuns {
		page, err := s.deps.Usage.Records(ctx, serviceusage.RecordQuery{
			From:  now.Add(-recentLedgerWindow).UnixMilli(),
			To:    now.UnixMilli(),
			Limit: RecentRuns,
		}, callerEmail, isAdmin)
		if err != nil {
			log.Printf("dashboard: usage records: %v", err)
		}
		for _, record := range page.Records {
			if len(runs) >= RecentRuns {
				break
			}
			runs = append(runs, ledgerRun(record, names, titles, summaries))
		}
	}
	if len(runs) > RecentRuns {
		runs = runs[:RecentRuns]
	}
	return runs
}

// ledgerRun renders one completed ledger record as an activity row.
func ledgerRun(record serviceusage.Record, names, titles, summaries map[string]string) Run {
	started := int64(0)
	if record.DurationMs > 0 {
		started = record.At - record.DurationMs
	}
	title := titles[record.ChatID]
	if title == "" {
		// The chat was deleted after its run was recorded. The row still
		// belongs on the list — the money was spent — so it is labelled
		// rather than dropped.
		title = "Deleted chat"
	}
	return Run{
		ID:          runID(record),
		ChatID:      record.ChatID,
		ChatTitle:   title,
		ChatSummary: summaries[record.ChatID],
		ProjectID:   record.ProjectID,
		ProjectName: names[record.ProjectID],
		Provider:    record.Provider,
		Model:       record.Model,
		Status:      RunFinished,
		StartedAt:   started,
		FinishedAt:  record.At,
		CostUSD:     record.CostUSD,
		Estimated:   record.Estimated,
		TotalTokens: record.TotalTokens(),
		Scheduled:   record.Scheduled,
	}
}

// runID keys one activity row. The ledger's own run id is used when it has
// one; older records predate it and fall back to chat plus instant, which is
// unique in practice because one chat runs one turn at a time.
func runID(record serviceusage.Record) string {
	if strings.TrimSpace(record.RunID) != "" {
		return "run:" + record.RunID
	}
	return "run:" + record.ChatID + ":" + strconv.FormatInt(record.At, 10)
}

// upcoming lists the next scheduled tasks with their last outcome. Only armed
// tasks with a deadline are shown: a disabled task is not going to happen,
// and putting it in a countdown list would be a lie.
func (s *Service) upcoming(
	ctx context.Context,
	callerEmail string,
	isAdmin bool,
	now time.Time,
	projects []Project,
) []Task {
	if s.deps.Schedules == nil {
		return []Task{}
	}
	tasks, err := s.deps.Schedules.List(ctx, callerEmail, isAdmin)
	if err != nil {
		log.Printf("dashboard: list scheduled tasks: %v", err)
		return []Task{}
	}
	names := projectNames(projects)
	due := make([]serviceschedule.Task, 0, len(tasks))
	for _, task := range tasks {
		if !task.Enabled || task.NextRunAt <= 0 {
			continue
		}
		due = append(due, task)
	}
	sort.SliceStable(due, func(i, j int) bool { return due[i].NextRunAt < due[j].NextRunAt })
	if len(due) > UpcomingTasks {
		due = due[:UpcomingTasks]
	}
	out := make([]Task, 0, len(due))
	for _, task := range due {
		out = append(out, Task{
			ID:          string(task.ID),
			Name:        task.Name,
			ProjectID:   string(task.ProjectID),
			ProjectName: names[string(task.ProjectID)],
			ChatID:      string(task.ChatID),
			Kind:        string(task.Kind),
			Cron:        task.Cron,
			Timezone:    task.Timezone,
			NextRunAt:   task.NextRunAt,
			LastRunAt:   task.LastRunAt,
			LastStatus:  string(task.LastStatus),
			LastError:   task.LastError,
		})
	}
	return out
}

// platform assembles the box's own card: the /healthz checks, the fleet
// memory budget, and the two switches (notifications, backups) whose "off"
// state is itself worth reporting.
func (s *Service) platform(
	ctx context.Context,
	report servicemonitoring.Report,
	healthMonitorWired bool,
) Platform {
	out := Platform{
		Status:               string(servicemonitoring.StatusSkipped),
		HealthMonitorEnabled: healthMonitorWired && s.deps.Health.Enabled(),
	}
	if report.Status != "" {
		out.Status = string(report.Status)
		out.Version = report.Version
		out.Checks = report.Checks
		out.Details = report.Details
	}
	if s.deps.Capacity != nil {
		view := s.deps.Capacity.Get(ctx)
		out.MemoryBudgetBytes = view.Host.BudgetMemoryBytes
		out.MemoryCommittedBytes = view.Host.CommittedBytes
		out.RunningContainers = view.Host.RunningContainers
		out.MaxRunningContainers = view.Settings.MaxRunningContainers
	}
	if s.deps.Notifications != nil {
		config := s.deps.Notifications.Config()
		out.NotificationsEnabled = config.Enabled
	}
	if s.deps.Backups != nil {
		backup := s.deps.Backups.Backup(ctx)
		out.BackupReadable = backup.Readable
		out.BackupAt = backup.LastAt
	}
	return out
}

// platformReport asks the monitoring service for the same body /healthz
// serves. proxied is false because this call did not arrive through Caddy —
// it is an internal read — so the edge check is probed rather than assumed.
func (s *Service) platformReport(ctx context.Context) servicemonitoring.Report {
	if s.deps.Platform == nil {
		return servicemonitoring.Report{}
	}
	return s.deps.Platform.Report(ctx, false)
}

// notificationsConfigured reports whether any sink is connected at all, which
// is what decides between "turn them on" and "set them up".
func (s *Service) notificationsConfigured() bool {
	if s.deps.Notifications == nil {
		return false
	}
	config := s.deps.Notifications.Config()
	return config.TelegramConfigured() || config.WebhookConfigured() || config.WhatsAppConfigured()
}

// chatRow is the internal view of a chat: everything the cards, the activity
// list and the alert rules read, resolved once.
type chatRow struct {
	ID    string
	Title string
	// Summary is the auxiliary model's one-line description of the chat, or
	// empty on a deployment that has none. It is what turns an activity row
	// from "Untitled chat" into something recognizable a week later.
	Summary        string
	ProjectID      string
	Provider       string
	Model          string
	Running        bool
	LastMessageAt  int64
	Autopilot      bool
	RoundsUsed     int
	MaxRounds      int
	AutopilotSince int64
}

// buildProjects turns the raw listings into the cards the Projects column
// draws, newest activity first so the project somebody is working in is at
// the top without them sorting anything.
func buildProjects(
	metas []serviceproject.Meta,
	chats []chatRow,
	health map[string]servicehealth.ProjectHealth,
	lastSnapshots map[string]int64,
) []Project {
	latestChat := map[string]chatRow{}
	running := map[string]bool{}
	for _, chat := range chats {
		if chat.ProjectID == "" {
			continue
		}
		if chat.Running {
			running[chat.ProjectID] = true
		}
		if current, ok := latestChat[chat.ProjectID]; !ok || chat.LastMessageAt > current.LastMessageAt {
			latestChat[chat.ProjectID] = chat
		}
	}

	out := make([]Project, 0, len(metas))
	for _, meta := range metas {
		id := string(meta.ID)
		card := Project{
			ID:             id,
			Name:           meta.Name,
			Slug:           meta.Slug,
			Status:         string(meta.Status),
			Health:         string(servicehealth.StatusUnknown),
			LastSnapshotAt: lastSnapshots[id],
			Running:        running[id],
		}
		if row, ok := health[id]; ok {
			card.Health = string(row.Status)
			card.HealthReasons = row.Reasons
			card.MemoryPct = row.MemoryPct
			if port, ok := servicehealth.FirstAppPort(row.Listeners); ok {
				card.PreviewPort = port
			}
		}
		if chat, ok := latestChat[id]; ok {
			card.LatestChatID = chat.ID
			card.LastActivityAt = chat.LastMessageAt
		}
		out = append(out, card)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].LastActivityAt > out[j].LastActivityAt
	})
	return out
}

// buildKPIs is the top row. Every number on it is read back off the sections
// below it, so a tile can never disagree with the card it summarizes.
func buildKPIs(projects []Project, alerts []Alert, usage Usage) KPIs {
	kpis := KPIs{TotalProjects: len(projects)}
	for _, project := range projects {
		if project.Status == string(serviceproject.StatusRunning) {
			kpis.RunningProjects++
		}
		if project.Running {
			kpis.ActiveRuns++
		}
	}
	for _, alert := range alerts {
		kpis.Alerts++
		if alert.Severity == SeverityCrit {
			kpis.CriticalAlerts++
		}
	}
	kpis.RunsThisWeek = usage.ThisWeek.Runs
	kpis.RunsLastWeek = usage.LastWeek.Runs
	kpis.TokensThisWeek = usage.ThisWeek.TotalTokens
	kpis.TokensLastWeek = usage.LastWeek.TotalTokens
	kpis.CostThisWeek = usage.ThisWeek.CostUSD
	kpis.CostLastWeek = usage.LastWeek.CostUSD
	kpis.EstimatedCostThisWeek = usage.ThisWeek.EstimatedCostUSD
	kpis.UnpricedRunsThisWeek = usage.ThisWeek.UnpricedRuns
	return kpis
}

// chatStates narrows the internal chat view to what the alert rules read.
func chatStates(chats []chatRow) []ChatState {
	out := make([]ChatState, 0, len(chats))
	for _, chat := range chats {
		out = append(out, ChatState{
			ID:             chat.ID,
			Title:          chat.Title,
			ProjectID:      chat.ProjectID,
			Running:        chat.Running,
			Autopilot:      chat.Autopilot,
			RoundsUsed:     chat.RoundsUsed,
			MaxRounds:      chat.MaxRounds,
			AutopilotSince: chat.AutopilotSince,
		})
	}
	return out
}

func projectNames(projects []Project) map[string]string {
	names := make(map[string]string, len(projects))
	for _, project := range projects {
		names[project.ID] = project.Name
	}
	return names
}

func chatTitles(chats []chatRow) map[string]string {
	titles := make(map[string]string, len(chats))
	for _, chat := range chats {
		titles[chat.ID] = chat.Title
	}
	return titles
}

// chatSummaries indexes the auxiliary model's one-liners by chat, so a
// completed run read back from the ledger can carry the same subtitle the
// sidebar shows. A chat with no summary — every chat, on a deployment without
// the auxiliary model — is simply absent from the map.
func chatSummaries(chats []chatRow) map[string]string {
	summaries := make(map[string]string, len(chats))
	for _, chat := range chats {
		if chat.Summary != "" {
			summaries[chat.ID] = chat.Summary
		}
	}
	return summaries
}

// window is one inclusive [from, to] usage range in unix milliseconds.
type window struct {
	from int64
	to   int64
}

// weekWindows splits the last two weeks into "this week" and "last week",
// both aligned to UTC midnight so the seven daily buckets are whole days and
// the two halves are the same length. The comparison is therefore
// "the last seven days against the seven before them", not "since Monday",
// which would make the delta meaningless every Monday morning.
func weekWindows(now time.Time) (window, window) {
	dayStart := now.UTC().Truncate(24 * time.Hour)
	thisFrom := dayStart.AddDate(0, 0, -(WindowDays - 1))
	lastFrom := thisFrom.AddDate(0, 0, -WindowDays)
	return window{
			from: thisFrom.UnixMilli(),
			to:   now.UnixMilli(),
		}, window{
			from: lastFrom.UnixMilli(),
			// One millisecond short of this week's first instant, because the
			// ledger scan treats both bounds as inclusive and a shared
			// boundary would count a run in both halves.
			to: thisFrom.UnixMilli() - 1,
		}
}
