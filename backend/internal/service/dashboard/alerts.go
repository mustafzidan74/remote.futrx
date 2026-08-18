package dashboard

import (
	"fmt"
	"sort"
	"strings"
	"time"

	servicemonitoring "github.com/futrx-com/remote.futrx.com/internal/service/monitoring"
)

// Thresholds behind the Attention list. They are named constants rather than
// literals because each one is a judgement call somebody will want to argue
// with, and because the tests assert against them by name instead of
// re-typing the number.
const (
	// TrashExpiryWarning is how close a trashed project must be to its purge
	// before the dashboard says so. Two days covers a Friday delete noticed
	// on Monday, which is the case the Trash exists for.
	TrashExpiryWarning = 48 * time.Hour

	// SnapshotStale is how long a running project may go without an archive
	// before the dashboard offers to take one. A stopped project is not
	// changing, so it is never nagged about.
	SnapshotStale = 7 * 24 * time.Hour

	// BackupStale is how long the host may go without a `remote-backup` run
	// before that is worth saying out loud. The timer is nightly, so two
	// days means it has missed at least one.
	BackupStale = 48 * time.Hour

	// CapacityWarnRatio is the share of the memory budget that, once
	// committed, means the next project start is likely to be refused.
	CapacityWarnRatio = 0.9
)

// ChatState is the slice of a chat the alert rules read.
type ChatState struct {
	ID        string
	Title     string
	ProjectID string
	Running   bool
	// Autopilot is on when the chat will keep prompting itself after a turn
	// settles. A running autopilot chat spends money unattended, which is
	// the whole reason it is surfaced here.
	Autopilot      bool
	RoundsUsed     int
	MaxRounds      int
	AutopilotSince int64
}

// TrashedProject is a project awaiting purge.
type TrashedProject struct {
	ID   string
	Name string
	// ExpiresAt is the unix-ms purge instant, zero when retention is off —
	// which means "kept until an admin purges it" and never expires.
	ExpiresAt int64
}

// BackupState is the host backup marker as the rules see it.
type BackupState struct {
	Readable bool
	LastAt   int64
}

// CapacityState is the fleet memory budget as the rules see it.
type CapacityState struct {
	BudgetBytes          uint64
	CommittedBytes       uint64
	RunningContainers    int
	MaxRunningContainers int
}

// AlertInput is everything the rules read. It is a plain struct of resolved
// values so the whole Attention column can be decided — and tested — without
// a single live service behind it.
type AlertInput struct {
	Now      time.Time
	Projects []Project
	Chats    []ChatState
	Trash    []TrashedProject

	SnapshotsAvailable bool
	// SnapshotsKnown lists the project ids whose archives were actually read.
	// A project missing from it is not "never snapshotted", it is "not
	// checked", and silence beats a false alarm.
	SnapshotsKnown map[string]bool

	NotificationsEnabled    bool
	NotificationsConfigured bool

	Backup   BackupState
	Platform servicemonitoring.Report
	Capacity CapacityState
}

// DeriveAlerts is the whole Attention column: every finding worth a human's
// time, each with the one action that resolves it, most severe first.
//
// The ordering is stable within a severity and follows the emission order
// below — platform, then per-project findings, then settings — so a list
// refreshed every sixty seconds does not reshuffle under the cursor.
func DeriveAlerts(in AlertInput) []Alert {
	alerts := make([]Alert, 0, 8)
	alerts = append(alerts, platformAlerts(in)...)
	alerts = append(alerts, healthAlerts(in)...)
	alerts = append(alerts, trashAlerts(in)...)
	alerts = append(alerts, snapshotAlerts(in)...)
	alerts = append(alerts, autopilotAlerts(in)...)
	alerts = append(alerts, capacityAlerts(in)...)
	alerts = append(alerts, notificationAlert(in)...)
	alerts = append(alerts, backupAlert(in)...)

	sort.SliceStable(alerts, func(i, j int) bool {
		return alerts[i].Severity.rank() < alerts[j].Severity.rank()
	})
	return alerts
}

// platformAlerts reports the box's own failures. A degraded store or LXD is
// the most serious thing this screen can say, because every other number on
// it was read through them.
func platformAlerts(in AlertInput) []Alert {
	report := in.Platform
	if report.Status == "" || report.Status == servicemonitoring.StatusOK {
		return nil
	}
	detail := strings.Join(report.Details, " · ")
	return []Alert{{
		ID:          "platform",
		Severity:    SeverityCrit,
		Kind:        KindPlatform,
		Title:       "The platform reports itself degraded",
		Detail:      detail,
		Action:      ActionOpenMonitoring,
		ActionLabel: "Open monitoring",
	}}
}

// healthAlerts turns the monitor's warn and crit verdicts into rows. Only
// projects the monitor actually has an opinion about are reported: an
// "unknown" project is one it is not watching, not one that is failing.
func healthAlerts(in AlertInput) []Alert {
	out := make([]Alert, 0, len(in.Projects))
	for _, project := range in.Projects {
		severity := SeverityWarn
		switch project.Health {
		case "crit":
			severity = SeverityCrit
		case "warn":
		default:
			continue
		}
		title := project.Name + " is degraded"
		if severity == SeverityCrit {
			title = project.Name + " is in a critical state"
		}
		out = append(out, Alert{
			ID:          "health:" + project.ID,
			Severity:    severity,
			Kind:        KindHealth,
			Title:       title,
			Detail:      strings.Join(project.HealthReasons, " · "),
			Action:      ActionOpenProject,
			ActionLabel: "Open project",
			ProjectID:   project.ID,
		})
	}
	return out
}

// trashAlerts warns about a deletion about to become permanent. Retention
// disabled (ExpiresAt zero) means the project is kept until somebody purges
// it by hand, so there is no deadline to warn about.
func trashAlerts(in AlertInput) []Alert {
	out := make([]Alert, 0, len(in.Trash))
	nowMilli := in.Now.UnixMilli()
	for _, project := range in.Trash {
		if project.ExpiresAt <= 0 {
			continue
		}
		remaining := time.Duration(project.ExpiresAt-nowMilli) * time.Millisecond
		if remaining > TrashExpiryWarning {
			continue
		}
		detail := "Purged " + humanizeDeadline(remaining) + ". Restoring brings back its files and database."
		out = append(out, Alert{
			ID:          "trash:" + project.ID,
			Severity:    SeverityWarn,
			Kind:        KindTrash,
			Title:       project.Name + " leaves the Trash soon",
			Detail:      detail,
			Action:      ActionRestoreTrash,
			ActionLabel: "Restore from trash",
			ProjectID:   project.ID,
			At:          project.ExpiresAt,
		})
	}
	return out
}

// snapshotAlerts nags about running projects whose files have not been
// archived lately. A stopped project is not changing, so it is left alone,
// and a project whose archives could not be read is not guessed about.
func snapshotAlerts(in AlertInput) []Alert {
	if !in.SnapshotsAvailable {
		return nil
	}
	out := make([]Alert, 0, len(in.Projects))
	nowMilli := in.Now.UnixMilli()
	for _, project := range in.Projects {
		if project.Status != "running" {
			continue
		}
		if in.SnapshotsKnown != nil && !in.SnapshotsKnown[project.ID] {
			continue
		}
		if project.LastSnapshotAt > 0 {
			age := time.Duration(nowMilli-project.LastSnapshotAt) * time.Millisecond
			if age < SnapshotStale {
				continue
			}
			out = append(out, Alert{
				ID:          "snapshot:" + project.ID,
				Severity:    SeverityWarn,
				Kind:        KindSnapshot,
				Title:       project.Name + " has not been snapshotted lately",
				Detail:      "Last archive " + humanizeAge(age) + " ago.",
				Action:      ActionSnapshotNow,
				ActionLabel: "Snapshot now",
				ProjectID:   project.ID,
				At:          project.LastSnapshotAt,
			})
			continue
		}
		out = append(out, Alert{
			ID:          "snapshot:" + project.ID,
			Severity:    SeverityWarn,
			Kind:        KindSnapshot,
			Title:       project.Name + " has never been snapshotted",
			Detail:      "A snapshot archives the workspace and its database so a bad turn can be undone.",
			Action:      ActionSnapshotNow,
			ActionLabel: "Snapshot now",
			ProjectID:   project.ID,
		})
	}
	return out
}

// autopilotAlerts surfaces loops spending money unattended. This is
// informational rather than a fault: an armed autopilot is something somebody
// asked for, and the point of the row is that they can see it is still going.
func autopilotAlerts(in AlertInput) []Alert {
	out := make([]Alert, 0, 2)
	for _, chat := range in.Chats {
		if !chat.Autopilot || !chat.Running {
			continue
		}
		detail := "The agent keeps prompting itself until it declares the task done."
		if chat.MaxRounds > 0 {
			detail = fmt.Sprintf("Round %d of %d.", chat.RoundsUsed, chat.MaxRounds)
		}
		out = append(out, Alert{
			ID:          "autopilot:" + chat.ID,
			Severity:    SeverityInfo,
			Kind:        KindAutopilot,
			Title:       "Autopilot is running in " + chatLabel(chat),
			Detail:      detail,
			Action:      ActionOpenChat,
			ActionLabel: "Open chat",
			ProjectID:   chat.ProjectID,
			ChatID:      chat.ID,
			At:          chat.AutopilotSince,
		})
	}
	return out
}

// capacityAlerts warns when the fleet has committed nearly all of the memory
// the host budget allows, because the next start is what fails.
func capacityAlerts(in AlertInput) []Alert {
	capacity := in.Capacity
	if capacity.BudgetBytes == 0 {
		return nil
	}
	ratio := float64(capacity.CommittedBytes) / float64(capacity.BudgetBytes)
	if ratio < CapacityWarnRatio {
		return nil
	}
	return []Alert{{
		ID:       "capacity",
		Severity: SeverityWarn,
		Kind:     KindCapacity,
		Title:    "The host memory budget is nearly committed",
		Detail: fmt.Sprintf(
			"%d%% of the container memory budget is committed across %d running container%s.",
			int(ratio*100), capacity.RunningContainers, plural(capacity.RunningContainers),
		),
		Action:      ActionOpenResources,
		ActionLabel: "Open resources",
	}}
}

// notificationAlert points out that nothing will tell anyone when a run
// finishes or a project degrades, which is the difference between this screen
// being a convenience and being the only way to find out.
func notificationAlert(in AlertInput) []Alert {
	if in.NotificationsEnabled {
		return nil
	}
	detail := "Nothing is delivered when a run finishes, an agent needs a decision, or a project degrades."
	label := "Enable notifications"
	if !in.NotificationsConfigured {
		detail = "No Telegram, WhatsApp or webhook sink is connected, so run and health events go nowhere."
		label = "Set up notifications"
	}
	return []Alert{{
		ID:          "notifications",
		Severity:    SeverityInfo,
		Kind:        KindNotifications,
		Title:       "Notifications are off",
		Detail:      detail,
		Action:      ActionEnableNotifications,
		ActionLabel: label,
	}}
}

// backupAlert reports a stalled host backup. A host with no marker directory
// never installed the backup step, and saying "no backup" there would be a
// false alarm rather than a finding, so it stays silent.
func backupAlert(in AlertInput) []Alert {
	if !in.Backup.Readable {
		return nil
	}
	nowMilli := in.Now.UnixMilli()
	if in.Backup.LastAt <= 0 {
		return []Alert{{
			ID:       "backup",
			Severity: SeverityWarn,
			Kind:     KindBackup,
			Title:    "No host backup has ever completed",
			Detail:   "The backup directory exists but holds no finished snapshot. Check remote-backup.timer.",
		}}
	}
	age := time.Duration(nowMilli-in.Backup.LastAt) * time.Millisecond
	if age < BackupStale {
		return nil
	}
	return []Alert{{
		ID:       "backup",
		Severity: SeverityWarn,
		Kind:     KindBackup,
		Title:    "The host has not been backed up recently",
		Detail:   "Last snapshot " + humanizeAge(age) + " ago. The backup timer runs nightly.",
		At:       in.Backup.LastAt,
	}}
}

// chatLabel names a chat the way the sidebar does, so an untitled chat reads
// the same in both places.
func chatLabel(chat ChatState) string {
	title := strings.TrimSpace(chat.Title)
	if title == "" {
		return "an untitled chat"
	}
	return title
}

// humanizeAge renders a duration the way a human reports one: the largest
// unit that still says something useful, and no false precision.
func humanizeAge(age time.Duration) string {
	switch {
	case age < time.Minute:
		return "less than a minute"
	case age < time.Hour:
		minutes := int(age.Minutes())
		return fmt.Sprintf("%d minute%s", minutes, plural(minutes))
	case age < 24*time.Hour:
		hours := int(age.Hours())
		return fmt.Sprintf("%d hour%s", hours, plural(hours))
	default:
		days := int(age.Hours() / 24)
		return fmt.Sprintf("%d day%s", days, plural(days))
	}
}

// humanizeDeadline renders time remaining, including the case where it has
// already run out — a purge that is overdue is the sweep not having run yet,
// and "in -3 hours" would be nonsense.
func humanizeDeadline(remaining time.Duration) string {
	if remaining <= 0 {
		return "at the next sweep"
	}
	return "in " + humanizeAge(remaining)
}

func plural(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}
