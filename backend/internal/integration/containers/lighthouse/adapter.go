// Package lighthouse runs Google's Lighthouse CLI inside a project container.
//
// It runs in the guest for the same reason screenshots do: the dev server is
// on container loopback, so nothing has to be published, shared or
// authenticated for the browser to reach it. The JSON report is written to a
// throwaway path in the guest, pulled back over the container CLI, and deleted.
package lighthouse

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/integration/containers/command"
	servicelighthouse "github.com/futrx-com/remote.futrx.com/internal/service/lighthouse"
)

// probeTimeout bounds the "is the CLI even here?" check. It is a `command -v`,
// so anything slower than this means the container is wedged.
const probeTimeout = 10 * time.Second

// cleanupTimeout bounds the best-effort unlink of the throwaway report.
const cleanupTimeout = 10 * time.Second

// browsersPath mirrors what the base image recipe exports, verbatim, from
// playwrightInstallScript in
// backend/internal/service/container/image/recipe.go. `lxc exec` sources
// neither /etc/profile.d nor /etc/environment, so it has to be passed
// explicitly. Change both together.
const browsersPath = "/opt/pw-browsers"

// chromeDiscovery finds the Chromium that Playwright installed.
//
// The path carries Playwright's own build number (chromium-1234), which moves
// with every Playwright release, so it cannot be a constant. Resolving it in
// the guest with a glob means an image upgrade does not silently break audits
// on the day Playwright bumps that number.
const chromeDiscovery = `CHROME_PATH="$(ls -d /opt/pw-browsers/chromium-*/chrome-linux64/chrome 2>/dev/null | head -1)"; export CHROME_PATH`

// missingToolingMarkers are what npm, npx and the shell print when the CLI is
// absent. They are matched case-insensitively so the caller gets the
// actionable 409 instead of a raw exec failure.
var missingToolingMarkers = []string{
	"command not found",
	"not found",
	"cannot find module",
	"could not determine executable to run",
}

// Adapter runs the Lighthouse CLI inside a project container.
type Adapter struct {
	runner command.Runner
}

var _ servicelighthouse.Runner = (*Adapter)(nil)

func NewAdapter(runner command.Runner) *Adapter {
	return &Adapter{runner: runner}
}

func (a *Adapter) Available() bool {
	return a != nil && a.runner != nil && a.runner.Available()
}

// Installed reports whether the container has the CLI.
//
// It is asked before every run rather than cached, because a container is
// replaced whenever its image changes and a cache would confidently answer for
// one that no longer exists. It is one `command -v`.
func (a *Adapter) Installed(ctx context.Context, containerName string) (bool, error) {
	if !a.Available() {
		return false, command.ErrUnavailable
	}
	out, err := command.RunWithTimeout(ctx, a.runner, probeTimeout,
		"exec", containerName, "--", "sh", "-c", "command -v lighthouse")
	if err != nil {
		return false, nil
	}
	return strings.TrimSpace(out) != "", nil
}

// Install adds the CLI to a container that predates it.
//
// New images ship with Lighthouse, but a project created before that has its
// own filesystem and will never get it from a rebuild it is not part of.
// Replacing every existing container to add one npm package would cost the
// operator far more than this does, so the install is offered as a deliberate,
// visible action instead.
func (a *Adapter) Install(ctx context.Context, containerName string) error {
	if !a.Available() {
		return command.ErrUnavailable
	}
	out, err := a.runner.Run(ctx,
		"exec", containerName,
		"--env", "HOME=/root",
		"--",
		"npm", "install", "-g", "lighthouse", "--silent",
	)
	if err != nil {
		return fmt.Errorf("install lighthouse in %s failed: %w; output: %s",
			containerName, err, tail(out))
	}
	installed, err := a.Installed(ctx, containerName)
	if err != nil {
		return err
	}
	if !installed {
		return fmt.Errorf("lighthouse install reported success but the binary is not on PATH: %s", tail(out))
	}
	return nil
}

// Audit runs one page and returns the raw JSON report.
//
// The whole sequence shares the caller's deadline: a page that never settles
// must not hold a run open while Chromium waits for a network idle that is not
// coming.
func (a *Adapter) Audit(ctx context.Context, req servicelighthouse.AuditRequest) ([]byte, error) {
	if !a.Available() {
		return nil, command.ErrUnavailable
	}
	if strings.TrimSpace(req.ContainerName) == "" || strings.TrimSpace(req.RemotePath) == "" {
		return nil, fmt.Errorf("lighthouse: container name and remote path are required")
	}

	// Only ever removed, never read by name from anywhere else: the file
	// exists for the length of one pull.
	defer a.cleanup(req.ContainerName, req.RemotePath)

	out, err := a.runner.Run(ctx, auditArgs(req)...)
	if err != nil {
		if isMissingTooling(out) {
			return nil, servicelighthouse.ErrToolingMissing
		}
		return nil, fmt.Errorf("lighthouse in %s failed: %w; output: %s",
			req.ContainerName, err, tail(out))
	}
	if isMissingTooling(out) {
		return nil, servicelighthouse.ErrToolingMissing
	}

	pulled, err := a.runner.Run(ctx, "file", "pull", req.ContainerName+req.RemotePath, "-")
	if err != nil {
		return nil, fmt.Errorf("pull lighthouse report from %s failed: %w; output: %s",
			req.ContainerName, err, tail(pulled))
	}
	return []byte(pulled), nil
}

// auditArgs is the exact `lxc exec` invocation, split out so a test can assert
// the command line without a container.
//
// Notes on the flags, because each is load-bearing:
//
//   - --no-sandbox: the container is already the sandbox, and Chromium's own
//     one needs privileges an unprivileged LXC guest does not have.
//   - --disable-dev-shm-usage: the guest's /dev/shm is small, and Chromium
//     crashes on a large page without this rather than reporting anything.
//   - --only-categories: the four an operator acts on. Skipping the rest is
//     what keeps a page under half a minute.
//   - --quiet: the CLI's progress output would otherwise be interleaved with
//     anything the exec prints, and this platform reads that stream.
func auditArgs(req servicelighthouse.AuditRequest) []string {
	preset := ""
	if req.FormFactor == servicelighthouse.FormFactorDesktop {
		preset = " --preset=desktop"
	}
	script := chromeDiscovery + `; exec lighthouse "$1" --quiet --output=json --output-path="$2"` +
		` --only-categories=performance,accessibility,best-practices,seo` +
		` --chrome-flags="--headless=new --no-sandbox --disable-dev-shm-usage"` + preset

	return []string{
		"exec", req.ContainerName,
		"--env", "HOME=/root",
		"--env", "PLAYWRIGHT_BROWSERS_PATH=" + browsersPath,
		"--",
		"sh", "-c", script, "sh", req.URL, req.RemotePath,
	}
}

func (a *Adapter) cleanup(containerName, remotePath string) {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	_, _ = a.runner.Run(ctx, "exec", containerName, "--", "rm", "-f", remotePath)
}

func isMissingTooling(output string) bool {
	lowered := strings.ToLower(output)
	for _, marker := range missingToolingMarkers {
		if strings.Contains(lowered, marker) {
			return true
		}
	}
	return false
}

// tail keeps an error message readable when a browser prints a stack trace.
func tail(output string) string {
	trimmed := strings.TrimSpace(output)
	const limit = 400
	if len(trimmed) <= limit {
		return trimmed
	}
	return "…" + trimmed[len(trimmed)-limit:]
}
