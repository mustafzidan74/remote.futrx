package serverinfo

import (
	"context"
	"time"
)

type Collector interface {
	Collect(ctx context.Context, now time.Time) Snapshot
}

// BackupProbe reads the host's backup marker directory. It is optional: a
// deployment that never installed the nightly backup timer simply leaves it
// unset, and every caller sees an unreadable BackupInfo.
type BackupProbe interface {
	Backup(ctx context.Context) BackupInfo
}

type Service struct {
	collector     Collector
	backups       BackupProbe
	appVersion    string
	dataPath      string
	workspacePath string
	startedAt     time.Time
}

// Option customizes an optional part of the server-info report.
type Option func(*Service)

// WithBackupProbe attaches the host backup marker reader. Without it the
// report's Backup section stays unreadable, which downstream readers treat as
// "this host has no backup timer" rather than "the backups have stopped".
func WithBackupProbe(probe BackupProbe) Option {
	return func(s *Service) { s.backups = probe }
}

func New(
	collector Collector,
	appVersion, dataPath, workspacePath string,
	options ...Option,
) *Service {
	service := &Service{
		collector:     collector,
		appVersion:    appVersion,
		dataPath:      dataPath,
		workspacePath: workspacePath,
		startedAt:     time.Now(),
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) Collect(ctx context.Context) Info {
	now := time.Now()
	snapshot := s.collector.Collect(ctx, now)
	snapshot.Host.ServiceUptimeSec = int64(now.Sub(s.startedAt).Seconds())
	snapshot.Host.AppVersion = s.appVersion
	snapshot.Host.DataPath = s.dataPath
	snapshot.Host.WorkspacePath = s.workspacePath
	return Info{
		CollectedAt: now.Unix(),
		Host:        snapshot.Host,
		CPU:         snapshot.CPU,
		Memory:      snapshot.Memory,
		Storage:     snapshot.Storage,
		Network:     snapshot.Network,
		Process:     snapshot.Process,
		Backup:      s.Backup(ctx),
	}
}

// Backup reports the host backup marker on its own, for callers that want the
// one section without collecting the whole host snapshot.
func (s *Service) Backup(ctx context.Context) BackupInfo {
	if s == nil || s.backups == nil {
		return BackupInfo{}
	}
	return s.backups.Backup(ctx)
}
