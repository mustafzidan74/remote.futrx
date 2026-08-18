package chat

import (
	"strings"
	"time"
)

// Synthetic prompt kinds. They travel on Event.Synthetic and on the audit
// entry of the run they started, so a reader of either can tell an unattended
// round from something a human asked for.
const (
	// SyntheticAutopilot is a "keep going" round the autopilot loop sent.
	SyntheticAutopilot = "autopilot"
	// SyntheticAutoTest is a Playwright verification pass. The Test menu
	// sends the same kind when a human triggers a check by hand, because the
	// prompt and the badge are the same either way.
	SyntheticAutoTest = "autotest"
)

// Autopilot defaults and bounds. The defaults are what a chat gets when the
// operator flips the switch without touching the numbers; the bounds exist so
// a typo in the API cannot arm a loop that runs for a week.
const (
	DefaultAutopilotRounds      = 8
	DefaultAutopilotDurationMin = 120

	MinAutopilotRounds      = 1
	MaxAutopilotRounds      = 50
	MinAutopilotDurationMin = 5
	MaxAutopilotDurationMin = 1440
)

// NormalizeSynthetic maps an incoming synthetic label to one of the known
// kinds. Anything else collapses to "" — an unrecognized label must never
// make a prompt look platform-issued.
func NormalizeSynthetic(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case SyntheticAutopilot:
		return SyntheticAutopilot
	case SyntheticAutoTest:
		return SyntheticAutoTest
	default:
		return ""
	}
}

// ValidAutopilotInput reports whether the limits a caller supplied are inside
// the accepted bounds. Absent fields are always valid: they mean "leave it".
func ValidAutopilotInput(in AutopilotInput) bool {
	if in.MaxRounds != nil && (*in.MaxRounds < MinAutopilotRounds || *in.MaxRounds > MaxAutopilotRounds) {
		return false
	}
	if in.MaxDurationMin != nil &&
		(*in.MaxDurationMin < MinAutopilotDurationMin || *in.MaxDurationMin > MaxAutopilotDurationMin) {
		return false
	}
	return true
}

// NormalizeAutopilot fills in the defaults and clamps the limits of a stored
// policy. It runs on the way out of the store too, so a hand-edited meta.json
// or a document written before this field existed still yields a policy the
// driver can reason about.
func NormalizeAutopilot(policy AutopilotPolicy) AutopilotPolicy {
	policy.MaxRounds = clamp(policy.MaxRounds, DefaultAutopilotRounds, MinAutopilotRounds, MaxAutopilotRounds)
	policy.MaxDurationMin = clamp(
		policy.MaxDurationMin,
		DefaultAutopilotDurationMin,
		MinAutopilotDurationMin,
		MaxAutopilotDurationMin,
	)
	if policy.RoundsUsed < 0 {
		policy.RoundsUsed = 0
	}
	policy.EnabledBy = strings.TrimSpace(policy.EnabledBy)
	return policy
}

// NormalizeAutoTest trims the stored auto-test policy.
func NormalizeAutoTest(policy AutoTestPolicy) AutoTestPolicy {
	policy.EnabledBy = strings.TrimSpace(policy.EnabledBy)
	return policy
}

// ApplyAutopilot folds a patch into the stored policy.
//
// Arming the loop is the one transition that resets state: RoundsUsed goes
// back to zero and the clock restarts, so re-arming a chat that already spent
// its budget actually gives it a fresh one. Adjusting the limits of a running
// autopilot deliberately does not restart anything.
func ApplyAutopilot(policy AutopilotPolicy, in AutopilotInput, actorEmail string, now time.Time) AutopilotPolicy {
	policy = NormalizeAutopilot(policy)
	if in.MaxRounds != nil {
		policy.MaxRounds = clamp(*in.MaxRounds, DefaultAutopilotRounds, MinAutopilotRounds, MaxAutopilotRounds)
	}
	if in.MaxDurationMin != nil {
		policy.MaxDurationMin = clamp(
			*in.MaxDurationMin,
			DefaultAutopilotDurationMin,
			MinAutopilotDurationMin,
			MaxAutopilotDurationMin,
		)
	}
	if in.Enabled == nil || *in.Enabled == policy.Enabled {
		return policy
	}
	policy.Enabled = *in.Enabled
	if !policy.Enabled {
		return policy
	}
	policy.RoundsUsed = 0
	policy.StartedAt = now.UnixMilli()
	if actor := strings.TrimSpace(actorEmail); actor != "" {
		policy.EnabledBy = actor
	}
	return policy
}

// ApplyAutoTest folds a patch into the stored auto-test policy.
func ApplyAutoTest(policy AutoTestPolicy, in AutoTestInput, actorEmail string) AutoTestPolicy {
	policy = NormalizeAutoTest(policy)
	if in.Enabled == nil {
		return policy
	}
	policy.Enabled = *in.Enabled
	if policy.Enabled {
		if actor := strings.TrimSpace(actorEmail); actor != "" {
			policy.EnabledBy = actor
		}
	}
	return policy
}

// PolicyActor is the human a synthetic run is attributed to: whoever armed the
// policy that caused it, falling back to the other policy's owner so a chat
// where only auto-test was armed still names someone.
func (m Meta) PolicyActor() string {
	if actor := strings.TrimSpace(m.Autopilot.EnabledBy); actor != "" {
		return actor
	}
	return strings.TrimSpace(m.AutoTest.EnabledBy)
}

func clamp(value, fallback, low, high int) int {
	if value == 0 {
		return fallback
	}
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}
