package agentendpoints

import "context"

// Store persists the whole register. The file-backed implementation lives in
// internal/stores/fileagentendpoints.
type Store interface {
	Load(ctx context.Context) ([]Endpoint, error)
	Save(ctx context.Context, endpoints []Endpoint) error
}

// Secrets reads platform-wide vault values on the run and probe paths only.
//
// It is deliberately narrower than the vault's per-project read: an endpoint
// profile is platform-level and may be used by a chat with no project at all,
// so its key must be an `env` entry scoped to *all* projects. That rule also
// keeps this from becoming a way to read a project-scoped secret from
// somewhere that project's container never reached.
type Secrets interface {
	PlatformValues(ctx context.Context, keys []string) (map[string]string, error)
}

// Probe is one Test: run this CLI, with this environment and these extra
// arguments, against this prompt, inside a container.
type Probe struct {
	CLI CLI
	// Model is the model id to ask for, empty to leave the CLI on the
	// endpoint's own default.
	Model string
	Env   map[string]string
	Args  []string
	// Prompt is the two-word request the probe sends. It is deliberately
	// trivial: the question is whether the endpoint answers at all.
	Prompt string
}

// Containers runs a probe inside a project container. Policy — which profile,
// which model, what gets masked — stays in this package; the adapter only
// performs the exec and hands back raw output.
type Containers interface {
	Probe(ctx context.Context, containerName string, probe Probe) (string, error)
}

// Target is one project's container, as this register sees it.
type Target struct {
	ProjectID     string
	ContainerName string
	Running       bool
}

// Projects resolves a project to its container for the Test action. It is
// satisfied by the project service; declaring it here keeps the dependency
// pointing one way.
type Projects interface {
	EndpointTarget(ctx context.Context, projectID string) (Target, error)
}
