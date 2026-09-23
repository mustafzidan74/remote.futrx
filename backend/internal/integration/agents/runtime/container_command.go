package runtime

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/agent"
)

// ContainerCommandSpec is the provider-owned portion of one lxc exec command.
// The builder preserves environment precedence while keeping provider CLI
// syntax in the adapter that understands it.
type ContainerCommandSpec struct {
	ContainerName      string
	WorkingDirectory   string
	PrefixEnvironment  []string
	Secrets            []agent.ProjectSecret
	ExcludedSecrets    []string
	SuffixEnvironment  []string
	RuntimeEnvironment map[string]string
	// FinalEnvironment is applied after the runtime environment, so a
	// third-party endpoint's base URL and key have the last word.
	FinalEnvironment []string
	Binary           string
	Arguments        []string
}

// environmentDir is where a run's environment file waits for the command that
// reads it. /run is a tmpfs in the container images, so the file never reaches
// the container's disk.
const environmentDir = "/run/remote"

// environmentPushTimeout bounds the file push that precedes every run. A push
// slower than this falls back to passing the environment on the command line.
const environmentPushTimeout = 20 * time.Second

// sourceAndExec loads the environment file named by $0, deletes it before the
// agent starts, and replaces itself with the agent. `set -a` exports every
// assignment, so each line needs no `export` of its own.
const sourceAndExec = `set -a; . "$0" || { rm -f -- "$0"; exit 127; }; set +a; rm -f -- "$0"; exec "$@"`

var shellName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// pushEnvironment writes content into the container at path, reading it from
// stdin so the values never appear in any process's arguments. Tests replace
// it; the default needs the lxc client.
var pushEnvironment = func(ctx context.Context, container, path string, content []byte) error {
	if _, err := exec.LookPath("lxc"); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, environmentPushTimeout)
	defer cancel()
	push := exec.CommandContext(ctx, "lxc", "file", "push", "--create-dirs", "--mode", "0600",
		"-", container+path)
	push.Stdin = bytes.NewReader(content)
	if out, err := push.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// BuildContainerCommand constructs, but does not start, an lxc exec command.
// Runtime keys mask same-named project secrets before invalid runtime names
// are discarded, preserving the backend-issued environment's precedence.
//
// The environment carries project secrets and API keys. Passed as `--env
// KEY=VALUE` it sat in the lxc client's arguments for the whole run, where
// anything able to list the host's processes could read it. It now travels as
// a file pushed over stdin, which the command sources and deletes before the
// agent starts. If the push fails the run still happens, the old way, and the
// fallback is logged — a secret on the command line is a smaller harm than a
// run that silently never starts.
func BuildContainerCommand(ctx context.Context, spec ContainerCommandSpec) *exec.Cmd {
	workingDirectory := spec.WorkingDirectory
	if workingDirectory == "" {
		workingDirectory = agent.ProjectWorkspacePath
	}
	environment := orderedEnvironment(spec)
	if len(environment) > 0 {
		path := environmentDir + "/env-" + randomSuffix()
		err := pushEnvironment(ctx, spec.ContainerName, path, environmentFile(environment))
		if err == nil {
			args := []string{"exec", "--cwd", workingDirectory, spec.ContainerName, "--",
				"sh", "-c", sourceAndExec, path, spec.Binary}
			args = append(args, spec.Arguments...)
			return exec.CommandContext(ctx, "lxc", args...)
		}
		log.Printf("container command: environment file for %s unavailable, passing it as arguments: %v",
			spec.ContainerName, err)
	}
	args := []string{"exec", "--cwd", workingDirectory}
	for _, entry := range environment {
		args = append(args, "--env", entry)
	}
	args = append(args, spec.ContainerName, "--", spec.Binary)
	args = append(args, spec.Arguments...)
	return exec.CommandContext(ctx, "lxc", args...)
}

// orderedEnvironment lists KEY=VALUE entries in precedence order: a later
// entry for the same key wins, exactly as repeated `--env` flags do.
func orderedEnvironment(spec ContainerCommandSpec) []string {
	var entries []string
	entries = append(entries, spec.PrefixEnvironment...)
	excluded := make(map[string]struct{}, len(spec.ExcludedSecrets))
	for _, key := range spec.ExcludedSecrets {
		excluded[key] = struct{}{}
	}
	for _, secret := range spec.Secrets {
		if _, skip := excluded[secret.Key]; skip {
			continue
		}
		if _, backendIssued := spec.RuntimeEnvironment[secret.Key]; backendIssued {
			continue
		}
		entries = append(entries, secret.Key+"="+secret.Value)
	}
	entries = append(entries, spec.SuffixEnvironment...)
	entries = append(entries, agent.RuntimeEnvironment(spec.RuntimeEnvironment)...)
	entries = append(entries, spec.FinalEnvironment...)
	return entries
}

// environmentFile renders entries as shell assignments, single-quoted so a
// value is never expanded or executed. A name the shell cannot assign is left
// out; every source of these names already requires POSIX names.
func environmentFile(entries []string) []byte {
	var out bytes.Buffer
	for _, entry := range entries {
		key, value, ok := strings.Cut(entry, "=")
		if !ok || !shellName.MatchString(key) {
			continue
		}
		out.WriteString(key)
		out.WriteString("='")
		out.WriteString(strings.ReplaceAll(value, "'", `'\''`))
		out.WriteString("'\n")
	}
	return out.Bytes()
}

func randomSuffix() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}
