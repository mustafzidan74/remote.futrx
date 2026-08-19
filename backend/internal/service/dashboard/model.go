// Package dashboard assembles the home screen's answer to "what is happening
// on this box?" from every service that already knows part of it.
//
// It is a read-only aggregator and owns no store. Its whole reason to exist
// is that the question spans eight subsystems — projects, health, chats, the
// usage ledger, schedules, snapshots, notifications, platform health and host
// capacity — and asking the browser to fan out into eight requests would make
// the landing view the slowest screen in the product. One request in, one
// snapshot out.
//
// Everything here is scoped to the caller: each source is asked with the same
// (email, isAdmin) pair the individual endpoints take, so a member's home
// screen can never name a project, chat, run or task they could not already
// reach on their own.
package dashboard

import (
	servicemonitoring "github.com/futrx-com/remote.futrx.com/internal/service/monitoring"
	serviceusage "github.com/futrx-com/remote.futrx.com/internal/service/usage"
)

// Snapshot is the GET /api/dashboard body: everything the home screen draws,
// as of one instant.
type Snapshot struct {
	GeneratedAt int64 `json:"generatedAt"`
	// WindowDays is how many UTC days the usage series and the week-over-week
	// comparison each cover, so the browser never hard-codes "7".
	WindowDays int `json:"windowDays"`

	KPIs     KPIs      `json:"kpis"`
	Projects []Project `json:"projects"`
	Alerts   []Alert   `json:"alerts"`
	Recent   []Run     `json:"recent"`
	Upcoming []Task    `json:"upcoming"`
	Usage    Usage     `json:"usage"`
	Platform Platform  `json:"platform"`
	// Sites lists the watched client websites that are not currently green.
	// An empty list on a deployment that watches sites is the good news.
	Sites ClientSiteBoard `json:"sites"`
}

// ClientSiteBoard is the home screen's client-site card: whether this
// deployment watches anything at all, and which of the sites the caller may
// see are currently unwell.
type ClientSiteBoard struct {
	// Available is false when no watcher is wired, which renders as "client
	// site monitoring is not set up" rather than as "everything is fine".
	Available bool            `json:"available"`
	Sites     []ClientSiteRow `json:"sites"`
}

// ClientSiteRow is one unwell site on the home screen.
type ClientSiteRow struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	URL    string `json:"url"`
	Status string `json:"status"`
	// Detail is the newest failure reason, already trimmed for a card row.
	Detail string `json:"detail,omitempty"`
	// Since is when the current state began, so the card can say how long it
	// has been down.
	Since         int64  `json:"since,omitempty"`
	LastCheckedAt int64  `json:"lastCheckedAt,omitempty"`
	ProjectID     string `json:"projectId,omitempty"`
}

// KPIs is the top row of tiles. Both halves of every comparison travel
// together so the browser computes the delta instead of the server guessing
// how it will be phrased.
type KPIs struct {
	RunningProjects int `json:"runningProjects"`
	TotalProjects   int `json:"totalProjects"`
	// ActiveRuns counts chats with a turn in flight right now.
	ActiveRuns int `json:"activeRuns"`

	RunsThisWeek   int64 `json:"runsThisWeek"`
	RunsLastWeek   int64 `json:"runsLastWeek"`
	TokensThisWeek int64 `json:"tokensThisWeek"`
	TokensLastWeek int64 `json:"tokensLastWeek"`

	CostThisWeek float64 `json:"costThisWeek"`
	CostLastWeek float64 `json:"costLastWeek"`
	// EstimatedCostThisWeek is the part of CostThisWeek that came from the
	// editable price table rather than from a provider, and
	// UnpricedRunsThisWeek counts runs nothing could price at all. The tile
	// needs both to say "$4.10*" honestly.
	EstimatedCostThisWeek float64 `json:"estimatedCostThisWeek"`
	UnpricedRunsThisWeek  int64   `json:"unpricedRunsThisWeek"`

	Alerts         int `json:"alerts"`
	CriticalAlerts int `json:"criticalAlerts"`
}

// Project is one card in the Projects column.
type Project struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Slug   string `json:"slug"`
	Status string `json:"status"`
	// Health is the monitor's verdict: ok, warn, crit, or unknown when it is
	// not watching this project. The card falls back to Status for its dot.
	Health        string   `json:"health"`
	HealthReasons []string `json:"healthReasons,omitempty"`
	MemoryPct     float64  `json:"memoryPct,omitempty"`
	// PreviewPort is the project's own lowest listening port, or zero when
	// nothing shareable is up. The browser builds the URL from it.
	PreviewPort int `json:"previewPort,omitempty"`
	// LatestChatID is where "open latest chat" goes: the project's most
	// recently active chat, or empty when it has none yet.
	LatestChatID   string `json:"latestChatId,omitempty"`
	LastActivityAt int64  `json:"lastActivityAt,omitempty"`
	// LastSnapshotAt is the newest ready snapshot, zero when there is none.
	LastSnapshotAt int64 `json:"lastSnapshotAt,omitempty"`
	// Running is true while a chat in this project has a turn in flight.
	Running bool `json:"running,omitempty"`
}

// Severity ranks an alert. It is a closed vocabulary: the browser maps it
// straight to a status colour and must never receive a value it cannot paint.
type Severity string

const (
	SeverityInfo Severity = "info"
	SeverityWarn Severity = "warn"
	SeverityCrit Severity = "crit"
)

// rank orders severities most-severe-first for the Attention list.
func (s Severity) rank() int {
	switch s {
	case SeverityCrit:
		return 0
	case SeverityWarn:
		return 1
	default:
		return 2
	}
}

// Kind names what the alert is about, for grouping and for the icon.
type Kind string

const (
	KindHealth        Kind = "health"
	KindAutopilot     Kind = "autopilot"
	KindTrash         Kind = "trash"
	KindSnapshot      Kind = "snapshot"
	KindNotifications Kind = "notifications"
	KindBackup        Kind = "backup"
	KindPlatform      Kind = "platform"
	KindCapacity      Kind = "capacity"
	// KindSiteWatch is a client website that is down or degraded. It is the
	// one alert on this screen about a machine the operator does not own.
	KindSiteWatch Kind = "siteWatch"
)

// Action is the fix the Attention list offers. It is a closed vocabulary so
// the browser wires a button to it rather than parsing the title, and so a
// server that learns a new alert cannot make an old client render a button
// that does nothing.
type Action string

const (
	ActionNone                Action = ""
	ActionOpenProject         Action = "open-project"
	ActionOpenChat            Action = "open-chat"
	ActionSnapshotNow         Action = "snapshot-now"
	ActionRestoreTrash        Action = "restore-trash"
	ActionEnableNotifications Action = "enable-notifications"
	ActionOpenMonitoring      Action = "open-monitoring"
	ActionOpenResources       Action = "open-resources"
	ActionOpenClientSites     Action = "open-client-sites"
)

// Alert is one row in the Attention column: what is wrong, and the one thing
// that fixes it.
type Alert struct {
	// ID is stable for the same finding across refreshes, so the list does
	// not reshuffle under the cursor every sixty seconds.
	ID       string   `json:"id"`
	Severity Severity `json:"severity"`
	Kind     Kind     `json:"kind"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	Action   Action   `json:"action,omitempty"`
	// ActionLabel is the button's text. It travels with the alert because the
	// same action reads differently per alert ("Snapshot now" vs "Restore").
	ActionLabel string `json:"actionLabel,omitempty"`
	ProjectID   string `json:"projectId,omitempty"`
	ChatID      string `json:"chatId,omitempty"`
	// SiteID names the watched client site a siteWatch alert is about.
	SiteID string `json:"siteId,omitempty"`
	// At is the instant the finding is about (deletion, last snapshot, last
	// backup), zero when it is about a setting rather than a moment.
	At int64 `json:"at,omitempty"`
}

// RunStatus is what the Recent activity row shows.
type RunStatus string

const (
	// RunRunning is a turn in flight. It has no ledger record yet, so it
	// carries no cost.
	RunRunning RunStatus = "running"
	// RunFinished is a completed turn read back from the usage ledger. The
	// ledger only records runs that completed, so there is no failed state
	// here — a failed turn simply never appears.
	RunFinished RunStatus = "finished"
)

// Run is one row in Recent activity.
type Run struct {
	// ID is unique per row so the list can key on it.
	ID        string `json:"id"`
	ChatID    string `json:"chatId"`
	ChatTitle string `json:"chatTitle"`
	// ChatSummary is the auxiliary model's one-line description of the chat,
	// shown under the title. Absent on a deployment without that model, which
	// is why the row never depends on it.
	ChatSummary string    `json:"chatSummary,omitempty"`
	ProjectID   string    `json:"projectId,omitempty"`
	ProjectName string    `json:"projectName,omitempty"`
	Provider    string    `json:"provider,omitempty"`
	Model       string    `json:"model,omitempty"`
	Status      RunStatus `json:"status"`
	StartedAt   int64     `json:"startedAt,omitempty"`
	FinishedAt  int64     `json:"finishedAt,omitempty"`
	// CostUSD is absent when the run's cost is unknown. Never read an absent
	// cost as zero.
	CostUSD     *float64 `json:"costUsd,omitempty"`
	Estimated   bool     `json:"estimated,omitempty"`
	TotalTokens int64    `json:"totalTokens,omitempty"`
	Scheduled   bool     `json:"scheduled,omitempty"`
}

// Task is one row in Upcoming: a scheduled task and how its last run went.
type Task struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ProjectID   string `json:"projectId,omitempty"`
	ProjectName string `json:"projectName,omitempty"`
	ChatID      string `json:"chatId,omitempty"`
	Kind        string `json:"kind"`
	Cron        string `json:"cron,omitempty"`
	Timezone    string `json:"timezone,omitempty"`
	NextRunAt   int64  `json:"nextRunAt"`
	LastRunAt   int64  `json:"lastRunAt,omitempty"`
	LastStatus  string `json:"lastRunStatus,omitempty"`
	LastError   string `json:"lastError,omitempty"`
}

// Usage is the 7-day series plus the two totals the delta is computed from.
type Usage struct {
	// Available is false when this deployment has no usage ledger, which the
	// card renders as "usage is not being recorded" rather than as zero cost.
	Available bool                    `json:"available"`
	Daily     []serviceusage.DayPoint `json:"daily"`
	ThisWeek  serviceusage.Totals     `json:"thisWeek"`
	LastWeek  serviceusage.Totals     `json:"lastWeek"`
}

// Platform is the box's own state: the same checks /healthz answers with,
// plus the memory budget the fleet is committed against.
type Platform struct {
	Status  string                   `json:"status"`
	Version string                   `json:"version,omitempty"`
	Checks  servicemonitoring.Checks `json:"checks"`
	Details []string                 `json:"details,omitempty"`

	MemoryBudgetBytes    uint64 `json:"memoryBudgetBytes,omitempty"`
	MemoryCommittedBytes uint64 `json:"memoryCommittedBytes,omitempty"`
	RunningContainers    int    `json:"runningContainers"`
	MaxRunningContainers int    `json:"maxRunningContainers,omitempty"`

	// HealthMonitorEnabled says whether project health dots mean anything.
	HealthMonitorEnabled bool `json:"healthMonitorEnabled"`
	NotificationsEnabled bool `json:"notificationsEnabled"`
	// BackupReadable is false when this host has no backup marker directory,
	// which is a fact about the deployment and never an alert.
	BackupReadable bool  `json:"backupReadable"`
	BackupAt       int64 `json:"backupAt,omitempty"`
}
