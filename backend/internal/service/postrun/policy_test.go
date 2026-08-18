package postrun

import (
	"strings"
	"testing"
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

var armedAt = time.Date(2026, 8, 18, 12, 40, 0, 0, time.UTC)

func autopilotMeta(mutate func(*servicechat.Meta)) servicechat.Meta {
	meta := servicechat.Meta{
		ID: "abcd1234",
		Autopilot: servicechat.AutopilotPolicy{
			Enabled:        true,
			MaxRounds:      8,
			MaxDurationMin: 120,
			StartedAt:      armedAt.UnixMilli(),
			EnabledBy:      "operator@example.com",
		},
	}
	if mutate != nil {
		mutate(&meta)
	}
	return meta
}

func TestDecide(t *testing.T) {
	tests := []struct {
		name       string
		meta       servicechat.Meta
		outcome    Outcome
		conditions Conditions
		want       Action
		wantReason Reason
		wantDetail string
		wantKind   string
	}{
		{
			name:    "idle chat with no policy does nothing",
			meta:    servicechat.Meta{ID: "abcd1234"},
			outcome: Outcome{Output: "all set"},
			want:    ActionNone,
		},
		{
			name:     "autopilot continues when the agent just ends a turn",
			meta:     autopilotMeta(nil),
			outcome:  Outcome{Output: "Refactored the handler. Next I will add tests."},
			want:     ActionContinue,
			wantKind: servicechat.SyntheticAutopilot,
		},
		{
			name:       "done marker stops the loop",
			meta:       autopilotMeta(nil),
			outcome:    Outcome{Output: "Shipped and verified.\n<<DONE>>"},
			want:       ActionStop,
			wantReason: ReasonDone,
		},
		{
			name:       "blocked marker stops the loop and keeps the reason",
			meta:       autopilotMeta(nil),
			outcome:    Outcome{Output: "I need the staging API key.\n<<BLOCKED: missing STRIPE_KEY secret>>"},
			want:       ActionStop,
			wantReason: ReasonBlocked,
			wantDetail: "missing STRIPE_KEY secret",
		},
		{
			name: "a quoted marker mid-message does not stop the loop",
			meta: autopilotMeta(nil),
			outcome: Outcome{
				Output: "I will end with <<DONE>> once the migration lands.\nStill working on step 2.",
			},
			want:     ActionContinue,
			wantKind: servicechat.SyntheticAutopilot,
		},
		{
			name:       "spent round budget stops the loop",
			meta:       autopilotMeta(func(m *servicechat.Meta) { m.Autopilot.RoundsUsed = 8 }),
			outcome:    Outcome{Output: "still going"},
			want:       ActionStop,
			wantReason: ReasonRounds,
		},
		{
			name:       "spent time budget stops the loop",
			meta:       autopilotMeta(nil),
			outcome:    Outcome{Output: "still going"},
			conditions: Conditions{Now: armedAt.Add(121 * time.Minute)},
			want:       ActionStop,
			wantReason: ReasonDuration,
		},
		{
			name:       "a failed run stops the loop instead of retrying",
			meta:       autopilotMeta(nil),
			outcome:    Outcome{Failed: true},
			want:       ActionStop,
			wantReason: ReasonFailed,
		},
		{
			name:    "a failed run in a chat without autopilot does nothing",
			meta:    servicechat.Meta{ID: "abcd1234", AutoTest: servicechat.AutoTestPolicy{Enabled: true}},
			outcome: Outcome{Failed: true},
			want:    ActionNone,
		},
		{
			name:       "an active run defers the decision",
			meta:       autopilotMeta(nil),
			outcome:    Outcome{Output: "more to do"},
			conditions: Conditions{RunActive: true, Now: armedAt},
			want:       ActionNone,
		},
		{
			name:       "a scheduled chat is left to the scheduler",
			meta:       autopilotMeta(nil),
			outcome:    Outcome{Output: "more to do"},
			conditions: Conditions{ScheduledChat: true, Now: armedAt},
			want:       ActionNone,
		},
		{
			name:    "a scheduled run is left to the schedule service",
			meta:    autopilotMeta(nil),
			outcome: Outcome{Output: "more to do", Scheduled: true},
			want:    ActionNone,
		},
		{
			name:     "auto-test fires after a human turn",
			meta:     servicechat.Meta{ID: "abcd1234", AutoTest: servicechat.AutoTestPolicy{Enabled: true}},
			outcome:  Outcome{Output: "Added the checkout button."},
			want:     ActionTest,
			wantKind: servicechat.SyntheticAutoTest,
		},
		{
			name:    "auto-test does not chain onto its own run",
			meta:    servicechat.Meta{ID: "abcd1234", AutoTest: servicechat.AutoTestPolicy{Enabled: true}},
			outcome: Outcome{Output: "PASS", Synthetic: servicechat.SyntheticAutoTest},
			want:    ActionNone,
		},
		{
			name: "with both armed the test runs before the next round",
			meta: autopilotMeta(func(m *servicechat.Meta) {
				m.AutoTest = servicechat.AutoTestPolicy{Enabled: true}
			}),
			outcome:  Outcome{Output: "Step 1 done."},
			want:     ActionTest,
			wantKind: servicechat.SyntheticAutoTest,
		},
		{
			name: "with both armed the round follows the settled test",
			meta: autopilotMeta(func(m *servicechat.Meta) {
				m.AutoTest = servicechat.AutoTestPolicy{Enabled: true}
			}),
			outcome:  Outcome{Output: "PASS", Synthetic: servicechat.SyntheticAutoTest},
			want:     ActionContinue,
			wantKind: servicechat.SyntheticAutopilot,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			conditions := test.conditions
			if conditions.Now.IsZero() {
				conditions.Now = armedAt.Add(time.Minute)
			}

			got := Decide(test.meta, test.outcome, conditions)

			if got.Action != test.want {
				t.Fatalf("action = %q, want %q", got.Action, test.want)
			}
			if got.Reason != test.wantReason {
				t.Errorf("reason = %q, want %q", got.Reason, test.wantReason)
			}
			if got.Detail != test.wantDetail {
				t.Errorf("detail = %q, want %q", got.Detail, test.wantDetail)
			}
			if got.Synthetic != test.wantKind {
				t.Errorf("synthetic = %q, want %q", got.Synthetic, test.wantKind)
			}
		})
	}
}

// The continue prompt must carry the marker instruction, because the marker is
// the only signal that ends the loop early.
func TestDecideContinueCarriesMarkerInstruction(t *testing.T) {
	got := Decide(autopilotMeta(nil), Outcome{Output: "working"}, Conditions{Now: armedAt})

	if !strings.Contains(got.Prompt, ContinuePrompt) {
		t.Errorf("continue prompt missing the instruction body: %q", got.Prompt)
	}
	if !strings.Contains(got.Prompt, DoneMarker) {
		t.Errorf("continue prompt does not ask for the done marker: %q", got.Prompt)
	}
	if !strings.Contains(got.Prompt, "<<BLOCKED:") {
		t.Errorf("continue prompt does not ask for the blocked marker: %q", got.Prompt)
	}
}

func TestDecideTestPromptForbidsLooseningAssertions(t *testing.T) {
	meta := servicechat.Meta{ID: "abcd1234", AutoTest: servicechat.AutoTestPolicy{Enabled: true}}

	got := Decide(meta, Outcome{Output: "changed a thing"}, Conditions{Now: armedAt})

	if !strings.Contains(got.Prompt, "Do not loosen assertions to pass.") {
		t.Errorf("auto-test prompt lost its anti-cheat clause: %q", got.Prompt)
	}
	if !strings.Contains(got.Prompt, "playwright-e2e") {
		t.Errorf("auto-test prompt does not name the skill: %q", got.Prompt)
	}
}

// A policy stored before the limits existed still has to be decidable: the
// defaults come from normalization, not from the caller.
func TestDecideAppliesDefaultLimitsToAnUnboundedPolicy(t *testing.T) {
	meta := servicechat.Meta{
		ID:        "abcd1234",
		Autopilot: servicechat.AutopilotPolicy{Enabled: true, RoundsUsed: 8, StartedAt: armedAt.UnixMilli()},
	}

	got := Decide(meta, Outcome{Output: "working"}, Conditions{Now: armedAt.Add(time.Minute)})

	if got.Action != ActionStop || got.Reason != ReasonRounds {
		t.Fatalf("action = %q reason = %q, want stop/rounds at the default round cap", got.Action, got.Reason)
	}
}

func TestStopSummary(t *testing.T) {
	tests := []struct {
		name   string
		rounds int
		reason Reason
		detail string
		want   string
	}{
		{
			name:   "done",
			rounds: 3,
			reason: ReasonDone,
			want:   "Autopilot finished after 3 rounds — the agent reported the task complete.",
		},
		{
			name:   "one round reads naturally",
			rounds: 1,
			reason: ReasonRounds,
			want:   "Autopilot finished after 1 round — the round limit was reached before the agent reported completion.",
		},
		{
			name:   "blocked keeps the agent's reason",
			rounds: 2,
			reason: ReasonBlocked,
			detail: "needs a database password",
			want:   "Autopilot finished after 2 rounds — the agent is blocked and needs you.\nneeds a database password",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := StopSummary(test.rounds, test.reason, test.detail); got != test.want {
				t.Errorf("StopSummary() = %q, want %q", got, test.want)
			}
		})
	}
}

// Team mode and autopilot both react to the same settled run and both would
// prompt the same chat. While a team loop has a hop in flight its next prompt
// is already decided, so autopilot stands down rather than racing it — and
// picks up again once the loop settles.
func TestDecideStandsDownWhileATeamLoopIsInFlight(t *testing.T) {
	tests := []struct {
		phase string
		want  Action
	}{
		{phase: servicechat.TeamPhaseReviewing, want: ActionNone},
		{phase: servicechat.TeamPhaseTesting, want: ActionNone},
		{phase: servicechat.TeamPhaseFixing, want: ActionNone},
		{phase: servicechat.TeamPhaseDone, want: ActionContinue},
		{phase: servicechat.TeamPhaseError, want: ActionContinue},
		{phase: servicechat.TeamPhaseIdle, want: ActionContinue},
	}

	for _, test := range tests {
		t.Run("phase "+test.phase, func(t *testing.T) {
			meta := autopilotMeta(func(m *servicechat.Meta) {
				m.Team = servicechat.TeamPolicy{Enabled: true, Phase: test.phase}
			})

			decision := Decide(meta, Outcome{Output: "ended a step"}, Conditions{Now: armedAt})

			if decision.Action != test.want {
				t.Fatalf("action = %q, want %q", decision.Action, test.want)
			}
		})
	}

	// A chat with team mode switched off is never held back by a stale phase.
	meta := autopilotMeta(func(m *servicechat.Meta) {
		m.Team = servicechat.TeamPolicy{Phase: servicechat.TeamPhaseReviewing}
	})
	if decision := Decide(meta, Outcome{Output: "ended a step"}, Conditions{Now: armedAt}); decision.Action != ActionContinue {
		t.Errorf("action = %q, want autopilot to run", decision.Action)
	}
}
