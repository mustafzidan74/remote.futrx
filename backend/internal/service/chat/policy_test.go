package chat

import (
	"testing"
	"time"
)

func boolPtr(value bool) *bool { return &value }
func intPtr(value int) *int    { return &value }

func TestApplyAutopilot(t *testing.T) {
	armedAt := time.Date(2026, 8, 18, 12, 40, 0, 0, time.UTC)
	later := armedAt.Add(90 * time.Minute)

	tests := []struct {
		name  string
		start AutopilotPolicy
		in    AutopilotInput
		actor string
		now   time.Time
		want  AutopilotPolicy
	}{
		{
			name:  "arming an untouched chat takes the documented defaults",
			in:    AutopilotInput{Enabled: boolPtr(true)},
			actor: "operator@example.com",
			now:   armedAt,
			want: AutopilotPolicy{
				Enabled:        true,
				MaxRounds:      DefaultAutopilotRounds,
				MaxDurationMin: DefaultAutopilotDurationMin,
				StartedAt:      armedAt.UnixMilli(),
				EnabledBy:      "operator@example.com",
			},
		},
		{
			name: "re-arming a spent loop resets the budget and the clock",
			start: AutopilotPolicy{
				MaxRounds:      4,
				MaxDurationMin: 30,
				RoundsUsed:     4,
				StartedAt:      armedAt.UnixMilli(),
				EnabledBy:      "operator@example.com",
			},
			in:    AutopilotInput{Enabled: boolPtr(true)},
			actor: "operator@example.com",
			now:   later,
			want: AutopilotPolicy{
				Enabled:        true,
				MaxRounds:      4,
				MaxDurationMin: 30,
				RoundsUsed:     0,
				StartedAt:      later.UnixMilli(),
				EnabledBy:      "operator@example.com",
			},
		},
		{
			name: "raising the limits mid-flight does not restart the clock",
			start: AutopilotPolicy{
				Enabled:        true,
				MaxRounds:      4,
				MaxDurationMin: 30,
				RoundsUsed:     3,
				StartedAt:      armedAt.UnixMilli(),
				EnabledBy:      "operator@example.com",
			},
			in:  AutopilotInput{MaxRounds: intPtr(12), MaxDurationMin: intPtr(240)},
			now: later,
			want: AutopilotPolicy{
				Enabled:        true,
				MaxRounds:      12,
				MaxDurationMin: 240,
				RoundsUsed:     3,
				StartedAt:      armedAt.UnixMilli(),
				EnabledBy:      "operator@example.com",
			},
		},
		{
			name: "stopping keeps the counters so the UI can still explain itself",
			start: AutopilotPolicy{
				Enabled:        true,
				MaxRounds:      8,
				MaxDurationMin: 120,
				RoundsUsed:     5,
				StartedAt:      armedAt.UnixMilli(),
				EnabledBy:      "operator@example.com",
			},
			in:  AutopilotInput{Enabled: boolPtr(false)},
			now: later,
			want: AutopilotPolicy{
				Enabled:        false,
				MaxRounds:      8,
				MaxDurationMin: 120,
				RoundsUsed:     5,
				StartedAt:      armedAt.UnixMilli(),
				EnabledBy:      "operator@example.com",
			},
		},
		{
			name:  "out-of-range limits are clamped rather than trusted",
			in:    AutopilotInput{Enabled: boolPtr(true), MaxRounds: intPtr(9000), MaxDurationMin: intPtr(1)},
			actor: "operator@example.com",
			now:   armedAt,
			want: AutopilotPolicy{
				Enabled:        true,
				MaxRounds:      MaxAutopilotRounds,
				MaxDurationMin: MinAutopilotDurationMin,
				StartedAt:      armedAt.UnixMilli(),
				EnabledBy:      "operator@example.com",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := ApplyAutopilot(test.start, test.in, test.actor, test.now)
			if got != test.want {
				t.Errorf("ApplyAutopilot() = %+v, want %+v", got, test.want)
			}
		})
	}
}

func TestApplyAutoTest(t *testing.T) {
	enabled := ApplyAutoTest(AutoTestPolicy{}, AutoTestInput{Enabled: boolPtr(true)}, "qa@example.com")
	if !enabled.Enabled || enabled.EnabledBy != "qa@example.com" {
		t.Fatalf("ApplyAutoTest() = %+v, want it armed and attributed", enabled)
	}

	// Disarming keeps the attribution: it is a record of who armed it last,
	// not a claim about the current state.
	disabled := ApplyAutoTest(enabled, AutoTestInput{Enabled: boolPtr(false)}, "someone@example.com")
	if disabled.Enabled || disabled.EnabledBy != "qa@example.com" {
		t.Errorf("ApplyAutoTest() = %+v, want it disarmed with the original owner", disabled)
	}
}

func TestValidAutopilotInput(t *testing.T) {
	tests := []struct {
		name string
		in   AutopilotInput
		want bool
	}{
		{name: "an empty patch is valid", in: AutopilotInput{}, want: true},
		{name: "a bare toggle is valid", in: AutopilotInput{Enabled: boolPtr(true)}, want: true},
		{name: "limits at the bounds are valid", in: AutopilotInput{
			MaxRounds:      intPtr(MaxAutopilotRounds),
			MaxDurationMin: intPtr(MinAutopilotDurationMin),
		}, want: true},
		{name: "zero rounds is rejected", in: AutopilotInput{MaxRounds: intPtr(0)}, want: false},
		{name: "negative rounds is rejected", in: AutopilotInput{MaxRounds: intPtr(-1)}, want: false},
		{name: "too many rounds is rejected", in: AutopilotInput{MaxRounds: intPtr(51)}, want: false},
		{name: "too short a window is rejected", in: AutopilotInput{MaxDurationMin: intPtr(4)}, want: false},
		{name: "too long a window is rejected", in: AutopilotInput{MaxDurationMin: intPtr(1441)}, want: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidAutopilotInput(test.in); got != test.want {
				t.Errorf("ValidAutopilotInput(%+v) = %v, want %v", test.in, got, test.want)
			}
		})
	}
}

func TestNormalizeSynthetic(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "autopilot", want: SyntheticAutopilot},
		{in: " AutoTest ", want: SyntheticAutoTest},
		{in: "", want: ""},
		{in: "trust-me", want: ""},
	}

	for _, test := range tests {
		t.Run(test.in, func(t *testing.T) {
			if got := NormalizeSynthetic(test.in); got != test.want {
				t.Errorf("NormalizeSynthetic(%q) = %q, want %q", test.in, got, test.want)
			}
		})
	}
}

func TestPolicyActorFallsBackToTheAutoTestOwner(t *testing.T) {
	meta := Meta{AutoTest: AutoTestPolicy{Enabled: true, EnabledBy: "qa@example.com"}}
	if got := meta.PolicyActor(); got != "qa@example.com" {
		t.Errorf("PolicyActor() = %q, want the auto-test owner", got)
	}

	meta.Autopilot.EnabledBy = "operator@example.com"
	if got := meta.PolicyActor(); got != "operator@example.com" {
		t.Errorf("PolicyActor() = %q, want the autopilot owner to win", got)
	}
}
