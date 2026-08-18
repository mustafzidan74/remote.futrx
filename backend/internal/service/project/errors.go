package project

import "errors"

var (
	ErrNameRequired       = errors.New("name is required")
	ErrInvalidID          = errors.New("invalid project id")
	ErrNotFound           = errors.New("project not found")
	ErrInvalidSecretKey   = errors.New("invalid secret key (must match [A-Za-z_][A-Za-z0-9_]*)")
	ErrInvalidLimits      = errors.New("invalid container resource limits")
	ErrSecretsUnavailable = errors.New("secrets store is not configured")
	ErrUnknownTemplate    = errors.New("unknown project template")

	// ErrSlugInTrash blocks a new project from taking the container name of a
	// project sitting in the trash. Renaming the trashed slug would break the
	// preview hostnames and the container name it is restored under, so the
	// operator has to decide first.
	ErrSlugInTrash = errors.New(
		"a project with this name is in the Trash - restore or purge it before reusing the name",
	)
	// ErrTrashed rejects a lifecycle operation on a project that is in the
	// trash. Restore it first.
	ErrTrashed = errors.New("project is in the Trash")
	// ErrNotTrashed rejects a restore or purge of a project that is live.
	ErrNotTrashed = errors.New("project is not in the Trash")
	// ErrSnapshotBusy blocks a restore while the automatic trash snapshot is
	// still being packed from the trashed directory.
	ErrSnapshotBusy = errors.New("a snapshot of this project is still being written - try again in a moment")
)
