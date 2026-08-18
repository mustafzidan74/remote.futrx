package snapshot

import (
	"context"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// Repository is the storage port for the per-project snapshot index.
// Implementations persist atomically and serialize concurrent callers per
// project.
type Repository interface {
	List(ctx context.Context, projectID serviceproject.ID) ([]Snapshot, error)
	// Update hands the project's stored records to fn and persists whatever fn
	// returns. fn runs while the project's index is locked, so callers can
	// read-modify-write without racing another request.
	Update(
		ctx context.Context,
		projectID serviceproject.ID,
		fn func([]Snapshot) ([]Snapshot, error),
	) ([]Snapshot, error)
}

// Archive is the host-filesystem port: it packs and unpacks archives and owns
// the archive root. Every path decision below /var/lib/remote/snapshots lives
// behind it, so the service never touches the filesystem directly.
type Archive interface {
	// Available reports whether the host has the tooling to pack an archive.
	Available() bool
	Pack(ctx context.Context, req PackRequest) (PackResult, error)
	// Restore swaps the project's durable directories for the archive's,
	// leaving the replaced tree stashed under RestoreRequest.StashName.
	Restore(ctx context.Context, req RestoreRequest) (RestoreResult, error)
	// ReadDatabase returns the archive's db.sql, or nil when it holds none.
	ReadDatabase(ctx context.Context, projectID, archive string) ([]byte, error)
	Remove(ctx context.Context, projectID, archive string) error
	// RemoveProject deletes every archive of one project.
	RemoveProject(ctx context.Context, projectID string) error
}

// Projects is the lookup and lifecycle the snapshot service needs. It is
// satisfied by *project.Service; declaring it here keeps the dependency
// pointing in one direction only.
type Projects interface {
	Get(ctx context.Context, id serviceproject.ID) (serviceproject.Meta, error)
	Start(ctx context.Context, id serviceproject.ID) (serviceproject.Meta, error)
	Stop(ctx context.Context, id serviceproject.ID) (serviceproject.Meta, error)
}

// Database is the in-container logical dump port. An empty engine from Dump
// means the container has no dump tool, which is the normal answer for a
// template without a database.
type Database interface {
	Dump(ctx context.Context, containerName string) ([]byte, string, error)
	Import(ctx context.Context, containerName, engine string, dump []byte) error
}

// Secrets reads a project's secret values for an opt-in manifest.
type Secrets interface {
	List(ctx context.Context, projectID serviceproject.ID) ([]serviceproject.Secret, error)
}

// Preparer remaps a restored directory into the container idmap. It is the
// same host adapter the container lifecycle uses to prepare a bind mount, so
// a restored workspace ends up owned exactly like a freshly created one.
type Preparer interface {
	Prepare(path string) error
}
