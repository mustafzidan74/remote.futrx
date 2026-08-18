// Package githubcli runs `git` and `gh` inside a project's container.
//
// The reason this lives in the container rather than on the host is the
// credential. `gh` reads `GITHUB_TOKEN` from its environment, and LXD has
// already put the project's secrets (and the vault entries scoped to it) into
// the container's persistent `environment.*` config — so every `lxc exec`
// inherits the token without the host process ever reading it, holding it, or
// being able to print it into an error message.
//
// The adapter builds argv and nothing else. It never composes a shell string,
// so the arguments it is handed — a branch name, a commit message, an issue
// title — become argv elements of the `lxc` process and are inert to every
// layer below.
package githubcli

import (
	"context"
	"errors"
	"strings"

	"github.com/futrx-com/remote.futrx.com/internal/integration/containers/command"
	servicegithub "github.com/futrx-com/remote.futrx.com/internal/service/github"
)

// workspacePath is where the project's files are bind-mounted in the guest.
// Every git and gh invocation runs there, because that is the repository.
const workspacePath = "/workspace"

// Adapter satisfies the github service's CLI port.
type Adapter struct {
	runner command.Runner
}

var _ servicegithub.CLI = (*Adapter)(nil)

// NewAdapter returns an adapter backed by runner.
func NewAdapter(runner command.Runner) *Adapter {
	return &Adapter{runner: runner}
}

func (a *Adapter) Available() bool {
	return a != nil && a.runner != nil && a.runner.Available()
}

// Run executes one command in the container's /workspace and returns its
// combined output. A non-nil error means a non-zero exit; the output comes
// back either way so the caller can classify the failure.
func (a *Adapter) Run(ctx context.Context, cmd servicegithub.Command) (string, error) {
	if !a.Available() {
		return "", command.ErrUnavailable
	}
	if strings.TrimSpace(cmd.ContainerName) == "" || len(cmd.Argv) == 0 {
		return "", errors.New("githubcli: container name and argv are required")
	}
	args := ExecArgs(cmd)
	timeout := cmd.Timeout
	if timeout <= 0 {
		timeout = servicegithub.QuickTimeout
	}
	if cmd.Stdin != "" {
		runCtx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		return a.runner.RunStdin(runCtx, strings.NewReader(cmd.Stdin), args...)
	}
	return command.RunWithTimeout(ctx, a.runner, timeout, args...)
}

// ExecArgs is the exact `lxc exec` invocation, split out so a test can assert
// the command line without a container.
//
// The three environment entries are not optional decoration:
//
//   - HOME=/root is what `gh` resolves its config directory from, and
//     `lxc exec` does not source a login shell, so without it gh looks in `/`.
//   - GH_PROMPT_DISABLED stops gh from blocking on an interactive prompt in a
//     session with no terminal, which would otherwise hang until the timeout.
//   - GH_NO_UPDATE_NOTIFIER keeps gh's version check off the critical path and
//     out of the output this platform parses.
//
// The credential itself is deliberately absent: it is already in the
// container's own environment.
func ExecArgs(cmd servicegithub.Command) []string {
	args := []string{
		"exec", cmd.ContainerName,
		"--cwd", workspacePath,
		"--env", "HOME=/root",
		"--env", "GH_PROMPT_DISABLED=1",
		"--env", "GH_NO_UPDATE_NOTIFIER=1",
		"--",
	}
	return append(args, cmd.Argv...)
}
