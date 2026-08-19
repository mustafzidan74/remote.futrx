package dashboard

import (
	"context"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	servicehealth "github.com/futrx-com/remote.futrx.com/internal/service/health"
	servicemonitoring "github.com/futrx-com/remote.futrx.com/internal/service/monitoring"
	servicenotify "github.com/futrx-com/remote.futrx.com/internal/service/notify"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	serviceresources "github.com/futrx-com/remote.futrx.com/internal/service/resources"
	serviceschedule "github.com/futrx-com/remote.futrx.com/internal/service/schedule"
	serviceserverinfo "github.com/futrx-com/remote.futrx.com/internal/service/serverinfo"
	servicesitewatch "github.com/futrx-com/remote.futrx.com/internal/service/sitewatch"
	servicesnapshot "github.com/futrx-com/remote.futrx.com/internal/service/snapshot"
	serviceusage "github.com/futrx-com/remote.futrx.com/internal/service/usage"
)

// The ports below are each the narrowest slice of an existing service this
// package needs. They are declared here rather than taking the concrete
// services so the aggregation can be exercised against fakes: every rule in
// alerts.go is decided by data, and a test that has to stand up LXD to prove
// "a stale snapshot warns" is a test nobody will keep running.
//
// Every optional port may be left nil. A deployment without a usage ledger,
// without snapshots, or without a container runtime still gets a home screen
// — with the sections those services would have filled marked unavailable
// rather than silently reported as zero.

// Projects lists what the caller may see, live and trashed alike.
type Projects interface {
	ListVisible(ctx context.Context, callerEmail string, isAdmin bool) ([]serviceproject.Meta, error)
	ListTrashed(ctx context.Context, callerEmail string, isAdmin bool) ([]serviceproject.Meta, error)
}

// Chats lists the caller's chats. It is the membership-filtered listing the
// sidebar already uses, so "recent activity" and the sidebar never disagree
// about which chats exist.
type Chats interface {
	List(ctx context.Context, callerEmail string, isAdmin bool) ([]servicechat.Meta, error)
}

// UsageLedger reads the token and cost ledger. Both methods apply their own
// visibility filter on top of the caller identity. It is named for the store
// rather than for the section it fills, because Usage is already the shape of
// that section on the wire.
type UsageLedger interface {
	Summary(
		ctx context.Context,
		query serviceusage.Query,
		callerEmail string,
		isAdmin bool,
	) (serviceusage.Summary, error)
	Records(
		ctx context.Context,
		query serviceusage.RecordQuery,
		callerEmail string,
		isAdmin bool,
	) (serviceusage.RecordPage, error)
}

// Health is the project health monitor's cache.
type Health interface {
	Enabled() bool
	Snapshot(ids []serviceproject.ID) []servicehealth.ProjectHealth
}

// Schedules lists the caller's scheduled tasks.
type Schedules interface {
	List(ctx context.Context, callerEmail string, isAdmin bool) ([]serviceschedule.Task, error)
}

// Snapshots reports what has been archived per project.
type Snapshots interface {
	Available() bool
	List(ctx context.Context, projectID serviceproject.ID) ([]servicesnapshot.Snapshot, error)
}

// Notifications reports whether outbound alerting is switched on. Only the
// enabled flag and whether a sink is configured are read; no credential ever
// reaches this package.
type Notifications interface {
	Config() servicenotify.Config
}

// PlatformHealth is the same report /healthz serves.
type PlatformHealth interface {
	Report(ctx context.Context, proxied bool) servicemonitoring.Report
}

// ClientSites is the always-on watcher for the operator's client websites.
// The dashboard asks it only for what is not green, because a card that lists
// forty healthy shops is a card nobody reads.
type ClientSites interface {
	Available() bool
	NotGreen(ctx context.Context, callerEmail string, isAdmin bool) ([]servicesitewatch.View, error)
}

// Capacity is the fleet's memory budget and running-container count.
type Capacity interface {
	Get(ctx context.Context) serviceresources.View
}

// Backups reports the host's backup marker directory.
type Backups interface {
	Backup(ctx context.Context) serviceserverinfo.BackupInfo
}
