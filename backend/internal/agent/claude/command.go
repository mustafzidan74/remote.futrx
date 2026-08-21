package claude

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

func (p *Provider) args(req agent.RunRequest) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
		"--dangerously-skip-permissions",
	}
	if model := sanitizeModel(req.Model); model != "" {
		args = append(args, "--model", model)
	}
	if effort := reasoningEffortArg(req.Preferences.ReasoningEffort); effort != "" {
		args = append(args, "--effort", effort)
	}
	if req.ResumeID != "" {
		args = append(args, "--resume", req.ResumeID)
		if req.Fork {
			args = append(args, "--fork-session")
		}
	}
	if req.EnableBrowser {
		args = append(args, "--mcp-config", browserMCPConfigPath)
	}
	return args
}

// appendMCPConfig adds one config file to the `--mcp-config` flag.
//
// The flag is variadic — the CLI documents it as taking space-separated files
// — so a second path has to join the existing group rather than open a second
// flag, which would replace the first and silently drop the Agent Browser's
// tools. Any existing group is therefore lifted out and re-emitted, once, at
// the end of the command line: that is the only position where a variadic
// option cannot swallow the value of a flag that follows it.
func appendMCPConfig(args []string, path string) []string {
	if path == "" {
		return args
	}
	kept := make([]string, 0, len(args)+2)
	configs := make([]string, 0, 2)
	for index := 0; index < len(args); index++ {
		if args[index] != "--mcp-config" {
			kept = append(kept, args[index])
			continue
		}
		for index+1 < len(args) && !strings.HasPrefix(args[index+1], "-") {
			index++
			configs = append(configs, args[index])
		}
	}
	configs = append(configs, path)
	kept = append(kept, "--mcp-config")
	return append(kept, configs...)
}

func sanitizeModel(model string) string {
	model = strings.TrimSpace(model)
	if idx := strings.Index(model, "["); idx > 0 {
		model = strings.TrimSpace(model[:idx])
	}
	return model
}

// reasoningEffortArg maps the conversation's reasoning effort onto the
// `claude --effort` levels (low, medium, high, xhigh, max, ultra). Unknown or empty
// values yield "" so the flag is omitted and the CLI picks its default.
func reasoningEffortArg(effort agent.ReasoningEffort) string {
	switch strings.ToLower(strings.TrimSpace(string(effort))) {
	case "low":
		return "low"
	case "medium":
		return "medium"
	case "high":
		return "high"
	case "xhigh":
		return "xhigh"
	case "max":
		return "max"
	case "ultra":
		return "ultra"
	default:
		return ""
	}
}

func (p *Provider) buildCmd(
	ctx context.Context,
	req agent.RunRequest,
	args []string,
	emit func(agent.Event),
) (*exec.Cmd, string, error) {
	cwd := req.Cwd
	if cwd == "" {
		cwd = os.Getenv("HOME")
		if cwd == "" {
			cwd = "/root"
		}
	}

	if req.ProjectID == "" || p.projects == nil {
		cmd := exec.CommandContext(ctx, "claude", args...)
		cmd.Dir = cwd
		// IS_SANDBOX=1 lets `claude --dangerously-skip-permissions` run under
		// uid 0. The box is single-user and the UI is auto-approve.
		cmd.Env = append(os.Environ(), "IS_SANDBOX=1")
		cmd.Env = agent.WithRuntimeEnvironment(cmd.Env, req.RuntimeEnv)
		// A third-party endpoint is applied last so its base URL and token
		// displace anything the host environment happens to carry.
		cmd.Env = agent.WithEndpointEnvironment(cmd.Env, req.Endpoint)
		cmd.Stdin = strings.NewReader(req.Prompt)
		return cmd, "", nil
	}

	project, err := p.projects.Get(ctx, serviceproject.ID(req.ProjectID))
	if err != nil {
		return nil, "", fmt.Errorf("project not found (%s): %w", req.ProjectID, err)
	}
	if project.ContainerName == "" {
		return nil, "", fmt.Errorf("project %s has no container - recreate the project", project.ID)
	}

	// The container can be deleted out-of-band (e.g. a workspace recycle onto a
	// new base image), leaving the cached Status stale. Always reconcile via
	// Start — it relaunches a missing instance from the base image and is a
	// no-op when already running; the cached Status only gates the indicator.
	if project.Status != serviceproject.StatusRunning {
		emitSystem(req, emit, "container_starting")
	}
	if _, err := p.projects.Start(ctx, project.ID); err != nil {
		return nil, "", fmt.Errorf("start container: %w", err)
	}

	if err := p.containerDeps.Validate(); err != nil {
		return nil, "", err
	}
	if !p.containerDeps.IsZero() {
		emitSystem(req, emit, "container_preparing")
		if err := p.containerDeps.CLI.Ensure(ctx, project.ContainerName, p.profile.CLI); err != nil {
			return nil, "", fmt.Errorf("install claude in container: %w", err)
		}
		// A run pointed at a third-party endpoint authenticates with the
		// operator's key for *that vendor*, published in the environment
		// below. Pushing the platform's Anthropic credentials for it would
		// put a first-party token in a container whose agent is about to talk
		// to somebody else, for no benefit at all.
		if req.Endpoint == nil {
			if err := p.containerDeps.Credentials.Ensure(ctx, project.ContainerName, p.profile.Credentials); err != nil {
				return nil, "", fmt.Errorf("seed claude auth in container: %w", err)
			}
		}
		if err := p.containerDeps.Workspace.EnsureAgentInstructions(ctx, project.ContainerName); err != nil {
			return nil, "", fmt.Errorf("push agent instructions to container: %w", err)
		}
		if err := p.containerDeps.Workspace.EnsureReplyPreferences(
			ctx, project.ContainerName, string(project.ID),
		); err != nil {
			// The reply preference is a nicety layered on top of the run, not
			// a precondition for it: a workspace whose AGENTS.md cannot be
			// rewritten still runs, just without the managed block.
			_ = err
		}
		if err := p.containerDeps.Workspace.EnsureSkillLinks(ctx, project.ContainerName); err != nil {
			// Symlink shim is best-effort: a stale /workspace/.codex shouldn't
			// block a claude run that doesn't depend on it.
			_ = err
		}
		if err := p.containerDeps.Browser.EnsureSkill(ctx, project.ContainerName); err != nil {
			// Best-effort migration for containers created before the skill.
			_ = err
		}
		if err := p.containerDeps.Browser.EnsureScript(ctx, project.ContainerName); err != nil {
			// Browser script + config are best-effort: their absence only matters
			// when the agent tries to drive Playwright. Don't fail the run.
			_ = err
		}
		if req.EnableBrowser {
			if err := p.containerDeps.Browser.EnsureMCP(ctx, project.ContainerName); err != nil {
				return nil, "", fmt.Errorf("provision browser MCP: %w", err)
			}
			if err := p.containerDeps.Browser.EnsureCore(ctx, project.ContainerName); err != nil {
				return nil, "", fmt.Errorf("start browser core: %w", err)
			}
		}
		if req.EnableScheduleTools {
			if err := p.containerDeps.ScheduleTools.Ensure(ctx, project.ContainerName); err != nil {
				return nil, "", fmt.Errorf("provision scheduled-task tools: %w", err)
			}
		}
		if err := p.containerDeps.Lifecycle.EnsureBootAutostart(ctx, project.ContainerName); err != nil {
			return nil, "", fmt.Errorf("set container boot.autostart: %w", err)
		}
		if p.containerDeps.MCP != nil {
			// External MCP tool servers are a capability, not a
			// precondition: a registry that cannot be materialized must not
			// cost the user their prompt. The run continues without them.
			mcpConfig, mcpErr := p.containerDeps.MCP.EnsureMCPServers(
				ctx, project.ContainerName, string(project.ID), string(agent.ProviderClaude),
			)
			if mcpErr != nil {
				log.Printf("claude[%s] materialize MCP servers: %v", req.ConversationID, mcpErr)
			} else if mcpConfig != "" {
				args = appendMCPConfig(args, mcpConfig)
			}
		}
	}

	lxcArgs := []string{
		"exec",
		"--cwd", "/workspace",
		"--env", "IS_SANDBOX=1",
		"--env", "HOME=/root",
	}
	if p.projects != nil {
		if secrets, err := p.projects.ListSecrets(ctx, project.ID); err == nil {
			for _, sec := range secrets {
				if _, backendIssued := req.RuntimeEnv[sec.Key]; backendIssued {
					continue
				}
				// A project secret must not be able to redirect a run the
				// platform pointed at a named endpoint, nor substitute its
				// own credential for the operator's.
				if agent.EndpointIssued(req.Endpoint, sec.Key) {
					continue
				}
				lxcArgs = append(lxcArgs, "--env", sec.Key+"="+sec.Value)
			}
		}
	}
	for _, entry := range agent.RuntimeEnvironment(req.RuntimeEnv) {
		lxcArgs = append(lxcArgs, "--env", entry)
	}
	// The endpoint's environment is the last word, and it is passed as
	// `--env` rather than written anywhere: nothing about this run survives it.
	for _, entry := range agent.EndpointEnvironment(req.Endpoint) {
		lxcArgs = append(lxcArgs, "--env", entry)
	}
	lxcArgs = append(lxcArgs, project.ContainerName, "--", "claude")
	lxcArgs = append(lxcArgs, args...)
	cmd := exec.CommandContext(ctx, "lxc", lxcArgs...)
	cmd.Stdin = strings.NewReader(req.Prompt)
	return cmd, project.ContainerName, nil
}

func emitSystem(req agent.RunRequest, emit func(agent.Event), subtype string) {
	emit(agent.Event{
		T:              time.Now().UnixMilli(),
		Type:           agent.EventSystem,
		Provider:       agent.ProviderClaude,
		ConversationID: req.ConversationID,
		Subtype:        subtype,
	})
}
