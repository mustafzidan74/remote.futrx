package kimi

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// containerKimiHome is the KIMI_CODE_HOME inside a project container — where
// kimi-code reads its OAuth credentials, config, and sessions.
const containerKimiHome = "/root/.kimi-code"

func (p *Provider) args(req agent.RunRequest) []string {
	// kimi-code takes the prompt as a positional argument (NOT stdin). Print
	// mode (`-p`) forces auto-approval, so we must not pass -y/--auto/--plan.
	args := []string{"-p", req.Prompt, "--output-format", "stream-json"}
	if model := sanitizeModel(req.Model); model != "" {
		args = append(args, "--model", model)
	}
	if req.ResumeID != "" {
		args = append(args, "--session", req.ResumeID)
	}
	return args
}

func sanitizeModel(model string) string {
	model = strings.TrimSpace(model)
	if idx := strings.Index(model, "["); idx > 0 {
		model = strings.TrimSpace(model[:idx])
	}
	return model
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
		cmd := exec.CommandContext(ctx, "kimi", args...)
		cmd.Dir = cwd
		cmd.Env = append(os.Environ(), "KIMI_CODE_HOME="+hostKimiHome())
		cmd.Env = agent.WithRuntimeEnvironment(cmd.Env, req.RuntimeEnv)
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
			return nil, "", fmt.Errorf("install kimi in container: %w", err)
		}
		if err := p.containerDeps.Credentials.Ensure(ctx, project.ContainerName, p.profile.Credentials); err != nil {
			return nil, "", fmt.Errorf("seed kimi auth in container: %w", err)
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
			// Best-effort: a stale skill shim shouldn't block a kimi run.
			_ = err
		}
		if err := p.containerDeps.Browser.EnsureSkill(ctx, project.ContainerName); err != nil {
			// Best-effort migration for containers created before the skill.
			_ = err
		}
		if err := p.containerDeps.Browser.EnsureScript(ctx, project.ContainerName); err != nil {
			// Best-effort: only matters if the agent runs scripts/browser.mjs.
			_ = err
		}
		if req.EnableScheduleTools {
			if err := p.containerDeps.ScheduleTools.Ensure(ctx, project.ContainerName); err != nil {
				return nil, "", fmt.Errorf("provision scheduled-task tools: %w", err)
			}
		}
		if err := p.containerDeps.Lifecycle.EnsureBootAutostart(ctx, project.ContainerName); err != nil {
			return nil, "", fmt.Errorf("set container boot.autostart: %w", err)
		}
	}

	lxcArgs := []string{
		"exec",
		"--cwd", "/workspace",
		"--env", "HOME=/root",
		"--env", "KIMI_CODE_HOME=" + containerKimiHome,
	}
	if p.projects != nil {
		if secrets, err := p.projects.ListSecrets(ctx, project.ID); err == nil {
			for _, sec := range secrets {
				if _, backendIssued := req.RuntimeEnv[sec.Key]; backendIssued {
					continue
				}
				lxcArgs = append(lxcArgs, "--env", sec.Key+"="+sec.Value)
			}
		}
	}
	for _, entry := range agent.RuntimeEnvironment(req.RuntimeEnv) {
		lxcArgs = append(lxcArgs, "--env", entry)
	}
	lxcArgs = append(lxcArgs, project.ContainerName, "--", "kimi")
	lxcArgs = append(lxcArgs, args...)
	cmd := exec.CommandContext(ctx, "lxc", lxcArgs...)
	return cmd, project.ContainerName, nil
}

func hostKimiHome() string {
	if v := os.Getenv("KIMI_CODE_HOME"); v != "" {
		return v
	}
	if home := os.Getenv("HOME"); home != "" {
		return home + "/.kimi-code"
	}
	return "/root/.kimi-code"
}

func emitSystem(req agent.RunRequest, emit func(agent.Event), subtype string) {
	emit(agent.Event{
		T:              time.Now().UnixMilli(),
		Type:           agent.EventSystem,
		Provider:       agent.ProviderKimi,
		ConversationID: req.ConversationID,
		Subtype:        subtype,
	})
}
