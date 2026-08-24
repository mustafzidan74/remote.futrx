// Package filepreview provisions the per-container workspace file preview: a
// read-only static server that makes anything an agent writes viewable.
//
// The gap it closes: the dev-URL proxy forwards <slug>--<port> to a port inside
// the container, which assumes something is listening. An agent asked for "a
// landing page" writes index.html and starts nothing, so the preview answered
// 502 while the chat said the work was done. The operator had a file they could
// not look at without opening the IDE.
//
// It is not a dev server. It reads files and returns them — no build step, no
// watcher, no execution. Anything that needs to run still runs on its own port,
// exactly as before.
package filepreview

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"strconv"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/integration/containers/command"
	"github.com/futrx-com/remote.futrx.com/internal/shared/output"
)

//go:embed assets/file-preview-up.sh
var installScript []byte

// Port is where the preview listens inside every container.
//
// It sits next to code-server's 8842 in the platform's own band rather than in
// the 3000/5173/8080 range an operator's dev server would take, so enabling
// this can never collide with the thing it is meant to sit beside.
const Port = 8843

// InstallScript returns the install program with the port filled in.
func InstallScript() []byte {
	return bytes.ReplaceAll(installScript, []byte("__PREVIEW_PORT__"), []byte(strconv.Itoa(Port)))
}

// Provisioner installs and enables the preview inside a container.
type Provisioner struct {
	runner command.Runner
}

func NewProvisioner(runner command.Runner) *Provisioner {
	return &Provisioner{runner: runner}
}

// Ensure makes the preview live. Idempotent and best-effort, like the other
// container migrations: a container that cannot run it keeps working, it just
// has no file preview.
//
// The active check comes first so a running container costs one exec, and the
// enable runs even when the unit already exists — a present-but-stopped unit is
// exactly the state that leaves the link dead while everything looks installed.
func (p *Provisioner) Ensure(ctx context.Context, containerName string) error {
	if _, err := command.RunWithTimeout(ctx, p.runner, 10*time.Second,
		"exec", containerName, "--", "systemctl", "is-active", "--quiet", "remote-file-preview.service"); err == nil {
		return nil
	}

	if _, err := command.RunWithTimeout(ctx, p.runner, 10*time.Second,
		"exec", containerName, "--", "test", "-f", "/etc/systemd/system/remote-file-preview.service"); err != nil {
		if out, err := command.RunWithTimeout(ctx, p.runner, 2*time.Minute,
			"exec", containerName, "--", "bash", "-c", string(InstallScript())); err != nil {
			return fmt.Errorf("install file preview: %w; output: %s", err, output.TruncateTail(out, 2000))
		}
		return nil
	}

	if out, err := command.RunWithTimeout(ctx, p.runner, 20*time.Second,
		"exec", containerName, "--", "systemctl", "enable", "--now", "remote-file-preview.service"); err != nil {
		return fmt.Errorf("enable file preview: %w; output: %s", err, output.TruncateTail(out, 1000))
	}
	return nil
}
