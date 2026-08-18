// Package schedulework runs the bounded in-container probes scheduled tasks
// need: the shell command behind a commandExitCode gate, and the git captures
// that let run history report which files a run touched.
package schedulework

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/integration/containers/command"
)

// WorkspaceDir is the path every project workspace is mounted at inside its
// container. Probes are always relative to it.
const WorkspaceDir = "/workspace"

// gitTimeout bounds one git capture. It is short on purpose: the scheduler
// loop waits on the "before" capture of every run.
const gitTimeout = 10 * time.Second

// maxProbeOutput caps how much of a probe's output travels back into a gate
// reason or a history record.
const maxProbeOutput = 8 << 10

// exitMarker is how the exit code of a gate command is smuggled out of a
// runner whose only channel is combined output.
const exitMarker = "__REMOTE_SCHEDULE_EXIT:"

var exitPattern = regexp.MustCompile(exitMarker + `(\d+)`)

// Adapter runs commands inside project containers through the shared runner.
type Adapter struct {
	runner command.Runner
}

func NewAdapter(runner command.Runner) *Adapter {
	return &Adapter{runner: runner}
}

// Available reports whether the container runtime is usable on this host.
func (a *Adapter) Available() bool {
	return a != nil && a.runner != nil && a.runner.Available()
}

// RunCommand executes one shell command with /workspace as the working
// directory and returns its combined output and exit code. The command runs
// as-is inside `sh -c`, so it is only ever driven by an operator-authored
// task definition, never by request input.
func (a *Adapter) RunCommand(
	ctx context.Context,
	containerName string,
	shellCommand string,
	timeout time.Duration,
) (string, int, error) {
	if !a.Available() {
		return "", 0, command.ErrUnavailable
	}
	if strings.TrimSpace(containerName) == "" {
		return "", 0, fmt.Errorf("schedule probe: container name is required")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	// The exit code is echoed as a marker line because the runner surfaces a
	// non-zero exit as a transport error, which a gate must be able to tell
	// apart from "the container is unreachable".
	script := fmt.Sprintf(
		"cd %s 2>/dev/null || exit 127; { %s\n}; printf '\\n%s%%d\\n' \"$?\"",
		WorkspaceDir, shellCommand, exitMarker,
	)
	output, err := command.RunWithTimeout(
		ctx, a.runner, timeout, "exec", containerName, "--", "sh", "-c", script,
	)
	code, found := parseExitCode(output)
	if !found {
		if err != nil {
			return truncate(output), 0, fmt.Errorf("schedule probe: %w", err)
		}
		return truncate(output), 0, fmt.Errorf("schedule probe: no exit code reported")
	}
	return truncate(stripExitMarker(output)), code, nil
}

// GitStatus reports whether /workspace is a git checkout and, when it is, its
// HEAD, porcelain status, and unstaged diff stat. Every failure degrades to
// "not a repository" rather than an error: run history is a report, and a
// missing report must never fail a run.
func (a *Adapter) GitStatus(
	ctx context.Context,
	containerName string,
) (repository bool, head, status, diffStat string, err error) {
	if !a.Available() {
		return false, "", "", "", command.ErrUnavailable
	}
	if strings.TrimSpace(containerName) == "" {
		return false, "", "", "", fmt.Errorf("schedule probe: container name is required")
	}
	// One exec, three probes: the scheduler pays a single container round trip
	// per capture, and the sections are separated by a marker the parser owns.
	script := strings.Join([]string{
		"cd " + WorkspaceDir + " 2>/dev/null || exit 3",
		"git rev-parse --is-inside-work-tree >/dev/null 2>&1 || exit 4",
		"echo '<<<HEAD>>>'",
		"git rev-parse HEAD 2>/dev/null",
		"echo '<<<STATUS>>>'",
		"git status --porcelain 2>/dev/null",
		"echo '<<<DIFFSTAT>>>'",
		"git diff --stat 2>/dev/null",
	}, "\n")
	output, runErr := command.RunWithTimeout(
		ctx, a.runner, gitTimeout, "exec", containerName, "--", "sh", "-c", script,
	)
	if runErr != nil || !strings.Contains(output, "<<<HEAD>>>") {
		return false, "", "", "", nil
	}
	head = strings.TrimSpace(section(output, "<<<HEAD>>>", "<<<STATUS>>>"))
	status = section(output, "<<<STATUS>>>", "<<<DIFFSTAT>>>")
	diffStat = sectionTail(output, "<<<DIFFSTAT>>>")
	return true, head, truncate(status), truncate(diffStat), nil
}

// GitShowStat returns `git show --stat` for one commit, read live.
func (a *Adapter) GitShowStat(
	ctx context.Context,
	containerName string,
	ref string,
) (string, error) {
	if !a.Available() {
		return "", command.ErrUnavailable
	}
	if !validCommitRef(ref) {
		return "", fmt.Errorf("schedule probe: invalid commit reference")
	}
	output, err := command.RunWithTimeout(
		ctx, a.runner, gitTimeout,
		"exec", containerName, "--",
		"git", "-C", WorkspaceDir, "show", "--stat", "--no-color", ref,
	)
	if err != nil {
		return "", fmt.Errorf("schedule probe: read commit: %w", err)
	}
	return truncate(strings.TrimRight(output, "\n")), nil
}

// validCommitRef accepts only a hex object name. The reference reaches an
// argv position, never a shell, but keeping it to hex means a corrupted
// history record can never turn into a git option.
func validCommitRef(ref string) bool {
	ref = strings.TrimSpace(ref)
	if len(ref) < 7 || len(ref) > 64 {
		return false
	}
	for _, c := range ref {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}

func parseExitCode(output string) (int, bool) {
	matches := exitPattern.FindAllStringSubmatch(output, -1)
	if len(matches) == 0 {
		return 0, false
	}
	code, err := strconv.Atoi(matches[len(matches)-1][1])
	if err != nil {
		return 0, false
	}
	return code, true
}

func stripExitMarker(output string) string {
	index := strings.LastIndex(output, exitMarker)
	if index < 0 {
		return output
	}
	return strings.TrimRight(output[:index], "\n")
}

func section(output, start, end string) string {
	from := strings.Index(output, start)
	if from < 0 {
		return ""
	}
	from += len(start)
	to := strings.Index(output[from:], end)
	if to < 0 {
		return strings.TrimPrefix(output[from:], "\n")
	}
	return strings.TrimPrefix(output[from:from+to], "\n")
}

func sectionTail(output, start string) string {
	from := strings.Index(output, start)
	if from < 0 {
		return ""
	}
	return strings.TrimPrefix(output[from+len(start):], "\n")
}

func truncate(value string) string {
	if len(value) <= maxProbeOutput {
		return value
	}
	return value[:maxProbeOutput] + "\n… (truncated)"
}
