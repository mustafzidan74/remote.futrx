package visualdiff

import (
	"context"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	servicescreenshot "github.com/futrx-com/remote.futrx.com/internal/service/screenshot"
)

// State is one project's whole visual record: the baseline it measures against
// and the comparisons run since. It is stored as a unit because the two are
// only meaningful together — a comparison whose baseline has been replaced is
// a set of numbers about nothing.
type State struct {
	Baseline    *Baseline    `json:"baseline,omitempty"`
	Comparisons []Comparison `json:"comparisons,omitempty"`
}

// Repository is the storage port for that record. Implementations persist
// atomically and serialize concurrent callers per project, exactly like the
// screenshot and snapshot indexes.
type Repository interface {
	Load(ctx context.Context, projectID serviceproject.ID) (State, error)
	// Update hands the stored state to fn and persists what fn returns. fn
	// runs while the project's record is locked, so a background run finishing
	// cannot lose an edit made by the request that started the next one.
	Update(
		ctx context.Context,
		projectID serviceproject.ID,
		fn func(State) (State, error),
	) (State, error)
}

// Blobs is the host-filesystem port holding the PNGs. Every path decision
// below DATA_DIR/visual lives behind it, so the service never touches the
// filesystem directly.
type Blobs interface {
	Write(projectID serviceproject.ID, file string, data []byte) error
	Read(projectID serviceproject.ID, file string) ([]byte, error)
	Remove(projectID serviceproject.ID, file string) error
}

// Capturer photographs one page inside a project's container.
//
// It is the screenshot service's port rather than one of this package's own.
// The two features point the same browser at the same kind of loopback URL
// under the same viewport rules, and a second port would mean a second set of
// rules about what may be photographed — which would drift, and the drift
// would be discovered as an inconsistency between two panels in the UI.
type Capturer = servicescreenshot.Capturer

// Projects is the lookup this service needs: a container name to shell into,
// and a status that lets it refuse a stopped project up front rather than
// after a browser timeout.
type Projects interface {
	Get(ctx context.Context, id serviceproject.ID) (serviceproject.Meta, error)
}
