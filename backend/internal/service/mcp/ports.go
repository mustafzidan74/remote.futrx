package mcp

import "context"

// Store persists the platform registry. The file-backed implementation lives
// in internal/stores/filemcp.
type Store interface {
	Load(ctx context.Context) ([]Server, error)
	Save(ctx context.Context, servers []Server) error
}

// ProjectStore persists one document per project: the disabled list, the
// project-only entries, and the last materialization record.
type ProjectStore interface {
	Load(ctx context.Context, projectID string) (ProjectSettings, error)
	Save(ctx context.Context, projectID string, settings ProjectSettings) error
}

// Containers is the container-facing port. Policy — what is stale, what a
// signature means — stays in this package; the adapter only performs the
// transfer and the probe.
type Containers interface {
	// Manifest reads the record the previous pass left in the container. A
	// container without one yields the zero manifest, not an error.
	Manifest(ctx context.Context, containerName string) (Manifest, error)
	// Apply writes every file in material, merges the managed region of the
	// codex config, deletes staleFiles, and stores the new manifest.
	Apply(ctx context.Context, containerName string, material Material, staleFiles []string) error
	// Probe runs one server's handshake inside the container and returns its
	// raw output. Values never reach a log; the caller masks them out of the
	// output before it leaves the backend.
	Probe(ctx context.Context, containerName string, server Server) (string, error)
}

// Secrets reads vault values on the materialization path only. It is scoped
// per project on purpose: an entry may only resolve keys the project's own
// container would already receive.
type Secrets interface {
	ValuesForProject(ctx context.Context, projectID string, keys []string) (map[string]string, error)
}

// Target is one project's container, as the registry sees it.
type Target struct {
	ProjectID     string
	ContainerName string
	Running       bool
}

// Projects resolves a project to its container. It is satisfied by the
// project service; declaring it here keeps the dependency pointing one way.
type Projects interface {
	MCPTarget(ctx context.Context, projectID string) (Target, error)
}
