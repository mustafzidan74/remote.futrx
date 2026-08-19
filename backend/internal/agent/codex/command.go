package codex

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

func (p *Provider) args(req agent.RunRequest) []string {
	common := []string{
		"--json",
		"--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
	}
	if model := sanitizeModel(req.Model); model != "" {
		common = append(common, "--model", model)
	}
	if effort := reasoningEffortArg(req.Preferences.ReasoningEffort); effort != "" {
		common = append(common, "-c", "model_reasoning_effort="+effort)
	}
	if tier := serviceTierArg(req.Preferences.ServiceTier); tier != "" {
		common = append(common, "-c", "service_tier="+tier)
	}
	if req.EnableBrowser {
		common = append(common, browserMCPConfigArgs()...)
	}
	if req.ResumeID != "" {
		args := append([]string{"exec", "resume"}, common...)
		args = append(args, req.ResumeID, "-")
		return args
	}
	args := append([]string{"exec"}, common...)
	args = append(args, "-")
	return args
}

func browserMCPConfigArgs() []string {
	return []string{
		"-c", `mcp_servers.browser.command="npx"`,
		"-c", `mcp_servers.browser.args=["@playwright/mcp","--cdp-endpoint","http://127.0.0.1:9222","--caps=vision"]`,
	}
}

func sanitizeModel(model string) string {
	model = strings.TrimSpace(model)
	if idx := strings.Index(model, "["); idx > 0 {
		model = strings.TrimSpace(model[:idx])
	}
	return model
}

func reasoningEffortArg(effort agent.ReasoningEffort) string {
	// Valid `model_reasoning_effort` values for the codex CLI, per its own
	// reasoning.effort validation: none, minimal, low, medium, high, xhigh, max, ultra.
	switch strings.ToLower(strings.TrimSpace(string(effort))) {
	case "none":
		return "none"
	case "minimal":
		return "minimal"
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

// serviceTierArg maps the conversation's speed selection onto codex's
// `-c service_tier=` values we expose (default, priority, fast). Unsupported tiers are
// warned-and-omitted by codex itself, so we only forward the ones we surface.
func serviceTierArg(tier agent.ServiceTier) string {
	switch strings.ToLower(strings.TrimSpace(string(tier))) {
	case "default":
		return "default"
	case "priority":
		return "priority"
	case "fast":
		return "fast"
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
		if err := ensureHostSubscriptionAuth(); err != nil {
			return nil, "", err
		}
		cmd := exec.CommandContext(ctx, "codex", args...)
		cmd.Dir = cwd
		cmd.Env = agent.WithRuntimeEnvironment(codexEnv(os.Environ()), req.RuntimeEnv)
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
			return nil, "", fmt.Errorf("codex CLI unavailable in container: %w", err)
		}
		if err := p.ensureCredentials(ctx, project.ContainerName); err != nil {
			return nil, "", fmt.Errorf("seed codex auth in container: %w", err)
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
			return nil, "", fmt.Errorf("prepare workspace skill links: %w", err)
		}
		if err := p.containerDeps.Browser.EnsureSkill(ctx, project.ContainerName); err != nil {
			// Best-effort migration for containers created before the skill.
			_ = err
		}
		if err := p.containerDeps.Browser.EnsureScript(ctx, project.ContainerName); err != nil {
			// Browser script provisioning is best-effort: its absence only
			// matters when the agent tries to run scripts/browser.mjs.
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
			// codex reads /root/.codex/config.toml at startup, so there is no
			// flag to add — the managed region is enough. Failing to write it
			// must not cost the user their prompt.
			if _, mcpErr := p.containerDeps.MCP.EnsureMCPServers(
				ctx, project.ContainerName, string(project.ID), string(agent.ProviderCodex),
			); mcpErr != nil {
				log.Printf("codex[%s] materialize MCP servers: %v", req.ConversationID, mcpErr)
			}
		}
	}

	lxcArgs := []string{
		"exec",
		"--cwd", "/workspace",
		"--env", "HOME=/root",
		"--env", "CODEX_HOME=/root/.codex",
	}
	if p.projects != nil {
		if secrets, err := p.projects.ListSecrets(ctx, project.ID); err == nil {
			for _, sec := range secrets {
				if sec.Key == "OPENAI_API_KEY" {
					continue
				}
				if _, backendIssued := req.RuntimeEnv[sec.Key]; backendIssued {
					continue
				}
				lxcArgs = append(lxcArgs, "--env", sec.Key+"="+sec.Value)
			}
		}
	}
	lxcArgs = append(lxcArgs, "--env", "OPENAI_API_KEY=")
	for _, entry := range agent.RuntimeEnvironment(req.RuntimeEnv) {
		lxcArgs = append(lxcArgs, "--env", entry)
	}
	lxcArgs = append(lxcArgs, project.ContainerName, "--", "codex")
	lxcArgs = append(lxcArgs, args...)
	cmd := exec.CommandContext(ctx, "lxc", lxcArgs...)
	cmd.Stdin = strings.NewReader(req.Prompt)
	return cmd, project.ContainerName, nil
}

func ensureHostSubscriptionAuth() error {
	path := filepath.Join(hostCodexHome(), "auth.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil
	}
	mode, _ := raw["auth_mode"].(string)
	mode = strings.TrimSpace(strings.ToLower(mode))
	_, hasAPIKey := raw["OPENAI_API_KEY"]
	if mode == "apikey" || (mode == "" && hasAPIKey) {
		return ErrCodexAPIKeyAuth
	}
	return nil
}

func codexEnv(base []string) []string {
	out := make([]string, 0, len(base)+1)
	hasCodexHome := false
	home := ""
	for _, env := range base {
		if strings.HasPrefix(env, "OPENAI_API_KEY=") {
			continue
		}
		if strings.HasPrefix(env, "CODEX_HOME=") {
			hasCodexHome = true
		}
		if strings.HasPrefix(env, "HOME=") {
			home = strings.TrimPrefix(env, "HOME=")
		}
		out = append(out, env)
	}
	if hasCodexHome {
		return out
	}
	if home != "" {
		return append(out, "CODEX_HOME="+home+"/.codex")
	}
	return out
}

func hostCodexHome() string {
	if v := os.Getenv("CODEX_HOME"); v != "" {
		return v
	}
	if home := os.Getenv("HOME"); home != "" {
		return filepath.Join(home, ".codex")
	}
	return "/root/.codex"
}

func emitSystem(req agent.RunRequest, emit func(agent.Event), subtype string) {
	emit(agent.Event{
		T:              time.Now().UnixMilli(),
		Type:           agent.EventSystem,
		Provider:       agent.ProviderCodex,
		ConversationID: req.ConversationID,
		Subtype:        subtype,
	})
}
