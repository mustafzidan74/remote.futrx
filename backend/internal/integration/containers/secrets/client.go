// Package secrets materializes the platform secrets vault inside a project
// container: credential files, SSH private keys, and the vault-owned regions
// of /root/.ssh/config and /root/.ssh/known_hosts.
//
// Every write goes through `lxc file push --mode=0600`, and the set of paths
// written is recorded in a manifest inside the container. The manifest is
// what makes removal exact: without it, a `file` entry pointed at a new path
// would leave the old file behind forever.
//
// Nothing here logs a value, a path's contents, or command output that could
// echo one back.
package secrets

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
	serviceglobalsecrets "github.com/futrx-com/remote.futrx.com/internal/service/globalsecrets"
)

const (
	queryTimeout   = 10 * time.Second
	applyTimeout   = 60 * time.Second
	pushTimeout    = 30 * time.Second
	regionConfig   = serviceglobalsecrets.SSHDir + "/.remote-vault-config"
	regionKnown    = serviceglobalsecrets.SSHDir + "/.remote-vault-known-hosts"
	directoryMode  = "700"
	secretFileMode = "0600"
)

// Client materializes vault material through the container runtime CLI.
type Client struct {
	runner command.Runner
}

// NewClient returns a materializer backed by runner.
func NewClient(runner command.Runner) *Client {
	return &Client{runner: runner}
}

// Manifest reads the record the previous sync left behind. A container that
// has never been synced — or whose manifest was removed with the rest of a
// replaced rootfs — reports the zero manifest so the next Apply simply writes
// everything fresh.
func (c *Client) Manifest(ctx context.Context, container string) (serviceglobalsecrets.Manifest, error) {
	if !c.runner.Available() {
		return serviceglobalsecrets.Manifest{}, command.ErrUnavailable
	}
	if strings.TrimSpace(container) == "" {
		return serviceglobalsecrets.Manifest{}, fmt.Errorf("container name required")
	}
	out, err := command.RunWithTimeout(
		ctx, c.runner, queryTimeout,
		"exec", container, "--", "cat", serviceglobalsecrets.ManifestPath,
	)
	if err != nil {
		return serviceglobalsecrets.Manifest{}, nil
	}
	trimmed := strings.TrimSpace(out)
	if trimmed == "" {
		return serviceglobalsecrets.Manifest{}, nil
	}
	var manifest serviceglobalsecrets.Manifest
	if err := json.Unmarshal([]byte(trimmed), &manifest); err != nil {
		// A corrupt manifest is not worth failing a sync over; treating it as
		// absent costs one round of stale material that the next edit fixes.
		return serviceglobalsecrets.Manifest{}, nil
	}
	return manifest, nil
}

// Apply writes every file in material, merges the vault-owned regions of the
// shared SSH files, deletes staleFiles, and stores the new manifest.
func (c *Client) Apply(
	ctx context.Context,
	container string,
	material serviceglobalsecrets.Material,
	staleFiles []string,
) error {
	if !c.runner.Available() {
		return command.ErrUnavailable
	}
	if strings.TrimSpace(container) == "" {
		return fmt.Errorf("container name required")
	}

	actx, cancel := context.WithTimeout(ctx, applyTimeout)
	defer cancel()

	if err := c.ensureDirectories(actx, container, material); err != nil {
		return err
	}
	for _, file := range material.Files {
		if err := c.push(actx, container, file.Path, file.Content); err != nil {
			return err
		}
	}
	// The two shared files are merged, not replaced, so a region file is
	// staged first and consumed by the script below.
	if err := c.push(actx, container, regionConfig, material.SSHConfig); err != nil {
		return err
	}
	if err := c.push(actx, container, regionKnown, material.KnownHosts); err != nil {
		return err
	}

	manifest, err := json.Marshal(serviceglobalsecrets.ManifestFor(material))
	if err != nil {
		return fmt.Errorf("encode secrets manifest: %w", err)
	}
	if err := c.push(actx, container, serviceglobalsecrets.ManifestPath, string(manifest)+"\n"); err != nil {
		return err
	}

	script := applyScript(staleFiles)
	if out, err := c.runner.Run(actx, "exec", container, "--", "sh", "-c", script); err != nil {
		// The script never echoes a value, so its output is safe to attach.
		return fmt.Errorf("converge secrets material on %s: %w; output: %s", container, err, out)
	}
	return nil
}

// push writes one file into the container at mode 0600 through a host temp
// file, which is deleted whatever happens next.
func (c *Client) push(ctx context.Context, container, destination, content string) error {
	tmp, err := os.CreateTemp("", "futrx-vault-*")
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
		"file", "push", "--mode="+secretFileMode, tmp.Name(), container+destination,
	)
	if err != nil {
		// Never attach output: `lxc file push` can echo the destination and,
		// on some failures, part of the payload.
		return fmt.Errorf("push %s to %s: %w", destination, container, err)
	}
	return nil
}

// ensureDirectories creates every parent directory a push needs, plus
// /root/.ssh itself, so a first sync into a fresh container succeeds.
func (c *Client) ensureDirectories(
	ctx context.Context,
	container string,
	material serviceglobalsecrets.Material,
) error {
	seen := map[string]bool{}
	directories := []string{serviceglobalsecrets.SSHDir}
	seen[serviceglobalsecrets.SSHDir] = true
	for _, file := range material.Files {
		parent := path.Dir(file.Path)
		if parent == "" || parent == "/" || seen[parent] {
			continue
		}
		seen[parent] = true
		directories = append(directories, parent)
	}
	sort.Strings(directories)

	args := append([]string{"exec", container, "--", "install", "-d", "-m", directoryMode}, directories...)
	if out, err := command.RunWithTimeout(ctx, c.runner, queryTimeout, args...); err != nil {
		return fmt.Errorf("create secret directories on %s: %w; output: %s", container, err, out)
	}
	return nil
}

// applyScript merges the staged regions into the shared SSH files and deletes
// what the previous manifest owned and this pass does not.
//
// The merge strips the previous vault region and appends the current one, so
// regenerating from unchanged input is byte-identical and anything an agent
// added to the file by hand survives untouched.
func applyScript(staleFiles []string) string {
	var script strings.Builder
	script.WriteString(`set -eu
BEGIN_MARK=` + shellQuote(serviceglobalsecrets.ManagedBegin) + `
END_MARK=` + shellQuote(serviceglobalsecrets.ManagedEnd) + `

merge_region() {
  target="$1"
  region="$2"
  [ -f "$target" ] || : > "$target"
  awk -v b="$BEGIN_MARK" -v e="$END_MARK" '
    $0 == b { skip = 1; next }
    $0 == e { skip = 0; next }
    skip != 1 { print }
  ' "$target" > "$target.vault-tmp"
  if [ -s "$region" ]; then
    printf '%s\n' "$BEGIN_MARK" >> "$target.vault-tmp"
    cat "$region" >> "$target.vault-tmp"
    printf '%s\n' "$END_MARK" >> "$target.vault-tmp"
  fi
  mv "$target.vault-tmp" "$target"
  chmod 600 "$target"
  rm -f "$region"
}

chmod 700 ` + shellQuote(serviceglobalsecrets.SSHDir) + `
merge_region ` + shellQuote(serviceglobalsecrets.SSHConfigPath) + ` ` + shellQuote(regionConfig) + `
merge_region ` + shellQuote(serviceglobalsecrets.KnownHostsPath) + ` ` + shellQuote(regionKnown) + `
`)
	for _, stale := range staleFiles {
		fmt.Fprintf(&script, "rm -f %s\n", shellQuote(stale))
	}
	return script.String()
}

// shellQuote wraps a value in single quotes for `sh -c`, escaping any single
// quote it contains.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
