package dashboard

import (
	"testing"
	"time"

	servicemonitoring "github.com/futrx-com/remote.futrx.com/internal/service/monitoring"
)

// alertNow is the instant every age-based rule is measured against.
var alertNow = time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

// baseAlertInput is a box with nothing wrong: notifications on, no trash, no
// backup directory, every project healthy. Each case below perturbs exactly
// one thing, so a rule that fires is provably the one the case is about.
func baseAlertInput() AlertInput {
	return AlertInput{
		Now:                     alertNow,
		NotificationsEnabled:    true,
		NotificationsConfigured: true,
		Platform:                servicemonitoring.Report{Status: servicemonitoring.StatusOK},
	}
}

func milli(offset time.Duration) int64 {
	return alertNow.Add(offset).UnixMilli()
}

// alertIDs is what the assertions compare against: the identity of each
// finding, not its prose.
func alertIDs(alerts []Alert) []string {
	ids := make([]string, 0, len(alerts))
	for _, alert := range alerts {
		ids = append(ids, alert.ID)
	}
	return ids
}

func findAlert(alerts []Alert, id string) (Alert, bool) {
	for _, alert := range alerts {
		if alert.ID == id {
			return alert, true
		}
	}
	return Alert{}, false
}

func TestDeriveAlerts(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*AlertInput)
		wantIDs []string
		// want is checked against the alert named by the first wanted id,
		// zero-valued fields are not compared.
		wantSeverity Severity
		wantAction   Action
	}{
		{
			name:    "a healthy box has nothing to say",
			mutate:  func(*AlertInput) {},
			wantIDs: nil,
		},
		{
			name: "a degraded project warns",
			mutate: func(in *AlertInput) {
				in.Projects = []Project{{ID: "p1", Name: "Shop", Health: "warn"}}
			},
			wantIDs:      []string{"health:p1"},
			wantSeverity: SeverityWarn,
			wantAction:   ActionOpenProject,
		},
		{
			name: "a critical project is critical",
			mutate: func(in *AlertInput) {
				in.Projects = []Project{{ID: "p1", Name: "Shop", Health: "crit"}}
			},
			wantIDs:      []string{"health:p1"},
			wantSeverity: SeverityCrit,
			wantAction:   ActionOpenProject,
		},
		{
			name: "an unmonitored project is not a finding",
			mutate: func(in *AlertInput) {
				in.Projects = []Project{{ID: "p1", Name: "Shop", Health: "unknown"}}
			},
			wantIDs: nil,
		},
		{
			name: "trash about to be purged offers a restore",
			mutate: func(in *AlertInput) {
				in.Trash = []TrashedProject{{ID: "p9", Name: "Old", ExpiresAt: milli(TrashExpiryWarning - time.Hour)}}
			},
			wantIDs:      []string{"trash:p9"},
			wantSeverity: SeverityWarn,
			wantAction:   ActionRestoreTrash,
		},
		{
			name: "trash with days left is left alone",
			mutate: func(in *AlertInput) {
				in.Trash = []TrashedProject{{ID: "p9", Name: "Old", ExpiresAt: milli(TrashExpiryWarning + time.Hour)}}
			},
			wantIDs: nil,
		},
		{
			name: "trash kept forever has no deadline to warn about",
			mutate: func(in *AlertInput) {
				in.Trash = []TrashedProject{{ID: "p9", Name: "Old", ExpiresAt: 0}}
			},
			wantIDs: nil,
		},
		{
			name: "a running project with a stale snapshot is offered a fresh one",
			mutate: func(in *AlertInput) {
				in.SnapshotsAvailable = true
				in.SnapshotsKnown = map[string]bool{"p1": true}
				in.Projects = []Project{{
					ID: "p1", Name: "Shop", Status: "running", Health: "ok",
					LastSnapshotAt: milli(-(SnapshotStale + time.Hour)),
				}}
			},
			wantIDs:      []string{"snapshot:p1"},
			wantSeverity: SeverityWarn,
			wantAction:   ActionSnapshotNow,
		},
		{
			name: "a running project never snapshotted is offered one",
			mutate: func(in *AlertInput) {
				in.SnapshotsAvailable = true
				in.SnapshotsKnown = map[string]bool{"p1": true}
				in.Projects = []Project{{ID: "p1", Name: "Shop", Status: "running", Health: "ok"}}
			},
			wantIDs:    []string{"snapshot:p1"},
			wantAction: ActionSnapshotNow,
		},
		{
			name: "a stopped project is never nagged about snapshots",
			mutate: func(in *AlertInput) {
				in.SnapshotsAvailable = true
				in.SnapshotsKnown = map[string]bool{"p1": true}
				in.Projects = []Project{{ID: "p1", Name: "Shop", Status: "stopped", Health: "ok"}}
			},
			wantIDs: nil,
		},
		{
			name: "a project whose archives could not be read is not guessed about",
			mutate: func(in *AlertInput) {
				in.SnapshotsAvailable = true
				in.SnapshotsKnown = map[string]bool{}
				in.Projects = []Project{{ID: "p1", Name: "Shop", Status: "running", Health: "ok"}}
			},
			wantIDs: nil,
		},
		{
			name: "a fresh snapshot says nothing",
			mutate: func(in *AlertInput) {
				in.SnapshotsAvailable = true
				in.SnapshotsKnown = map[string]bool{"p1": true}
				in.Projects = []Project{{
					ID: "p1", Name: "Shop", Status: "running", Health: "ok",
					LastSnapshotAt: milli(-time.Hour),
				}}
			},
			wantIDs: nil,
		},
		{
			name: "a running autopilot loop is reported",
			mutate: func(in *AlertInput) {
				in.Chats = []ChatState{{
					ID: "c1", Title: "Ship it", Running: true, Autopilot: true,
					RoundsUsed: 3, MaxRounds: 10,
				}}
			},
			wantIDs:      []string{"autopilot:c1"},
			wantSeverity: SeverityInfo,
			wantAction:   ActionOpenChat,
		},
		{
			name: "an armed but idle autopilot is not a loop in flight",
			mutate: func(in *AlertInput) {
				in.Chats = []ChatState{{ID: "c1", Title: "Ship it", Autopilot: true}}
			},
			wantIDs: nil,
		},
		{
			name: "notifications switched off is worth saying",
			mutate: func(in *AlertInput) {
				in.NotificationsEnabled = false
			},
			wantIDs:      []string{"notifications"},
			wantSeverity: SeverityInfo,
			wantAction:   ActionEnableNotifications,
		},
		{
			name: "a stale host backup warns",
			mutate: func(in *AlertInput) {
				in.Backup = BackupState{Readable: true, LastAt: milli(-(BackupStale + time.Hour))}
			},
			wantIDs:      []string{"backup"},
			wantSeverity: SeverityWarn,
		},
		{
			name: "a recent host backup says nothing",
			mutate: func(in *AlertInput) {
				in.Backup = BackupState{Readable: true, LastAt: milli(-time.Hour)}
			},
			wantIDs: nil,
		},
		{
			name: "a host with no backup directory is never accused of missing backups",
			mutate: func(in *AlertInput) {
				in.Backup = BackupState{Readable: false}
			},
			wantIDs: nil,
		},
		{
			name: "a readable but empty backup directory warns",
			mutate: func(in *AlertInput) {
				in.Backup = BackupState{Readable: true}
			},
			wantIDs:      []string{"backup"},
			wantSeverity: SeverityWarn,
		},
		{
			name: "a nearly committed memory budget warns",
			mutate: func(in *AlertInput) {
				in.Capacity = CapacityState{BudgetBytes: 1000, CommittedBytes: 950, RunningContainers: 4}
			},
			wantIDs:      []string{"capacity"},
			wantSeverity: SeverityWarn,
			wantAction:   ActionOpenResources,
		},
		{
			name: "a half-committed budget says nothing",
			mutate: func(in *AlertInput) {
				in.Capacity = CapacityState{BudgetBytes: 1000, CommittedBytes: 500}
			},
			wantIDs: nil,
		},
		{
			name: "a degraded platform is critical",
			mutate: func(in *AlertInput) {
				in.Platform = servicemonitoring.Report{
					Status:  servicemonitoring.StatusDegraded,
					Details: []string{servicemonitoring.DetailLXD},
				}
			},
			wantIDs:      []string{"platform"},
			wantSeverity: SeverityCrit,
			wantAction:   ActionOpenMonitoring,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			in := baseAlertInput()
			test.mutate(&in)
			alerts := DeriveAlerts(in)

			got := alertIDs(alerts)
			if len(got) != len(test.wantIDs) {
				t.Fatalf("alerts = %v, want %v", got, test.wantIDs)
			}
			for index, want := range test.wantIDs {
				if got[index] != want {
					t.Fatalf("alerts = %v, want %v", got, test.wantIDs)
				}
			}
			if len(test.wantIDs) == 0 {
				return
			}
			alert, _ := findAlert(alerts, test.wantIDs[0])
			if test.wantSeverity != "" && alert.Severity != test.wantSeverity {
				t.Fatalf("severity = %q, want %q", alert.Severity, test.wantSeverity)
			}
			if test.wantAction != ActionNone && alert.Action != test.wantAction {
				t.Fatalf("action = %q, want %q", alert.Action, test.wantAction)
			}
			if test.wantAction != ActionNone && alert.ActionLabel == "" {
				t.Fatal("actionLabel is empty, want a button caption for the offered action")
			}
		})
	}
}

// The Attention column is read top-down, so the worst thing on the box has to
// be the first row regardless of the order the rules happen to run in.
func TestDeriveAlertsOrdersMostSevereFirst(t *testing.T) {
	in := baseAlertInput()
	in.NotificationsEnabled = false
	in.Projects = []Project{
		{ID: "warn", Name: "Warn", Health: "warn"},
		{ID: "crit", Name: "Crit", Health: "crit"},
	}

	alerts := DeriveAlerts(in)

	want := []Severity{SeverityCrit, SeverityWarn, SeverityInfo}
	if len(alerts) != len(want) {
		t.Fatalf("alerts = %v, want %d rows", alertIDs(alerts), len(want))
	}
	for index, severity := range want {
		if alerts[index].Severity != severity {
			t.Fatalf("alerts = %v, want severities %v", alertIDs(alerts), want)
		}
	}
}

func TestClientSiteAlerts(t *testing.T) {
	tests := []struct {
		name         string
		sites        []ClientSiteRow
		wantIDs      []string
		wantSeverity Severity
		wantTitle    string
	}{
		{
			name:    "nothing watched says nothing",
			sites:   nil,
			wantIDs: []string{},
		},
		{
			name: "a down site is critical",
			sites: []ClientSiteRow{{
				ID: "aaaaaaaaaaaaaaaaaaaaaaaa", Label: "shop.example.com",
				Status: "down", Detail: "answered HTTP 502", Since: milli(-20 * time.Minute),
			}},
			wantIDs:      []string{"site:aaaaaaaaaaaaaaaaaaaaaaaa"},
			wantSeverity: SeverityCrit,
			wantTitle:    "shop.example.com is down",
		},
		{
			name: "a slow site is a warning",
			sites: []ClientSiteRow{{
				ID: "bbbbbbbbbbbbbbbbbbbbbbbb", Label: "blog.example.com", Status: "slow",
			}},
			wantIDs:      []string{"site:bbbbbbbbbbbbbbbbbbbbbbbb"},
			wantSeverity: SeverityWarn,
			wantTitle:    "blog.example.com is slow",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			input := baseAlertInput()
			input.Sites = test.sites
			alerts := DeriveAlerts(input)
			got := alertIDs(alerts)
			if len(got) != len(test.wantIDs) {
				t.Fatalf("alerts = %v, want %v", got, test.wantIDs)
			}
			if len(test.wantIDs) == 0 {
				return
			}
			alert, ok := findAlert(alerts, test.wantIDs[0])
			if !ok {
				t.Fatalf("alerts = %v, want %q", got, test.wantIDs[0])
			}
			if alert.Severity != test.wantSeverity {
				t.Fatalf("severity = %q, want %q", alert.Severity, test.wantSeverity)
			}
			if alert.Title != test.wantTitle {
				t.Fatalf("title = %q, want %q", alert.Title, test.wantTitle)
			}
			if alert.Kind != KindSiteWatch {
				t.Fatalf("kind = %q, want %q", alert.Kind, KindSiteWatch)
			}
			if alert.Action != ActionOpenClientSites {
				t.Fatalf("action = %q, want %q", alert.Action, ActionOpenClientSites)
			}
			if alert.SiteID != string(test.sites[0].ID) {
				t.Fatalf("siteId = %q, want the row's id", alert.SiteID)
			}
		})
	}
}

// A down client site outranks a warning about this box, because a customer's
// shop being dark is the most urgent thing this screen can say.
func TestClientSiteAlertsSortAheadOfWarnings(t *testing.T) {
	input := baseAlertInput()
	input.NotificationsEnabled = false
	input.Sites = []ClientSiteRow{{ID: "aaaaaaaaaaaaaaaaaaaaaaaa", Label: "shop.example.com", Status: "down"}}
	alerts := DeriveAlerts(input)
	if len(alerts) < 2 {
		t.Fatalf("alerts = %v, want the site and the notifications finding", alertIDs(alerts))
	}
	if alerts[0].ID != "site:aaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("first alert = %q, want the down site", alerts[0].ID)
	}
}
