// Package health watches running project containers and folds their memory,
// CPU, disk, and preview reachability into a single traffic-light status that
// the sidebar renders and the notification pipeline alerts on.
//
// The monitor is deliberately coarse. One sweep per interval reuses the same
// cheap LXD query paths the project page already calls; nothing here polls per
// second, and no probe shells into a container beyond the listener scan the
// preview picker already performs.
package health

import (
	"sort"
	"strconv"
	"strings"

	serviceshare "github.com/futrx-com/remote.futrx.com/internal/service/share"
)

// Status is one project's traffic light.
type Status string

const (
	// StatusUnknown means no usable measurement was taken: LXD was
	// unreachable, or the container reported no memory accounting.
	StatusUnknown Status = "unknown"
	// StatusOK means every measured signal is inside its threshold.
	StatusOK Status = "ok"
	// StatusWarn means the project still works but is close to a limit.
	StatusWarn Status = "warn"
	// StatusCrit means the project is at or past the point where the agent,
	// the IDE, or the user's app starts failing.
	StatusCrit Status = "crit"
)

// Memory thresholds, in percent of the container's enforced memory limit.
// They are deliberately tight: an LXD container that reaches its ceiling does
// not slow down, it starts OOM-killing whichever process asks next.
const (
	WarnMemoryPercent = 80.0
	CritMemoryPercent = 92.0
)

// ProjectHealth is the cached verdict for one project. It is served whole by
// GET /api/projects/health and broadcast row by row over the workspace
// socket, so its JSON shape is a public contract.
type ProjectHealth struct {
	ProjectID string `json:"projectId"`
	Status    Status `json:"status"`

	MemoryUsedBytes  int64   `json:"memoryUsedBytes,omitempty"`
	MemoryLimitBytes int64   `json:"memoryLimitBytes,omitempty"`
	MemoryPct        float64 `json:"memoryPct,omitempty"`

	// CPUPct is the average load across the container's cores since the
	// previous sweep. It is absent on the first sweep after a start, which is
	// the only sample with nothing to subtract from.
	CPUPct *float64 `json:"cpuPct,omitempty"`
	// DiskUsedPct is the root disk against its quota. It is absent when the
	// storage pool enforces no quota, which is the common case.
	DiskUsedPct *float64 `json:"diskUsedPct,omitempty"`

	// Listeners are every externally reachable TCP port inside the container,
	// platform plumbing included, in ascending order.
	Listeners []int `json:"listeners,omitempty"`
	// PreviewOK reports the HTTP probe of the first application port. It is
	// nil when there was no application port to probe.
	PreviewOK *bool `json:"previewOk,omitempty"`

	LastCheckedAt int64 `json:"lastCheckedAt,omitempty"`
	// Reasons explain the status in the tooltip, most severe first. They
	// describe the most recent measurement, which during the one sweep a
	// hysteresis change is pending can be ahead of Status.
	Reasons []string `json:"reasons,omitempty"`
}

// platformPorts are in-container listeners owned by the platform rather than
// by the project's own application. They answer whether or not the user's app
// is up, so probing one would report a healthy preview for a dead project.
// The port numbers are shared with the public-link rules, which refuse the
// same set for the same reason.
var platformPorts = map[int]struct{}{
	serviceshare.AgentBrowserPort: {},
	serviceshare.IDEProxyPort:     {},
	serviceshare.IDEDirectPort:    {},
	serviceshare.CDPPort:          {},
}

// FirstAppPort is firstAppPort for callers outside this package. The home
// dashboard shows a "preview is listening" chip for exactly the port the
// monitor probes, and one definition of "the project's own port" is the only
// way those two can never disagree.
func FirstAppPort(ports []int) (int, bool) {
	return firstAppPort(ports)
}

// firstAppPort returns the lowest listener that belongs to the project rather
// than to the platform, and whether there was one at all.
func firstAppPort(ports []int) (int, bool) {
	sorted := append([]int(nil), ports...)
	sort.Ints(sorted)
	for _, port := range sorted {
		if _, platform := platformPorts[port]; platform {
			continue
		}
		// Privileged ports (sshd on 22, in-container mail/DNS daemons) are
		// never previews: Caddy only routes ports >= 1024, so an HTTP probe
		// there would flag a healthy project as broken.
		if port < serviceshare.MinPort {
			continue
		}
		return port, true
	}
	return 0, false
}

// describeListeners names the processes an operator would recognize behind a
// project's open ports, so an alert says what is eating the memory and not
// only how much of it is gone.
func describeListeners(ports []int) string {
	sorted := append([]int(nil), ports...)
	sort.Ints(sorted)
	names := make([]string, 0, len(sorted))
	seen := map[string]struct{}{}
	for _, port := range sorted {
		name := listenerName(port)
		if name == "" {
			continue
		}
		if _, dupe := seen[name]; dupe {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return strings.Join(names, " + ")
}

func listenerName(port int) string {
	switch port {
	case serviceshare.AgentBrowserPort:
		return "agent browser"
	case serviceshare.IDEProxyPort, serviceshare.IDEDirectPort:
		return "code-server"
	case serviceshare.CDPPort:
		// The DevTools endpoint is the same Chromium as the agent browser;
		// naming it twice would overstate what is running.
		return ""
	default:
		return ":" + strconv.Itoa(port)
	}
}

// byteUnits are ordered largest first so a pair is rendered in the largest
// unit that leaves the limit with a whole digit.
var byteUnits = []struct {
	size   int64
	suffix string
}{
	{1 << 40, "TiB"},
	{1 << 30, "GiB"},
	{1 << 20, "MiB"},
	{1 << 10, "KiB"},
}

// formatBytePair renders a used/limit pair in one shared unit, chosen from the
// limit, so an alert reads "1.41/1.5 GiB" rather than "1.41GiB/1.5GiB".
func formatBytePair(used, limit int64) string {
	size, suffix := int64(1), "B"
	for _, unit := range byteUnits {
		if limit >= unit.size {
			size, suffix = unit.size, unit.suffix
			break
		}
	}
	return trimDecimals(used, size) + "/" + trimDecimals(limit, size) + " " + suffix
}

// trimDecimals renders bytes in units of size with up to two decimals and no
// trailing zeroes.
func trimDecimals(bytes, size int64) string {
	text := strconv.FormatFloat(float64(bytes)/float64(size), 'f', 2, 64)
	text = strings.TrimRight(text, "0")
	return strings.TrimSuffix(text, ".")
}
