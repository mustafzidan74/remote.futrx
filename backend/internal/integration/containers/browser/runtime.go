package browser

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/futrx-com/remote.futrx.com/internal/integration/containers/command"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
	"github.com/futrx-com/remote.futrx.com/internal/shared/output"
)

const (
	agentBrowserReadyTimeout = 60 * time.Second
	stopTimeout              = 30 * time.Second
	navigateTimeout          = 15 * time.Second

	// cdpEndpoint is the DevTools HTTP endpoint gui-up.sh binds Chrome to,
	// on container loopback only. `PUT /json/new?<url>` opens a new tab and
	// focuses it; newer Chrome refuses the historic GET form.
	cdpEndpoint = "http://127.0.0.1:9222"
)

// agentBrowserRuntime owns launcher commands and translation of the launcher's
// split core/view status.
type agentBrowserRuntime struct {
	runner command.Runner
}

func (r *agentBrowserRuntime) start(ctx context.Context, containerName, verb, label string) error {
	if out, err := command.RunWithTimeout(ctx, r.runner, agentBrowserReadyTimeout, "exec", containerName, "--", "sh", containerGUIScript, verb); err != nil {
		return fmt.Errorf("%s: %w; output: %s", label, err, output.TruncateTail(out, 1000))
	}
	return nil
}

func (r *agentBrowserRuntime) stop(ctx context.Context, containerName string) error {
	if !r.runner.Available() {
		return command.ErrUnavailable
	}
	if out, err := command.RunWithTimeout(ctx, r.runner, stopTimeout, "exec", containerName, "--", "sh", containerGUIScript, "stop"); err != nil {
		return fmt.Errorf("stop agent browser: %w; output: %s", err, output.TruncateTail(out, 1000))
	}
	return nil
}

func (r *agentBrowserRuntime) stopView(ctx context.Context, containerName string) error {
	if !r.runner.Available() {
		return command.ErrUnavailable
	}
	if out, err := command.RunWithTimeout(ctx, r.runner, stopTimeout, "exec", containerName, "--", "sh", containerGUIScript, "stop-view"); err != nil {
		return fmt.Errorf("stop agent browser view: %w; output: %s", err, output.TruncateTail(out, 1000))
	}
	return nil
}

// navigate opens url in a new tab of the container's running Chrome. The
// request is made from inside the container, so the CDP port never has to
// leave loopback and a preview served on 127.0.0.1 is reached without the
// platform's authenticated public edge.
func (r *agentBrowserRuntime) navigate(ctx context.Context, containerName, url string) error {
	if !r.runner.Available() {
		return command.ErrUnavailable
	}
	out, err := command.RunWithTimeout(
		ctx, r.runner, navigateTimeout,
		"exec", containerName, "--",
		"curl", "-sS", "-f", "--max-time", "10", "-X", "PUT", cdpEndpoint+"/json/new?"+url,
	)
	if err != nil {
		return fmt.Errorf("navigate agent browser: %w; output: %s", err, output.TruncateTail(out, 1000))
	}
	return nil
}

func (r *agentBrowserRuntime) running(ctx context.Context, containerName string) (bool, error) {
	info, err := r.status(ctx, containerName)
	if err != nil {
		return false, err
	}
	return info.Core == "ready", nil
}

func (r *agentBrowserRuntime) status(ctx context.Context, containerName string) (serviceproject.AgentBrowserInfo, error) {
	if !r.runner.Available() {
		return serviceproject.AgentBrowserInfo{}, command.ErrUnavailable
	}
	out, err := command.RunWithTimeout(ctx, r.runner, queryTimeout, "exec", containerName, "--", "sh", containerGUIScript, "status")
	if err != nil {
		return serviceproject.AgentBrowserInfo{
			Status: serviceproject.AgentBrowserStatusStopped,
			Core:   "off",
			View:   "off",
		}, nil
	}
	return parseAgentBrowserStatus(out), nil
}

func parseAgentBrowserStatus(out string) serviceproject.AgentBrowserInfo {
	info := serviceproject.AgentBrowserInfo{
		Status: serviceproject.AgentBrowserStatusStopped,
		Core:   "off",
		View:   "off",
	}
	for _, field := range strings.Fields(out) {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch strings.TrimSpace(key) {
		case "core":
			info.Core = value
		case "view":
			info.View = value
		case "clients":
			if count, err := strconv.Atoi(value); err == nil {
				info.ViewerCount = count
			}
		case "uptime_sec":
			if seconds, err := strconv.ParseInt(value, 10, 64); err == nil {
				info.UptimeSec = seconds
			}
		}
	}
	switch {
	case info.Core == "ready" && info.View == "ready":
		info.Status = serviceproject.AgentBrowserStatusReady
	case info.Core == "ready":
		info.Status = serviceproject.AgentBrowserStatusCoreReady
	default:
		info.Status = serviceproject.AgentBrowserStatusStopped
	}
	return info
}
