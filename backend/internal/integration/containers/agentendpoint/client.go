// Package agentendpoint runs the third-party endpoint Test probe: one
// two-word prompt through the real agent CLI, configured for one endpoint,
// inside a project's container.
//
// It runs the *real* binary on purpose. A probe that spoke HTTP to the base
// URL itself would answer a different question — whether the URL is reachable
// — and would miss the thing an operator actually needs to know: whether this
// CLI, at the version this platform pins, accepts this vendor's compatibility
// mode and gets an answer back.
//
// Nothing here logs the environment it publishes. The resolved key travels as
// `lxc exec --env`, exactly as project secrets and the scheduled-task grant
// already do, so it never lands in a script this platform writes; the raw
// output goes back to the service, which masks the value before it reaches an
// API response.
package agentendpoint

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/integration/containers/command"
	serviceendpoints "github.com/futrx-com/remote.futrx.com/internal/service/agentendpoints"
	"github.com/futrx-com/remote.futrx.com/internal/shared/output"
)

const (
	// probeTimeout bounds one probe. A third-party endpoint under load can be
	// slow to first token, so this is more generous than the MCP handshake's
	// budget but still short of a real turn.
	probeTimeout = 120 * time.Second

	// probeOutputLimit bounds what a misbehaving endpoint can push back
	// through the Test action. The service truncates again after masking.
	probeOutputLimit = 8000
)

// Client runs probes through the container runtime CLI.
type Client struct {
	runner command.Runner
}

// NewClient returns a probe runner backed by runner.
func NewClient(runner command.Runner) *Client {
	return &Client{runner: runner}
}

// Probe launches the CLI named by the probe inside containerName and returns
// its combined output. A non-nil error means the CLI exited non-zero, which
// is itself an answer the operator wants to see.
func (c *Client) Probe(
	ctx context.Context,
	containerName string,
	probe serviceendpoints.Probe,
) (string, error) {
	if !c.runner.Available() {
		return "", command.ErrUnavailable
	}
	if strings.TrimSpace(containerName) == "" {
		return "", fmt.Errorf("container name required")
	}

	args := []string{"exec", containerName, "--cwd", "/workspace", "--env", "HOME=/root"}
	switch probe.CLI {
	case serviceendpoints.CLIClaude:
		// The same flag the run path sets: it is what lets
		// `--dangerously-skip-permissions` run as uid 0 in the container.
		args = append(args, "--env", "IS_SANDBOX=1")
	case serviceendpoints.CLICodex:
		args = append(args, "--env", "CODEX_HOME=/root/.codex")
	default:
		return "", serviceendpoints.ErrInvalidCLI
	}
	for _, name := range sortedKeys(probe.Env) {
		args = append(args, "--env", name+"="+probe.Env[name])
	}

	cli, cliArgs, err := commandLine(probe)
	if err != nil {
		return "", err
	}
	args = append(args, "--", cli)
	args = append(args, cliArgs...)

	probeCtx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, err := c.runner.RunStdin(probeCtx, strings.NewReader(probe.Prompt), args...)
	return output.TruncateTail(strings.TrimSpace(out), probeOutputLimit), err
}

// commandLine renders the CLI invocation for one probe.
//
// Both are the headless, single-shot forms this platform's run path already
// uses, minus the streaming-JSON flags: a probe wants human-readable text,
// not an event stream to parse.
func commandLine(probe serviceendpoints.Probe) (string, []string, error) {
	switch probe.CLI {
	case serviceendpoints.CLIClaude:
		args := []string{"-p", "--dangerously-skip-permissions"}
		if model := strings.TrimSpace(probe.Model); model != "" {
			args = append(args, "--model", model)
		}
		return "claude", args, nil

	case serviceendpoints.CLICodex:
		args := []string{"exec", "--skip-git-repo-check", "--dangerously-bypass-approvals-and-sandbox"}
		if model := strings.TrimSpace(probe.Model); model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, probe.Args...)
		// The trailing "-" makes codex read the prompt from stdin, which is
		// how the run path feeds it too.
		return "codex", append(args, "-"), nil

	default:
		return "", nil, serviceendpoints.ErrInvalidCLI
	}
}

func sortedKeys(values map[string]string) []string {
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
