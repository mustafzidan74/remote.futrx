package claude

import (
	"context"
	"os"
	"os/exec"
	"strings"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
	agentruntime "github.com/futrx-com/remote.futrx.com/internal/integration/agents/runtime"
)

func (p *Provider) args(req agent.RunRequest) []string {
	args := []string{
		"-p",
		"--output-format", "stream-json",
		"--include-partial-messages",
		"--verbose",
	}
	if req.Mode == agent.RunModePlan {
		args = append(args, "--permission-mode", string(agent.RunModePlan))
	} else {
		args = append(args, "--dangerously-skip-permissions")
	}
	if model := normalizeModelSelection(req.Model); model != "" {
		args = append(args, "--model", model)
	}
	if effort := reasoningEffortArg(req.Preferences.ReasoningEffort); effort != "" {
		args = append(args, "--effort", effort)
	}
	if req.Preferences.ServiceTier == agent.ServiceTier(fastServiceTier) {
		args = append(args, "--settings", `{"fastMode":true}`)
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

// reasoningEffortArg syntax-checks the selected or saved value. Empty or
// malformed values omit the flag so the CLI picks a default.
func reasoningEffortArg(effort agent.ReasoningEffort) string {
	return agent.NormalizeCapabilityValue(string(effort))
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

	if req.ProjectID == "" || p.projectPreparer == nil {
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

	project, err := p.projectPreparer.Prepare(ctx, agent.ProjectPreparationRequest{
		ProjectID:           agent.ProjectID(req.ProjectID),
		ConversationID:      req.ConversationID,
		EnableBrowser:       req.EnableBrowser,
		EnableScheduleTools: req.EnableScheduleTools,
		Endpoint:            req.Endpoint,
	}, emit)
	if err != nil {
		return nil, "", err
	}
	args = appendMCPConfig(args, project.MCPConfigPath)
	cmd := agentruntime.BuildContainerCommand(ctx, agentruntime.ContainerCommandSpec{
		ContainerName:      project.ContainerName,
		PrefixEnvironment:  []string{"IS_SANDBOX=1", "HOME=/root"},
		Secrets:            project.Secrets,
		RuntimeEnvironment: req.RuntimeEnv,
		// The endpoint's environment is the last word, and it is passed as
		// `--env` rather than written anywhere: nothing about this run survives it.
		FinalEnvironment: agent.EndpointEnvironment(req.Endpoint),
		Binary:           p.profile.CLI.Binary,
		Arguments:        args,
	})
	cmd.Stdin = strings.NewReader(req.Prompt)
	return cmd, project.ContainerName, nil
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
