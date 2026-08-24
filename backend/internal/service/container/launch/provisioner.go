// Package launch coordinates best-effort capabilities after a new container
// is launched.
package launch

import "context"

type RegisteredCredentialEnsurer interface {
	EnsureRegistered(ctx context.Context, containerName string) error
}

type BrowserProvisioner interface {
	EnsureScript(ctx context.Context, containerName string) error
	EnsureSkill(ctx context.Context, containerName string) error
	EnsureNesting(ctx context.Context, containerName string) error
}

type WorkspaceProvisioner interface {
	EnsureSkillLinks(ctx context.Context, containerName string) error
}

type CodeServerProvisioner interface {
	Ensure(ctx context.Context, containerName, displayName string) error
}

// FilePreviewProvisioner installs the read-only static server that makes a
// file an agent wrote viewable without a dev server.
type FilePreviewProvisioner interface {
	Ensure(ctx context.Context, containerName string) error
}

type ScheduleToolsProvisioner interface {
	Ensure(ctx context.Context, containerName string) error
}

// Provisioner applies launch-time capabilities in their stable order. Every
// step is deliberately best-effort so one unavailable capability cannot block
// the remaining migrations or the newly launched container.
type Provisioner struct {
	credentials   RegisteredCredentialEnsurer
	workspace     WorkspaceProvisioner
	browser       BrowserProvisioner
	codeServer    CodeServerProvisioner
	filePreview   FilePreviewProvisioner
	scheduleTools ScheduleToolsProvisioner
}

// WithFilePreview attaches the workspace file preview. Optional so a
// deployment that has not migrated its containers keeps launching.
func (p *Provisioner) WithFilePreview(preview FilePreviewProvisioner) *Provisioner {
	p.filePreview = preview
	return p
}

func NewProvisioner(
	credentials RegisteredCredentialEnsurer,
	workspace WorkspaceProvisioner,
	browser BrowserProvisioner,
	codeServer CodeServerProvisioner,
	scheduleTools ...ScheduleToolsProvisioner,
) *Provisioner {
	var scheduled ScheduleToolsProvisioner
	if len(scheduleTools) > 0 {
		scheduled = scheduleTools[0]
	}
	return &Provisioner{
		credentials:   credentials,
		workspace:     workspace,
		browser:       browser,
		codeServer:    codeServer,
		scheduleTools: scheduled,
	}
}

// Provision applies launch-time capabilities in their stable order.
func (p *Provisioner) Provision(ctx context.Context, containerName, displayName string) {
	p.ProvisionCredentials(ctx, containerName)
	_ = p.workspace.EnsureSkillLinks(ctx, containerName)
	_ = p.browser.EnsureScript(ctx, containerName)
	_ = p.browser.EnsureSkill(ctx, containerName)
	_ = p.browser.EnsureNesting(ctx, containerName)
	if p.scheduleTools != nil {
		_ = p.scheduleTools.Ensure(ctx, containerName)
	}
	_ = p.codeServer.Ensure(ctx, containerName, displayName)
	if p.filePreview != nil {
		_ = p.filePreview.Ensure(ctx, containerName)
	}
}

// ProvisionCredentials seeds agent credentials alone.
//
// It is separate from Provision because the two answer different questions.
// The rest of provisioning migrates a container's contents and only needs to
// run when the container is new or its configuration changed; credentials
// change *outside* any container, whenever an operator signs an agent in, and
// a container that was already running when that happened would otherwise
// never learn about it. Seeding on every start is what makes "sign in once"
// true for projects that already exist.
//
// It is cheap to repeat: each file is pushed only when the host copy is newer
// than the container's.
func (p *Provisioner) ProvisionCredentials(ctx context.Context, containerName string) {
	if p.credentials == nil {
		return
	}
	_ = p.credentials.EnsureRegistered(ctx, containerName)
}
