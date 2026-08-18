package githubcli

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	servicegithub "github.com/futrx-com/remote.futrx.com/internal/service/github"
)

type stubRunner struct {
	available bool
	args      []string
	stdin     string
	out       string
	err       error
	deadline  bool
}

func (r *stubRunner) Available() bool { return r.available }

func (r *stubRunner) Run(ctx context.Context, args ...string) (string, error) {
	r.args = args
	_, r.deadline = ctx.Deadline()
	return r.out, r.err
}

func (r *stubRunner) RunStdin(ctx context.Context, stdin io.Reader, args ...string) (string, error) {
	r.args = args
	_, r.deadline = ctx.Deadline()
	if stdin != nil {
		body, _ := io.ReadAll(stdin)
		r.stdin = string(body)
	}
	return r.out, r.err
}

func TestExecArgs(t *testing.T) {
	tests := []struct {
		name string
		cmd  servicegithub.Command
		want []string
	}{
		{
			name: "a git command runs in the workspace",
			cmd:  servicegithub.Command{ContainerName: "demo", Argv: []string{"git", "status", "-sb"}},
			want: []string{
				"exec", "demo", "--cwd", "/workspace",
				"--env", "HOME=/root",
				"--env", "GH_PROMPT_DISABLED=1",
				"--env", "GH_NO_UPDATE_NOTIFIER=1",
				"--", "git", "status", "-sb",
			},
		},
		{
			// A commit message with spaces, quotes and shell metacharacters
			// stays exactly one argv element, because no shell is involved.
			name: "an awkward argument stays one element",
			cmd: servicegithub.Command{
				ContainerName: "demo",
				Argv:          []string{"git", "commit", "-m", `"; rm -rf / #`},
			},
			want: []string{
				"exec", "demo", "--cwd", "/workspace",
				"--env", "HOME=/root",
				"--env", "GH_PROMPT_DISABLED=1",
				"--env", "GH_NO_UPDATE_NOTIFIER=1",
				"--", "git", "commit", "-m", `"; rm -rf / #`,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ExecArgs(test.cmd)
			if len(got) != len(test.want) {
				t.Fatalf("ExecArgs = %v, want %v", got, test.want)
			}
			for index := range got {
				if got[index] != test.want[index] {
					t.Fatalf("ExecArgs[%d] = %q, want %q", index, got[index], test.want[index])
				}
			}
		})
	}
}

func TestExecArgsNeverCarriesACredential(t *testing.T) {
	// The token lives in the container's own environment; the host must never
	// put it on a command line, where it would be visible in `ps` output.
	args := ExecArgs(servicegithub.Command{ContainerName: "demo", Argv: []string{"gh", "pr", "list"}})
	joined := strings.ToLower(strings.Join(args, " "))
	for _, forbidden := range []string{"token", "gh_token", "password"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("argv %v must not mention %q", args, forbidden)
		}
	}
}

func TestRunGuards(t *testing.T) {
	tests := []struct {
		name      string
		available bool
		cmd       servicegithub.Command
		wantErr   bool
	}{
		{
			name: "no container runtime", available: false,
			cmd:     servicegithub.Command{ContainerName: "demo", Argv: []string{"git", "status"}},
			wantErr: true,
		},
		{
			name: "no container name", available: true,
			cmd:     servicegithub.Command{Argv: []string{"git", "status"}},
			wantErr: true,
		},
		{
			name: "no argv", available: true,
			cmd:     servicegithub.Command{ContainerName: "demo"},
			wantErr: true,
		},
		{
			name: "well formed", available: true,
			cmd: servicegithub.Command{ContainerName: "demo", Argv: []string{"git", "status"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &stubRunner{available: test.available}
			_, err := NewAdapter(runner).Run(context.Background(), test.cmd)
			if (err != nil) != test.wantErr {
				t.Fatalf("Run returned %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}

func TestRunPipesStdinAndBoundsTheDeadline(t *testing.T) {
	runner := &stubRunner{available: true, out: "https://github.com/o/r/pull/1"}
	adapter := NewAdapter(runner)

	out, err := adapter.Run(context.Background(), servicegithub.Command{
		ContainerName: "demo",
		Argv:          []string{"gh", "pr", "create", "--body-file", "-"},
		Stdin:         "a multi-line\nbody",
		Timeout:       5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run returned %v", err)
	}
	if out != runner.out {
		t.Fatalf("out = %q, want %q", out, runner.out)
	}
	if runner.stdin != "a multi-line\nbody" {
		t.Fatalf("stdin = %q, want the body piped verbatim", runner.stdin)
	}
	if !runner.deadline {
		t.Fatal("every invocation must carry a deadline")
	}
}

func TestRunReturnsOutputAlongsideAFailure(t *testing.T) {
	// The caller classifies failures by reading the output, so a non-zero
	// exit must not swallow it.
	runner := &stubRunner{available: true, out: "gh: Bad credentials", err: errors.New("exit 1")}
	out, err := NewAdapter(runner).Run(context.Background(), servicegithub.Command{
		ContainerName: "demo", Argv: []string{"gh", "repo", "view", "o/r"},
	})
	if err == nil {
		t.Fatal("expected the failure to surface")
	}
	if out != "gh: Bad credentials" {
		t.Fatalf("out = %q, want the command's output", out)
	}
}
