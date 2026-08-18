// Package snapshot owns per-project point-in-time copies: an archive of the
// project's durable host directories (the workspace and the agent homes)
// together with a logical database dump taken from inside the container and a
// manifest describing what the archive holds.
//
// Snapshots exist because a project's durable state is split across two
// places. /workspace and the agent homes live on the host and survive a
// container replacement; a template's database (MariaDB for the WordPress
// stack) lives in the disposable container rootfs and does not. An archive
// that only tarred the host directories would restore a WordPress install
// with no content in it, so the dump is part of the same artifact.
package snapshot

import (
	"errors"
	"time"
)

// ID identifies one snapshot inside a project.
type ID string

// Status is the lifecycle of one snapshot record. Packing a multi-gigabyte
// workspace takes minutes, so a record exists (and is listed) before its
// archive does.
type Status string

const (
	StatusPending Status = "pending"
	StatusRunning Status = "running"
	StatusReady   Status = "ready"
	StatusFailed  Status = "failed"
)

// Kind separates the snapshots a user asked for from the one the platform
// takes automatically when a project is moved to the trash.
type Kind string

const (
	KindManual Kind = "manual"
	KindTrash  Kind = "trash"
)

// JobKind labels a background operation so the UI can describe what is
// running without inspecting the snapshot records.
type JobKind string

const (
	JobCapture JobKind = "capture"
	JobRestore JobKind = "restore"
)

const (
	// RetentionCount is how many snapshots one project keeps. The oldest are
	// evicted (record and archive) after every successful capture.
	//
	// TODO(admin-settings): make this an operator-tunable fleet setting next
	// to the resource policy instead of a compiled constant.
	RetentionCount = 10

	// MaxLabelLength bounds the operator's reminder text.
	MaxLabelLength = 80

	// ManifestName and DatabaseName are part of the on-disk archive format: a
	// restore of an archive written by an older build must keep working.
	ManifestName = "meta.json"
	DatabaseName = "db.sql"

	// WorkspaceEntry and AgentHomeEntry are the two durable directories of a
	// project, relative to its host directory.
	WorkspaceEntry = "workspace"
	AgentHomeEntry = "agent-home"

	// ManifestSchemaVersion is bumped whenever Manifest gains a field a
	// restore must understand.
	ManifestSchemaVersion = 1

	// captureTimeout and restoreTimeout bound one background run. Packing a
	// large workspace is minutes of work, so the budget is generous; it exists
	// to stop a wedged tar from leaking a goroutine forever.
	captureTimeout = 60 * time.Minute
	restoreTimeout = 60 * time.Minute

	// databaseTimeout bounds one dump or import inside a container.
	databaseTimeout = 10 * time.Minute
)

var (
	ErrUnavailable     = errors.New("snapshot storage is not configured")
	ErrNotFound        = errors.New("snapshot not found")
	ErrNotReady        = errors.New("snapshot is still being written")
	ErrLabelTooLong    = errors.New("snapshot label is too long")
	ErrBusy            = errors.New("another snapshot operation is already running for this project")
	ErrConfirmRequired = errors.New("restoring a snapshot replaces the project files: confirm the action")
)

// Snapshot is the stored record of one archive.
type Snapshot struct {
	ID        ID     `json:"id"`
	Label     string `json:"label,omitempty"`
	Kind      Kind   `json:"kind"`
	Status    Status `json:"status"`
	Error     string `json:"error,omitempty"`
	CreatedBy string `json:"createdBy,omitempty"`
	CreatedAt int64  `json:"createdAt"`
	// FinishedAt is when packing settled, successfully or not.
	FinishedAt int64 `json:"finishedAt,omitempty"`
	// Archive is the file name inside the project's archive directory. It is
	// empty until packing succeeds.
	Archive string `json:"archive,omitempty"`
	// Format is "tar.zst" when the host has zstd and "tar.gz" otherwise.
	Format    string `json:"format,omitempty"`
	SizeBytes int64  `json:"sizeBytes,omitempty"`
	// HasDatabase reports whether the archive carries a db.sql. Templates
	// without a database engine produce archives without one.
	HasDatabase     bool   `json:"hasDatabase"`
	DatabaseEngine  string `json:"databaseEngine,omitempty"`
	IncludesSecrets bool   `json:"includesSecrets"`
	Slug            string `json:"slug,omitempty"`
	Template        string `json:"template,omitempty"`
}

// Terminal reports whether the record will not change again.
func (s Snapshot) Terminal() bool {
	return s.Status == StatusReady || s.Status == StatusFailed
}

// Manifest is meta.json inside the archive: enough context to identify what
// was captured without consulting DATA_DIR, which a disaster recovery may not
// have.
//
// Secrets are omitted unless the caller explicitly asked for them, because an
// archive is a plain file an operator may copy off the box.
type Manifest struct {
	SchemaVersion int               `json:"schemaVersion"`
	SnapshotID    string            `json:"snapshotId"`
	ProjectID     string            `json:"projectId"`
	ProjectName   string            `json:"projectName"`
	Slug          string            `json:"slug"`
	ContainerName string            `json:"containerName,omitempty"`
	Template      string            `json:"template"`
	Label         string            `json:"label,omitempty"`
	Kind          string            `json:"kind"`
	CreatedBy     string            `json:"createdBy,omitempty"`
	CreatedAt     int64             `json:"createdAt"`
	Directories   []string          `json:"directories"`
	Database      string            `json:"database,omitempty"`
	Secrets       map[string]string `json:"secrets,omitempty"`
}

// Job is the status of one background capture or restore. Records survive a
// restart; jobs do not — they exist so the UI can show progress and surface a
// failure that has no snapshot record of its own (a restore).
type Job struct {
	ID         string  `json:"id"`
	ProjectID  string  `json:"projectId"`
	Kind       JobKind `json:"kind"`
	SnapshotID ID      `json:"snapshotId,omitempty"`
	Status     Status  `json:"status"`
	Error      string  `json:"error,omitempty"`
	StartedAt  int64   `json:"startedAt"`
	FinishedAt int64   `json:"finishedAt,omitempty"`
}

// CreateInput is the caller-supplied half of a manual snapshot.
type CreateInput struct {
	Label string `json:"label,omitempty"`
	// IncludeSecrets copies the project's secret values into the manifest.
	// Off by default: an archive is a plain file that may leave the host.
	IncludeSecrets bool `json:"includeSecrets,omitempty"`
	// Confirm is required by the restore endpoint, not by create; it lives on
	// the shared request body the transport decodes.
	Confirm bool `json:"confirm,omitempty"`
}

// PackRequest is the archive port's input. SourceDir holds the entries; the
// manifest and database bytes are staged next to the archive and written into
// it as ManifestName and DatabaseName.
type PackRequest struct {
	ProjectID string
	// Name is the archive base name without its format extension.
	Name      string
	SourceDir string
	Entries   []string
	Manifest  []byte
	Database  []byte
}

// PackResult reports where the archive landed.
type PackResult struct {
	Archive   string
	Format    string
	SizeBytes int64
}

// RestoreRequest asks the archive port to swap a project's durable
// directories for the ones inside an archive.
type RestoreRequest struct {
	ProjectID string
	Archive   string
	// ProjectDir is the project's host directory (the parent of workspace).
	ProjectDir string
	// StashName is the directory the replaced tree is moved to, inside the
	// project's archive directory.
	StashName string
	Entries   []string
}

// RestoreResult reports what the swap produced.
type RestoreResult struct {
	StashPath string
	Database  []byte
}
