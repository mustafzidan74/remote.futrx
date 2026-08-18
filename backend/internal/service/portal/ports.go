package portal

import (
	"context"

	servicegithistory "github.com/futrx-com/remote.futrx.com/internal/service/githistory"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	serviceshare "github.com/futrx-com/remote.futrx.com/internal/service/share"
	serviceusage "github.com/futrx-com/remote.futrx.com/internal/service/usage"
)

// Repository is the storage port for per-project portal records.
// Implementations persist atomically and serialize concurrent callers.
type Repository interface {
	Get(ctx context.Context, projectID serviceproject.ID) (Portal, error)
	Save(ctx context.Context, projectID serviceproject.ID, record Portal) error
}

// Projects resolves the project a portal belongs to.
type Projects interface {
	Get(ctx context.Context, id serviceproject.ID) (serviceproject.Meta, error)
}

// Shares is the live public-preview lookup. The portal links a preview port
// only when this reports an active share for it, which is exactly the set of
// ports an outside visitor can reach at all.
type Shares interface {
	List(ctx context.Context, projectID serviceproject.ID) ([]serviceshare.Share, error)
}

// History reads the workspace's git log. It is the same service the project
// page uses, so the portal changelog and the in-app history never disagree.
type History interface {
	Commits(ctx context.Context, cwd, repo string, limit int) (servicegithistory.Commits, error)
}

// Usage is the optional activity source behind the "show usage" toggle. Only
// the run count is ever rendered — a client portal must not disclose what the
// operator's agent runs cost.
type Usage interface {
	ProjectSummary(
		ctx context.Context,
		projectID string,
		from, to int64,
		callerEmail string,
		isAdmin bool,
	) (serviceusage.Summary, error)
}
