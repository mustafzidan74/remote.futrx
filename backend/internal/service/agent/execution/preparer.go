// Package execution owns provider-neutral project preparation for agent runs.
// Provider adapters declare the small policy differences and retain their own
// host commands, CLI arguments, stdin strategy, and output protocol.
package execution

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
	"github.com/futrx-com/remote.futrx.com/internal/agent/provisioning"
)

type Options struct {
	// Provider identifies system events and supplies default error wording.
	Provider agent.ProviderID
	// Profile is the exact provisioning policy already validated by the module
	// factory. New clones it before retaining any slices or template bytes.
	Profile provisioning.Profile
	// CLIErrorOperation and CredentialErrorOperation override the provider-based
	// default prefixes when compatibility requires established user-facing text.
	CLIErrorOperation        string
	CredentialErrorOperation string
	// BeforeCredentials receives an isolated profile snapshot for provider-specific
	// validation immediately before the generic credential synchronizer is invoked.
	BeforeCredentials func(provisioning.Profile) error
	// SkillLinksRequired turns the otherwise best-effort compatibility-link
	// migration into a fatal run prerequisite.
	SkillLinksRequired bool
	// BrowserAssets migrates the shared browser skill and script on every
	// prepared run. Both operations remain best effort.
	BrowserAssets bool
	// BrowserMCPRuntime provisions MCP configuration and starts browser core when
	// the already policy-gated run request enables Browser tools.
	BrowserMCPRuntime bool
}

// gitCredentialProvisioner is the optional workspace step that lets git use
// the container's GitHub token.
type gitCredentialProvisioner interface {
	EnsureGitCredentialHelper(ctx context.Context, containerName string) error
}

type Preparer struct {
	projects   agent.ProjectResolver
	containers provisioning.ContainerDependencies
	options    Options
}

// New returns nil when no project resolver is available, allowing host-only
// runtime composition and focused tests without a project service.
func New(
	projects agent.ProjectResolver,
	containers provisioning.ContainerDependencies,
	options Options,
) agent.ProjectPreparer {
	if projects == nil {
		return nil
	}
	options.Profile = options.Profile.Clone()
	return &Preparer{projects: projects, containers: containers, options: options}
}

func (p *Preparer) Prepare(
	ctx context.Context,
	request agent.ProjectPreparationRequest,
	emit func(agent.Event),
) (agent.PreparedProject, error) {
	project, err := p.projects.Get(ctx, request.ProjectID)
	if err != nil {
		return agent.PreparedProject{}, fmt.Errorf("project not found (%s): %w", request.ProjectID, err)
	}
	if project.ContainerName == "" {
		return agent.PreparedProject{}, fmt.Errorf("project %s has no container - recreate the project", project.ID)
	}
	if project.Status != agent.ProjectStatusRunning {
		p.emitSystem(request, emit, "container_starting")
	}
	if _, err := p.projects.Start(ctx, project.ID); err != nil {
		return agent.PreparedProject{}, fmt.Errorf("start container: %w", err)
	}
	if err := p.containers.Validate(); err != nil {
		return agent.PreparedProject{}, err
	}
	if !p.containers.IsZero() {
		p.emitSystem(request, emit, "container_preparing")
		mcpConfigPath, err := p.prepareContainer(ctx, request, project)
		if err != nil {
			return agent.PreparedProject{}, err
		}
		prepared := agent.PreparedProject{ID: project.ID, ContainerName: project.ContainerName}
		prepared.MCPConfigPath = mcpConfigPath
		prepared.Secrets = p.projectSecrets(ctx, request, project.ID)
		return prepared, nil
	}

	prepared := agent.PreparedProject{ID: project.ID, ContainerName: project.ContainerName}
	prepared.Secrets = p.projectSecrets(ctx, request, project.ID)
	return prepared, nil
}

// projectSecrets lists the project's secrets minus any key a third-party
// endpoint issues: a project secret must not be able to redirect a run the
// platform pointed at a named endpoint, nor substitute its own credential for
// the operator's.
func (p *Preparer) projectSecrets(
	ctx context.Context,
	request agent.ProjectPreparationRequest,
	projectID agent.ProjectID,
) []agent.ProjectSecret {
	secrets, err := p.projects.ListSecrets(ctx, projectID)
	if err != nil {
		return nil
	}
	if request.Endpoint == nil {
		return secrets
	}
	kept := make([]agent.ProjectSecret, 0, len(secrets))
	for _, secret := range secrets {
		if !agent.EndpointIssued(request.Endpoint, secret.Key) {
			kept = append(kept, secret)
		}
	}
	return kept
}

func (p *Preparer) prepareContainer(
	ctx context.Context,
	request agent.ProjectPreparationRequest,
	project agent.Project,
) (string, error) {
	containerName := project.ContainerName
	if err := p.containers.CLI.Ensure(ctx, containerName, p.options.Profile.CLI); err != nil {
		return "", fmt.Errorf("%s: %w", p.cliErrorOperation(), err)
	}
	// A run pointed at a third-party endpoint authenticates with the
	// operator's key for that vendor. Seeding the platform's first-party
	// credentials would put a token in a container whose agent is about to
	// talk to somebody else, for no benefit at all.
	if !p.options.Profile.Credentials.Empty() && request.Endpoint == nil {
		if p.options.BeforeCredentials != nil {
			if err := p.options.BeforeCredentials(p.options.Profile.Clone()); err != nil {
				return "", fmt.Errorf("%s: %w", p.credentialErrorOperation(), err)
			}
		}
		if err := p.containers.Credentials.Ensure(ctx, containerName, p.options.Profile.Credentials); err != nil {
			return "", fmt.Errorf("%s: %w", p.credentialErrorOperation(), err)
		}
	}
	if err := p.containers.Workspace.EnsureAgentInstructions(ctx, containerName); err != nil {
		return "", fmt.Errorf("push agent instructions to container: %w", err)
	}
	// The reply preference is a nicety layered on top of the run, not a
	// precondition for it: a workspace whose AGENTS.md cannot be rewritten
	// still runs, just without the managed block.
	_ = p.containers.Workspace.EnsureReplyPreferences(ctx, containerName, string(project.ID))
	// Same footing: a git that cannot find the GitHub token is a worse run,
	// not a reason to refuse one. Optional, so a workspace provisioner that
	// predates it (and every test double) still satisfies the interface.
	if git, ok := p.containers.Workspace.(gitCredentialProvisioner); ok {
		_ = git.EnsureGitCredentialHelper(ctx, containerName)
	}
	if err := p.containers.RuntimeAssets.Ensure(ctx, containerName, p.options.Profile.RuntimeAssets); err != nil {
		return "", fmt.Errorf("push agent runtime assets to container: %w", err)
	}
	if err := p.containers.Workspace.EnsureSkillLinks(ctx, containerName); err != nil && p.options.SkillLinksRequired {
		return "", fmt.Errorf("prepare workspace skill links: %w", err)
	}
	if p.options.BrowserAssets {
		_ = p.containers.Browser.EnsureSkill(ctx, containerName)
		_ = p.containers.Browser.EnsureScript(ctx, containerName)
	}
	if p.options.BrowserMCPRuntime && request.EnableBrowser {
		if err := p.containers.Browser.EnsureMCP(ctx, containerName); err != nil {
			return "", fmt.Errorf("provision browser MCP: %w", err)
		}
		if err := p.containers.Browser.EnsureCore(ctx, containerName); err != nil {
			return "", fmt.Errorf("start browser core: %w", err)
		}
	}
	if request.EnableScheduleTools {
		if err := p.containers.ScheduleTools.Ensure(ctx, containerName); err != nil {
			return "", fmt.Errorf("provision scheduled-task tools: %w", err)
		}
	}
	if err := p.containers.Lifecycle.EnsureBootAutostart(ctx, containerName); err != nil {
		return "", fmt.Errorf("set container boot.autostart: %w", err)
	}
	mcpConfigPath := ""
	if p.containers.MCP != nil {
		// External MCP tool servers are a capability, not a precondition: a
		// registry that cannot be materialized must not cost the user their
		// prompt. The run continues without them.
		path, err := p.containers.MCP.EnsureMCPServers(ctx, containerName, string(project.ID), string(p.options.Provider))
		if err != nil {
			log.Printf("%s[%s] materialize MCP servers: %v", p.options.Provider, request.ConversationID, err)
		} else {
			mcpConfigPath = path
		}
	}
	return mcpConfigPath, nil
}

func (p *Preparer) cliErrorOperation() string {
	if p.options.CLIErrorOperation != "" {
		return p.options.CLIErrorOperation
	}
	return "install " + string(p.options.Provider) + " in container"
}

func (p *Preparer) credentialErrorOperation() string {
	if p.options.CredentialErrorOperation != "" {
		return p.options.CredentialErrorOperation
	}
	return "seed " + string(p.options.Provider) + " auth in container"
}

func (p *Preparer) emitSystem(
	request agent.ProjectPreparationRequest,
	emit func(agent.Event),
	subtype string,
) {
	if emit == nil {
		return
	}
	emit(agent.Event{
		T:              time.Now().UnixMilli(),
		Type:           agent.EventSystem,
		Provider:       p.options.Provider,
		ConversationID: request.ConversationID,
		Subtype:        subtype,
	})
}
