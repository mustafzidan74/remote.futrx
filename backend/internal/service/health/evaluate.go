package health

import (
	"fmt"
	"strconv"
	"strings"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

// Signals is everything one sweep measured about a single project, in the
// order of severity the evaluator reads them. Keeping the inputs in a plain
// struct is what makes the threshold rules testable without LXD.
type Signals struct {
	// ProjectStatus is the stored lifecycle status; ContainerState is what
	// LXD reports right now. They disagree when a container died under a
	// project the platform still believes is running.
	ProjectStatus  serviceproject.Status
	ContainerState serviceproject.ContainerState

	MemoryUsedBytes  int64
	MemoryLimitBytes int64

	// Listeners are every reachable port, platform plumbing included.
	Listeners []int
	// Preview is the outcome of the HTTP probe. Attempted is false when the
	// container exposed no application port to probe.
	Preview Probe
}

// Probe is the outcome of one preview request against a project's first
// application port.
type Probe struct {
	Attempted  bool
	Port       int
	StatusCode int
	// Err is set when the request never produced a response: connection
	// refused, DNS failure, or the 3 second deadline.
	Err error
}

// Result is the verdict of one evaluation: the status the signals justify and
// the human-readable reasons behind it, most severe first.
type Result struct {
	Status    Status
	Reasons   []string
	MemoryPct float64
	// PreviewOK mirrors Probe: nil when nothing was probed.
	PreviewOK *bool
}

// Evaluate applies the threshold rules to one sweep's signals. It is pure: the
// same signals always produce the same verdict, which is what lets the monitor
// stay a thin loop around it.
//
// Severity is resolved by taking the worst of the independent rules rather
// than by first match, so a project that is both out of memory and serving
// errors reports both reasons under the more severe status.
func Evaluate(signals Signals) Result {
	out := Result{Status: StatusUnknown}
	var critical, warnings []string

	switch {
	case signals.ProjectStatus == serviceproject.StatusError:
		critical = append(critical, "the project is in an error state")
	case signals.ContainerState == serviceproject.ContainerStateMissing:
		critical = append(critical, "the container is missing")
	}

	measured := false
	if signals.MemoryLimitBytes > 0 && signals.MemoryUsedBytes > 0 {
		measured = true
		out.MemoryPct = round1(float64(signals.MemoryUsedBytes) / float64(signals.MemoryLimitBytes) * 100)
		reason := memoryReason(out.MemoryPct, signals.MemoryUsedBytes, signals.MemoryLimitBytes)
		switch {
		case out.MemoryPct >= CritMemoryPercent:
			critical = append(critical, reason)
		case out.MemoryPct >= WarnMemoryPercent:
			warnings = append(warnings, reason)
		}
	}

	if signals.Preview.Attempted {
		measured = true
		port := strconv.Itoa(signals.Preview.Port)
		switch {
		case signals.Preview.Err != nil:
			// A listener exists by construction — the probe only runs when
			// one was found — so an unreachable port means the app is wedged,
			// not merely absent.
			ok := false
			out.PreviewOK = &ok
			critical = append(critical, "the app on port "+port+" is not answering")
		case signals.Preview.StatusCode >= 500:
			ok := false
			out.PreviewOK = &ok
			warnings = append(
				warnings,
				"the app on port "+port+" returned HTTP "+strconv.Itoa(signals.Preview.StatusCode),
			)
		default:
			ok := true
			out.PreviewOK = &ok
		}
	}

	switch {
	case len(critical) > 0:
		out.Status = StatusCrit
	case len(warnings) > 0:
		out.Status = StatusWarn
	case measured:
		out.Status = StatusOK
	}
	out.Reasons = append(critical, warnings...)
	return out
}

// hysteresisChecks is how many consecutive sweeps must agree before a project
// changes status. Two is enough to absorb a single failed lxc call or a
// momentary allocation spike without hiding a real regression for more than
// one extra interval.
const hysteresisChecks = 2

// stateMachine debounces one project's status. It is the only thing standing
// between a container that hovers on 80% memory and a phone that buzzes every
// minute.
type stateMachine struct {
	// published is the status currently shown and last alerted on. The empty
	// string means the project has never been evaluated.
	published Status
	candidate Status
	streak    int
}

// Observe folds one evaluation into the machine and reports the status to
// publish now, plus whether that publication is a transition worth telling a
// human about.
func (m *stateMachine) Observe(observed Status) (Status, bool) {
	if observed == m.published {
		m.candidate, m.streak = "", 0
		return m.published, false
	}
	if m.candidate == observed {
		m.streak++
	} else {
		m.candidate, m.streak = observed, 1
	}
	// A project seen for the first time has nothing to flap between, so its
	// first reading is adopted without waiting for confirmation.
	if m.published != "" && m.streak < hysteresisChecks {
		return m.published, false
	}
	previous := m.published
	m.published, m.candidate, m.streak = observed, "", 0
	return m.published, alertWorthy(previous, observed)
}

// alertWorthy decides which published transitions become notifications. Every
// step into a degraded status is worth one, and so is the recovery out of one;
// the "unknown" status never is, because losing contact with LXD for a sweep
// is an operational blip rather than a project-level event.
func alertWorthy(previous, next Status) bool {
	switch next {
	case StatusWarn, StatusCrit:
		return previous != next
	case StatusOK:
		return previous == StatusWarn || previous == StatusCrit
	default:
		return false
	}
}

// AlertSummary renders the one-line body of a health notification: which
// project, what tipped it over (or that it recovered), and what is running
// inside that could explain it.
func AlertSummary(projectName string, health ProjectHealth) string {
	detail := strings.Join(health.Reasons, "; ")
	if detail == "" {
		detail = describeMemory(health)
	}
	if health.Status == StatusOK {
		detail = "back to normal, " + detail
	}
	summary := projectName + " " + detail
	if running := describeListeners(health.Listeners); running != "" {
		summary += " — " + running + " running"
	}
	return summary
}

// describeMemory is the fallback detail for a project with nothing left to
// complain about, which is what a recovery notification looks like.
func describeMemory(health ProjectHealth) string {
	if health.MemoryLimitBytes <= 0 {
		return "all health checks are passing"
	}
	return memoryReason(health.MemoryPct, health.MemoryUsedBytes, health.MemoryLimitBytes)
}

// memoryReason is the one phrase every memory-shaped message shares, so a
// tooltip and a Telegram message never word the same number differently.
func memoryReason(percent float64, used, limit int64) string {
	return fmt.Sprintf(
		"memory %s%% (%s)",
		strconv.FormatFloat(percent, 'f', -1, 64),
		formatBytePair(used, limit),
	)
}

// round1 keeps a percentage at one decimal: enough to see 92.4 cross a
// threshold, few enough digits to read in a sidebar tooltip.
func round1(value float64) float64 {
	return float64(int64(value*10+0.5)) / 10
}
