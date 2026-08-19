// Package mcp materializes the platform's MCP registry inside a project
// container: Claude Code's `--mcp-config` JSON, the managed region of the
// codex TOML config, and the manifest that makes removing a deleted entry
// exact.
//
// Every write goes through `lxc file push --mode=0600`, because a rendered
// config can carry a value resolved from the Secrets vault. Nothing here logs
// a config body, and the probe's output is handed back to the service to be
// masked before it reaches an API response.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/integration/containers/command"
	servicemcp "github.com/futrx-com/remote.futrx.com/internal/service/mcp"
	"github.com/futrx-com/remote.futrx.com/internal/shared/output"
)

const (
	queryTimeout  = 10 * time.Second
	applyTimeout  = 60 * time.Second
	pushTimeout   = 30 * time.Second
	probeTimeout  = 45 * time.Second
	directoryMode = "700"

	// regionFile stages the managed codex region before the apply script
	// merges it, exactly as the secrets vault stages its ssh_config region.
	regionFile = servicemcp.CodexConfigDir + "/.remote-mcp-region"

	// probeOutputLimit bounds what a misbehaving server can push into an API
	// response through the Test action.
	probeOutputLimit = 4000
)

// initializeRequest is the first message of the MCP handshake. A server that
// answers it at all is reachable and speaking the protocol, which is what the
// Test action is asking. The `notifications/initialized` line that would
// follow is deliberately omitted: the probe closes the pipe instead.
const initializeRequest = `{"jsonrpc":"2.0","id":1,"method":"initialize",` +
	`"params":{"protocolVersion":"2024-11-05","capabilities":{},` +
	`"clientInfo":{"name":"remote.futrx-probe","version":"1"}}}`

// Client materializes registry material through the container runtime CLI.
type Client struct {
	runner command.Runner
}

// NewClient returns a materializer backed by runner.
func NewClient(runner command.Runner) *Client {
	return &Client{runner: runner}
}

// Manifest reads the record the previous pass left behind. A container that
// has never been materialized — or whose manifest went with a replaced rootfs
// — reports the zero manifest so the next Apply writes everything fresh.
func (c *Client) Manifest(ctx context.Context, containerName string) (servicemcp.Manifest, error) {
	if !c.runner.Available() {
		return servicemcp.Manifest{}, command.ErrUnavailable
	}
	if strings.TrimSpace(containerName) == "" {
		return servicemcp.Manifest{}, fmt.Errorf("container name required")
	}
	out, err := command.RunWithTimeout(
		ctx, c.runner, queryTimeout,
		"exec", containerName, "--", "cat", servicemcp.ManifestPath,
	)
	if err != nil {
		return servicemcp.Manifest{}, nil
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return servicemcp.Manifest{}, nil
	}
	var manifest servicemcp.Manifest
	if err := json.Unmarshal([]byte(trimmed), &manifest); err != nil {
		// A corrupt manifest is not worth failing a run over; treating it as
		// absent costs one redundant write that the next pass then skips.
		return servicemcp.Manifest{}, nil
	}
	return manifest, nil
}

// Apply writes every file in material, merges the managed region into the
// codex config, deletes staleFiles, and stores the new manifest.
func (c *Client) Apply(
	ctx context.Context,
	containerName string,
	material servicemcp.Material,
	staleFiles []string,
) error {
	if !c.runner.Available() {
		return command.ErrUnavailable
	}
	if strings.TrimSpace(containerName) == "" {
		return fmt.Errorf("container name required")
	}

	actx, cancel := context.WithTimeout(ctx, applyTimeout)
	defer cancel()

	if err := c.ensureDirectories(actx, containerName, material); err != nil {
		return err
	}
	for _, file := range material.Files {
		if err := c.push(actx, containerName, file.Path, file.Content); err != nil {
			return err
		}
	}
	// The codex config is shared with the operator and with codex itself, so
	// the platform region is staged and merged rather than replacing the file.
	if err := c.push(actx, containerName, regionFile, material.CodexRegion); err != nil {
		return err
	}

	manifest, err := json.Marshal(servicemcp.ManifestFor(material))
	if err != nil {
		return fmt.Errorf("encode MCP manifest: %w", err)
	}
	if err := c.push(actx, containerName, servicemcp.ManifestPath, string(manifest)+"\n"); err != nil {
		return err
	}

	script := applyScript(staleFiles)
	if out, err := c.runner.Run(actx, "exec", containerName, "--", "sh", "-c", script); err != nil {
		// The script never echoes a config body, so its output is safe here.
		return fmt.Errorf("converge MCP configuration on %s: %w; output: %s", containerName, err, out)
	}
	return nil
}

// Probe performs one MCP handshake inside the container and returns the raw
// output. A non-nil error means the probe could not complete; the output is
// still returned so an operator sees why.
func (c *Client) Probe(
	ctx context.Context,
	containerName string,
	server servicemcp.Server,
) (string, error) {
	if !c.runner.Available() {
		return "", command.ErrUnavailable
	}
	if strings.TrimSpace(containerName) == "" {
		return "", fmt.Errorf("container name required")
	}

	args := []string{"exec", containerName, "--cwd", "/workspace", "--env", "HOME=/root"}
	if server.Transport == servicemcp.TransportStdio {
		// Environment travels as `lxc exec --env` rather than inside the
		// script text, so a value never lands in a shell history or in the
		// script this platform writes.
		for _, name := range sortedKeys(server.Env) {
			args = append(args, "--env", name+"="+server.Env[name])
		}
	}
	args = append(args, "--", "sh", "-c", probeScript(server))

	out, err := command.RunWithTimeout(ctx, c.runner, probeTimeout, args...)
	return output.TruncateTail(strings.TrimSpace(out), probeOutputLimit), err
}

// probeScript renders the one-liner run inside the container. Every
// interpolated value is single-quoted, so an argument containing a shell
// metacharacter is data rather than syntax.
func probeScript(server servicemcp.Server) string {
	if server.Transport == servicemcp.TransportHTTP {
		var script strings.Builder
		script.WriteString("printf '%s' " + shellQuote(initializeRequest) +
			" | timeout 30 curl -sS --max-time 25 -o - -w '\\n[http %{http_code}]\\n'" +
			" -X POST -H 'Content-Type: application/json'" +
			" -H 'Accept: application/json, text/event-stream'")
		for _, name := range sortedKeys(server.Headers) {
			script.WriteString(" -H " + shellQuote(name+": "+server.Headers[name]))
		}
		script.WriteString(" --data-binary @- " + shellQuote(server.URL) + " 2>&1")
		return script.String()
	}

	command := shellQuote(server.Command)
	for _, arg := range server.Args {
		command += " " + shellQuote(arg)
	}
	// The handshake is a single line on stdin; the server answers and the
	// pipe closes, which is what ends a well-behaved stdio server.
	return "printf '%s\\n' " + shellQuote(initializeRequest) +
		" | timeout 30 " + command + " 2>&1 | head -c 4000"
}

// push writes one file into the container at mode 0600 through a host temp
// file, which is deleted whatever happens next.
func (c *Client) push(ctx context.Context, containerName, destination, content string) error {
	tmp, err := os.CreateTemp("", "futrx-mcp-*")
	if err != nil {
		return fmt.Errorf("temp file: %w", err)
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("secure temp file: %w", err)
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	_, err = command.RunWithTimeout(
		ctx, c.runner, pushTimeout,
		"file", "push", "--mode="+servicemcp.ConfigFileMode, tmp.Name(), containerName+destination,
	)
	if err != nil {
		// Never attach output: `lxc file push` can echo part of the payload,
		// and a rendered config can carry a resolved vault value.
		return fmt.Errorf("push %s to %s: %w", destination, containerName, err)
	}
	return nil
}

// ensureDirectories creates every parent a push needs, plus the codex home,
// so a first pass into a fresh container succeeds.
func (c *Client) ensureDirectories(
	ctx context.Context,
	containerName string,
	material servicemcp.Material,
) error {
	seen := map[string]bool{servicemcp.CodexConfigDir: true}
	directories := []string{servicemcp.CodexConfigDir}
	for _, file := range material.Files {
		parent := path.Dir(file.Path)
		if parent == "" || parent == "/" || seen[parent] {
			continue
		}
		seen[parent] = true
		directories = append(directories, parent)
	}
	sort.Strings(directories)

	args := append([]string{"exec", containerName, "--", "install", "-d", "-m", directoryMode}, directories...)
	if out, err := command.RunWithTimeout(ctx, c.runner, queryTimeout, args...); err != nil {
		return fmt.Errorf("create MCP config directories on %s: %w; output: %s", containerName, err, out)
	}
	return nil
}

// applyScript merges the staged region into the codex config and deletes what
// the previous manifest owned and this pass does not.
//
// The merge strips the previous region and appends the current one, so
// regenerating from unchanged input is byte-identical and anything an
// operator or the codex CLI added to the file survives untouched. The region
// is appended last, where a run of TOML tables is always legal.
func applyScript(staleFiles []string) string {
	var script strings.Builder
	script.WriteString(`set -eu
BEGIN_MARK=` + shellQuote(servicemcp.ManagedBegin) + `
END_MARK=` + shellQuote(servicemcp.ManagedEnd) + `
TARGET=` + shellQuote(servicemcp.CodexConfigPath) + `
REGION=` + shellQuote(regionFile) + `

[ -f "$TARGET" ] || : > "$TARGET"
awk -v b="$BEGIN_MARK" -v e="$END_MARK" '
  $0 == b { skip = 1; next }
  $0 == e { skip = 0; next }
  skip != 1 { print }
' "$TARGET" > "$TARGET.mcp-tmp"
if [ -s "$REGION" ]; then
  printf '%s\n' "$BEGIN_MARK" >> "$TARGET.mcp-tmp"
  cat "$REGION" >> "$TARGET.mcp-tmp"
  printf '%s\n' "$END_MARK" >> "$TARGET.mcp-tmp"
fi
mv "$TARGET.mcp-tmp" "$TARGET"
chmod 600 "$TARGET"
rm -f "$REGION"
`)
	for _, stale := range staleFiles {
		fmt.Fprintf(&script, "rm -f %s\n", shellQuote(stale))
	}
	return script.String()
}

func sortedKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// shellQuote wraps a value in single quotes for `sh -c`, escaping any single
// quote it contains.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
