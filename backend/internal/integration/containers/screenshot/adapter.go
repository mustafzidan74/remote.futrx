// Package screenshot photographs a project's own preview from inside its
// container.
//
// The capture runs in the guest rather than on the host for the same reason
// the Agent Browser does: the dev server is reachable on container loopback,
// so nothing has to be published, shared, or authenticated to look at it. The
// PNG is written to a throwaway path in the guest, pulled back over the
// container CLI, and deleted.
package screenshot

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/integration/containers/command"
	servicescreenshot "github.com/futrx-com/remote.futrx.com/internal/service/screenshot"
)

// probeTimeout bounds the "is Playwright even here?" check. It is a
// `command -v`, so anything slower than this means the container is wedged.
const probeTimeout = 10 * time.Second

// cleanupTimeout bounds the best-effort unlink of the throwaway file.
const cleanupTimeout = 10 * time.Second

// browsersPath and nodePath mirror the two variables the base image recipe
// exports, verbatim, from playwrightInstallScript in
// backend/internal/service/container/image/recipe.go. They have to be passed
// explicitly: the recipe publishes them through /etc/profile.d and
// /etc/environment, and `lxc exec` sources neither — without them Playwright
// looks for Chromium in ~/.cache and reports it missing on an image that has
// it. Change both together.
const (
	browsersPath = "/opt/pw-browsers"
	nodePath     = "/usr/lib/node_modules"
)

// missingToolingMarkers are the strings Playwright and npx print when the
// package or the browser binary is absent. They are matched case-insensitively
// so the caller gets the actionable 409 hint instead of a raw exec failure.
var missingToolingMarkers = []string{
	"executable doesn't exist",
	"please run the following command to download new browsers",
	"could not determine executable to run",
	"command not found",
	"cannot find module",
}

// Adapter runs the headless browser inside a project container.
type Adapter struct {
	runner command.Runner
}

var _ servicescreenshot.Capturer = (*Adapter)(nil)

// NewAdapter returns an adapter backed by runner.
func NewAdapter(runner command.Runner) *Adapter {
	return &Adapter{runner: runner}
}

func (a *Adapter) Available() bool {
	return a != nil && a.runner != nil && a.runner.Available()
}

// Capture drives `playwright screenshot` in the guest and returns the PNG.
//
// The whole sequence shares the caller's deadline (the service budgets 30
// seconds for it): a slow page must not be able to hold an HTTP request open
// while Chromium waits for a network idle that never comes.
func (a *Adapter) Capture(ctx context.Context, req servicescreenshot.CaptureRequest) ([]byte, error) {
	if !a.Available() {
		return nil, command.ErrUnavailable
	}
	if strings.TrimSpace(req.ContainerName) == "" || strings.TrimSpace(req.RemotePath) == "" {
		return nil, fmt.Errorf("screenshot: container name and remote path are required")
	}
	if err := a.probe(ctx, req.ContainerName); err != nil {
		return nil, err
	}

	// Only ever removed, never read back by name from anywhere else: the file
	// exists for the length of one pull.
	defer a.cleanup(req.ContainerName, req.RemotePath)

	out, err := a.runner.Run(ctx, screenshotArgs(req)...)
	if err != nil {
		if isMissingTooling(out) {
			return nil, servicescreenshot.ErrToolingMissing
		}
		return nil, fmt.Errorf("playwright screenshot in %s failed: %w; output: %s",
			req.ContainerName, err, tail(out))
	}
	// Playwright can exit 0 having printed a browser-missing warning when the
	// bundled runner falls back; treat that as the same actionable state.
	if isMissingTooling(out) {
		return nil, servicescreenshot.ErrToolingMissing
	}

	return a.pull(ctx, req.ContainerName, req.RemotePath)
}

// pull copies the PNG to a host temp file and reads it back.
//
// It does not pull to stdout, which would be the shorter spelling: the runner
// returns stdout and stderr merged into one string, so any warning `lxc` chose
// to print during a successful transfer would end up spliced into the image.
// Every other file pull in this codebase lands on a host path for the same
// reason.
func (a *Adapter) pull(ctx context.Context, containerName, remotePath string) ([]byte, error) {
	local, err := os.CreateTemp("", "remote-shot-*.png")
	if err != nil {
		return nil, fmt.Errorf("stage screenshot on the host: %w", err)
	}
	localPath := local.Name()
	// lxc writes the file itself; the handle only reserved the name.
	local.Close()
	defer os.Remove(localPath)

	out, err := a.runner.Run(ctx, "file", "pull", containerName+remotePath, localPath)
	if err != nil {
		return nil, fmt.Errorf("pull screenshot from %s failed: %w; output: %s",
			containerName, err, tail(out))
	}
	data, err := os.ReadFile(localPath)
	if err != nil {
		return nil, fmt.Errorf("read the pulled screenshot: %w", err)
	}
	return data, nil
}

// screenshotArgs is the exact `lxc exec` invocation, split out so a test can
// assert the command line without a container.
//
// `--no-install` keeps npx from reaching for the network: Playwright is baked
// into the base image, so a container without it is a rebuild, not a download.
// The viewport is comma-separated because that is what the Playwright CLI
// parses.
func screenshotArgs(req servicescreenshot.CaptureRequest) []string {
	args := []string{
		"exec", req.ContainerName,
		"--env", "HOME=/root",
		"--env", "PLAYWRIGHT_BROWSERS_PATH=" + browsersPath,
		"--env", "NODE_PATH=" + nodePath,
		"--",
		"npx", "--no-install", "playwright", "screenshot",
		"--browser", "chromium",
		"--viewport-size", strconv.Itoa(req.Width) + "," + strconv.Itoa(req.Height),
	}
	if req.FullPage {
		args = append(args, "--full-page")
	}
	return append(args, req.URL, req.RemotePath)
}

// probe answers "can this container run Playwright at all?" before a browser
// is launched, so a container built from an older image reports the rebuild
// hint in a second rather than after a Chromium timeout.
func (a *Adapter) probe(ctx context.Context, containerName string) error {
	out, err := command.RunWithTimeout(ctx, a.runner, probeTimeout,
		"exec", containerName, "--", "sh", "-c", "command -v npx")
	if err != nil || strings.TrimSpace(out) == "" {
		return servicescreenshot.ErrToolingMissing
	}
	return nil
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
