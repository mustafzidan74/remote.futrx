package globalsecrets

import "context"

// Store persists the whole vault document. The file-backed implementation
// lives in internal/stores/fileglobalsecrets.
type Store interface {
	Load(ctx context.Context) ([]Secret, error)
	Save(ctx context.Context, secrets []Secret) error
}

// ContainerEnvironment applies environment variables to a container. It is
// the same port the project service uses, declared here so the vault does not
// depend on the project package.
type ContainerEnvironment interface {
	ApplyDiff(ctx context.Context, containerName string, set map[string]string, unset []string) error
}

// ContainerMaterializer writes vault files and SSH material into a container
// and remembers what it wrote. Policy — what is stale — stays in the service;
// this port only performs the transfer.
type ContainerMaterializer interface {
	// Manifest reads the record the previous sync left in the container. A
	// container without one yields the zero manifest, not an error.
	Manifest(ctx context.Context, containerName string) (Manifest, error)
	// Apply pushes every file in material, merges the managed ssh_config and
	// known_hosts regions, deletes staleFiles, and stores the new manifest.
	Apply(ctx context.Context, containerName string, material Material, staleFiles []string) error
}

// SSHProber runs a connectivity probe from the host against one target.
type SSHProber interface {
	Probe(ctx context.Context, target SSHTarget) (TestResult, error)
}

// SecretTarget is one project the vault can reach, with the secret keys that
// project defines itself. The own keys are what shadow an `env` entry.
type SecretTarget struct {
	ProjectID     string
	ContainerName string
	Running       bool
	OwnKeys       []string
}

// Projects enumerates the containers a vault change has to be pushed to. It
// is satisfied by the project service; declaring it here keeps the dependency
// pointing one way only.
type Projects interface {
	SecretTargets(ctx context.Context) ([]SecretTarget, error)
}
