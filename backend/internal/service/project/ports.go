package project

import "context"

type Repository interface {
	List(ctx context.Context) ([]Meta, error)
	Create(ctx context.Context, meta Meta) (Meta, error)
	Get(ctx context.Context, id ID) (Meta, error)
	GetBySlug(ctx context.Context, slug string) (Meta, error)
	Update(ctx context.Context, id ID, fn func(*Meta)) (Meta, error)
	SetStatus(ctx context.Context, id ID, status Status, errMsg string) (Meta, error)
	Delete(ctx context.Context, id ID) error
}

// ContainerLifecycle is the project service's container state-transition port.
// Implementations may use LXD or any other runtime; project policy only relies
// on these lifecycle operations.
type ContainerLifecycle interface {
	Available() bool
	// Ensure converges a project to one complete, running container with all
	// durable mounts attached.
	Ensure(ctx context.Context, p Meta) error
	Busy(ctx context.Context, containerName string) (bool, error)
	Start(ctx context.Context, containerName string) error
	Stop(ctx context.Context, containerName string) error
	// Restart force-restarts the container from the host, killing any
	// wedged processes inside without needing their cooperation.
	Restart(ctx context.Context, containerName string) error
	Delete(ctx context.Context, containerName string) error
	State(ctx context.Context, containerName string) (ContainerState, error)
	// EnsureResources converges the fleet-default resource envelope (the
	// managed LXD profile) onto an existing container.
	EnsureResources(ctx context.Context, containerName string) error
	// SetResourceLimits applies container-local overrides. Empty values remove
	// overrides so the managed fleet profile becomes effective again.
	SetResourceLimits(ctx context.Context, containerName string, limits ContainerLimits) error
}

// Snapshots is the project service's safety-net port. Deleting a project
// takes an automatic snapshot first, and un-deleting one re-imports the
// database that lived in the destroyed container rootfs.
//
// The port is declared here, and satisfied by the snapshot service, so the
// dependency points one way only: snapshots know about projects, projects
// only know this interface.
type Snapshots interface {
	// CaptureTrash records the automatic snapshot taken while a project is
	// moved to the trash. sourceDir is where the project files now live and
	// database is the dump taken before the container was destroyed. It
	// returns the snapshot id, which the project meta remembers.
	CaptureTrash(
		ctx context.Context,
		id ID,
		sourceDir string,
		database []byte,
		engine, actor string,
	) (string, error)
	// RestoreDatabase re-imports one snapshot's database into the project's
	// running container. A snapshot without a database is a no-op.
	RestoreDatabase(ctx context.Context, id ID, snapshotID string) error
	// PurgeAll drops every snapshot of a project, permanently.
	PurgeAll(ctx context.Context, id ID) error
	// Busy reports whether a capture or restore is still running. Moving a
	// project out of the trash while its trash snapshot is being packed would
	// pull the directories out from under the archiver.
	Busy(id ID) bool
}

// ProjectStorage moves a project's durable host directories between the live
// workspace root and the trash root. It is the only thing that knows either
// path; project policy only knows "trashed" and "not trashed".
type ProjectStorage interface {
	// Trash moves projectDir into the trash root and returns its new path.
	Trash(ctx context.Context, id ID, projectDir string) (string, error)
	// Untrash moves the trashed copy back to projectDir.
	Untrash(ctx context.Context, id ID, projectDir string) error
	// PurgeTrash permanently removes the trashed copy.
	PurgeTrash(ctx context.Context, id ID) error
}

// ContainerDatabase dumps a template's database from inside the container.
// The engine is empty when the container has no dump tool, which is the
// normal answer for a template that ships no database.
type ContainerDatabase interface {
	Dump(ctx context.Context, containerName string) ([]byte, string, error)
}

// ContainerPolicy supplies the fleet-wide resource policy that per-project
// operations are measured against: the defaults a project inherits, the
// ceiling an override may not pass, live host capacity, and whether the
// storage pool can enforce a disk quota at all.
type ContainerPolicy interface {
	Policy(ctx context.Context) ContainerPolicySnapshot
	// Validate rejects an override above the operator ceiling or above what
	// the host can physically back.
	Validate(ctx context.Context, limits ContainerLimits) error
}

// ContainerAdmission is the aggregate guard consulted before a container is
// launched or started. It exists so a host reboot that autostarts every
// workspace cannot commit more memory than the host has.
type ContainerAdmission interface {
	// AuthorizeStart reports whether one more running container fits.
	// memoryLimit is the candidate's effective ceiling; an empty value means
	// "the fleet default". force is the admin escape hatch.
	AuthorizeStart(ctx context.Context, containerName, memoryLimit string, force bool) error
}

// ContainerEnvironment applies project secrets to future container sessions.
type ContainerEnvironment interface {
	ApplyDiff(ctx context.Context, containerName string, set map[string]string, unset []string) error
}

// GlobalSecrets is the platform secrets vault as project policy sees it: a
// source of environment variables, credential files, and SSH targets every
// scoped project container inherits without an operator re-entering them.
//
// The port is declared here, and satisfied by an adapter over the vault
// service, so the dependency points one way only: the vault knows nothing
// about projects beyond the ids and container names it is handed.
type GlobalSecrets interface {
	// SyncContainer converges one container onto the vault. ownKeys are the
	// project's own secret keys, which shadow vault entries of the same name.
	SyncContainer(ctx context.Context, projectID, containerName string, ownKeys []string) error
	// InheritedForProject lists what the vault contributes to one project, so
	// a member can see everything the container will have.
	InheritedForProject(ctx context.Context, projectID string, ownKeys []string) ([]InheritedSecret, error)
}

// ContainerInspector returns a best-effort diagnostic snapshot.
type ContainerInspector interface {
	Inspect(ctx context.Context, containerName string) (ContainerInspect, error)
}

// ContainerNetwork repairs guest network configuration.
type ContainerNetwork interface {
	Repair(ctx context.Context, containerName string) error
}

// ContainerListeners discovers externally reachable guest applications.
type ContainerListeners interface {
	List(ctx context.Context, containerName string) ([]ContainerApp, error)
}

// ContainerBrowser manages the browser capabilities consumed by projects.
type ContainerBrowser interface {
	Ensure(ctx context.Context, containerName string) error
	Stop(ctx context.Context, containerName string) error
	StopView(ctx context.Context, containerName string) error
	Navigate(ctx context.Context, containerName, url string) error
	Status(ctx context.Context, containerName string) (AgentBrowserInfo, error)
	Port() int
}

// ContainerTemplates is the project service's stack-preset port. It answers
// which templates exist (create-time validation) and how far one project's
// one-time provisioning has got (status payload).
type ContainerTemplates interface {
	// Has reports whether name is a known template.
	Has(name string) bool
	// DefaultName is the template assigned when a caller requests none.
	DefaultName() string
	// TemplateStatus reports the provisioning state of one project's
	// container, including where to sign in to what the template installed.
	TemplateStatus(ctx context.Context, project Meta) TemplateStatus
	// ResolveTemplateInputs validates raw create-request inputs against a
	// template's declaration and splits them into persistable values and
	// project secrets. It returns an error wrapping ErrInvalidTemplateInput
	// for anything the declaration refuses.
	ResolveTemplateInputs(
		template string,
		raw map[string]any,
		context TemplateInputContext,
	) (TemplateInputValues, error)
	// ForgetTemplateState drops any cached state for a deleted container.
	ForgetTemplateState(containerName string)
}

// ContainerDependencies groups the independently replaceable container
// capabilities used by Service. A nil capability preserves the behavior of a
// nil container manager for the operations that consume it.
type ContainerDependencies struct {
	Lifecycle   ContainerLifecycle
	Environment ContainerEnvironment
	Inspector   ContainerInspector
	Network     ContainerNetwork
	Listeners   ContainerListeners
	Browser     ContainerBrowser
	Policy      ContainerPolicy
	Admission   ContainerAdmission
	Templates   ContainerTemplates
	Database    ContainerDatabase
}
