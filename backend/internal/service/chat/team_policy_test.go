package chat

import (
	"testing"
	"time"
)

func providerPtr(value Provider) *Provider { return &value }

func TestValidTeamInputBounds(t *testing.T) {
	tests := []struct {
		name  string
		in    TeamInput
		valid bool
	}{
		{name: "a bare toggle is accepted", in: TeamInput{Enabled: boolPtr(true)}, valid: true},
		{name: "the default loop count", in: TeamInput{MaxLoops: intPtr(DefaultTeamLoops)}, valid: true},
		{name: "the floor", in: TeamInput{MaxLoops: intPtr(MinTeamLoops)}, valid: true},
		{name: "the cap", in: TeamInput{MaxLoops: intPtr(MaxTeamLoops)}, valid: true},
		{name: "zero loops", in: TeamInput{MaxLoops: intPtr(0)}},
		{name: "above the cap", in: TeamInput{MaxLoops: intPtr(MaxTeamLoops + 1)}},
		{
			name: "a known provider on a seat",
			in: TeamInput{Roles: &TeamRolesInput{
				Reviewer: &TeamRoleInput{Provider: providerPtr(ProviderKimi)},
			}},
			valid: true,
		},
		{
			name: "an empty provider hands the choice back to the platform",
			in: TeamInput{Roles: &TeamRolesInput{
				Reviewer: &TeamRoleInput{Provider: providerPtr("")},
			}},
			valid: true,
		},
		{
			name: "an unknown provider is refused rather than defaulted",
			in: TeamInput{Roles: &TeamRolesInput{
				Tester: &TeamRoleInput{Provider: providerPtr("gpt5")},
			}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := ValidTeamInput(test.in); got != test.valid {
				t.Fatalf("ValidTeamInput = %v, want %v", got, test.valid)
			}
		})
	}
}

func TestNormalizeTeamFillsDefaults(t *testing.T) {
	normalized := NormalizeTeam(TeamPolicy{
		Enabled:   true,
		LoopsUsed: -3,
		Phase:     "REVIEWING",
		Verdict:   "SHIP",
		Roles: TeamRoles{
			Implementer: TeamRole{Provider: "  CLAUDE ", Enabled: false, ChatID: "leftover"},
			Reviewer:    TeamRole{Provider: "unknown"},
		},
	})

	if normalized.MaxLoops != DefaultTeamLoops {
		t.Errorf("maxLoops = %d, want the default", normalized.MaxLoops)
	}
	if normalized.LoopsUsed != 0 {
		t.Errorf("loopsUsed = %d, want a negative count clamped to zero", normalized.LoopsUsed)
	}
	if normalized.Phase != TeamPhaseReviewing || normalized.Verdict != TeamVerdictShip {
		t.Errorf("phase=%q verdict=%q", normalized.Phase, normalized.Verdict)
	}
	if normalized.Roles.Implementer.Provider != ProviderClaude {
		t.Errorf("implementer provider = %q", normalized.Roles.Implementer.Provider)
	}
	// The implementer is the chat itself: it can never be switched off, and it
	// never owns a companion chat.
	if !normalized.Roles.Implementer.Enabled || normalized.Roles.Implementer.ChatID != "" {
		t.Errorf("implementer seat = %+v", normalized.Roles.Implementer)
	}
	// An unrecognized provider is "unset", not Codex: the team service must be
	// free to pick a genuine second opinion.
	if normalized.Roles.Reviewer.Provider != "" {
		t.Errorf("reviewer provider = %q, want unset", normalized.Roles.Reviewer.Provider)
	}

	if phase := NormalizeTeamPhase("wandering"); phase != TeamPhaseIdle {
		t.Errorf("unknown phase = %q, want idle", phase)
	}
	if verdict := NormalizeTeamVerdict("probably"); verdict != "" {
		t.Errorf("unknown verdict = %q, want empty", verdict)
	}
}

func TestApplyTeamArmsResetsAndPreservesCompanions(t *testing.T) {
	now := time.UnixMilli(1_700_000_000_000)
	stored := TeamPolicy{
		Enabled:   true,
		MaxLoops:  3,
		AutoFix:   true,
		LoopsUsed: 2,
		Phase:     TeamPhaseDone,
		Verdict:   TeamVerdictPass,
		Hops:      []TeamHop{{Role: TeamRoleReviewer}},
		EnabledBy: "first@example.com",
		Roles: TeamRoles{
			Reviewer: TeamRole{Provider: ProviderCodex, Enabled: true, ChatID: "reviewchat"},
			Tester:   TeamRole{Provider: ProviderCodex, Enabled: true, ChatID: "testchat"},
		},
	}

	// Adjusting a limit mid-session must not restart anything.
	adjusted := ApplyTeam(stored, TeamInput{MaxLoops: intPtr(5)}, "second@example.com", now)
	if adjusted.MaxLoops != 5 || adjusted.LoopsUsed != 2 || adjusted.Phase != TeamPhaseDone {
		t.Fatalf("adjusted = %+v", adjusted)
	}
	if adjusted.EnabledBy != "first@example.com" {
		t.Errorf("enabledBy = %q, want whoever armed it kept", adjusted.EnabledBy)
	}

	off := ApplyTeam(adjusted, TeamInput{Enabled: boolPtr(false)}, "second@example.com", now)
	if off.Enabled || off.Phase != TeamPhaseIdle {
		t.Fatalf("off = %+v", off)
	}
	if off.LoopsUsed != 2 {
		t.Errorf("loopsUsed = %d, want the spent count kept for display", off.LoopsUsed)
	}

	on := ApplyTeam(off, TeamInput{Enabled: boolPtr(true)}, "second@example.com", now)
	if !on.Enabled || on.LoopsUsed != 0 || on.Verdict != "" || len(on.Hops) != 0 {
		t.Fatalf("re-armed = %+v, want a fresh session", on)
	}
	if on.EnabledBy != "second@example.com" {
		t.Errorf("enabledBy = %q, want whoever re-armed it", on.EnabledBy)
	}
	// The companion chats survive, so a second session reuses the reviewer's
	// thread rather than littering the project with a chat per session.
	if on.Roles.Reviewer.ChatID != "reviewchat" || on.Roles.Tester.ChatID != "testchat" {
		t.Errorf("roles = %+v, want the companion chats kept", on.Roles)
	}
}

// Changing a seat's provider invalidates its chat: that chat's provider,
// session, and skills all belong to the agent that is being replaced.
func TestApplyTeamDropsTheCompanionWhenTheProviderChanges(t *testing.T) {
	stored := TeamPolicy{
		Enabled: true,
		Roles: TeamRoles{
			Reviewer: TeamRole{Provider: ProviderCodex, Enabled: true, ChatID: "reviewchat"},
		},
	}

	same := ApplyTeam(stored, TeamInput{Roles: &TeamRolesInput{
		Reviewer: &TeamRoleInput{Provider: providerPtr(ProviderCodex)},
	}}, "", time.Now())
	if same.Roles.Reviewer.ChatID != "reviewchat" {
		t.Errorf("an unchanged provider dropped the companion chat")
	}

	moved := ApplyTeam(stored, TeamInput{Roles: &TeamRolesInput{
		Reviewer: &TeamRoleInput{Provider: providerPtr(ProviderKimi)},
	}}, "", time.Now())
	if moved.Roles.Reviewer.ChatID != "" {
		t.Errorf("chatId = %q, want the stale companion dropped", moved.Roles.Reviewer.ChatID)
	}
	if moved.Roles.Reviewer.Provider != ProviderKimi {
		t.Errorf("provider = %q", moved.Roles.Reviewer.Provider)
	}
}

func TestTeamActiveOnlyWhileAHopIsInFlight(t *testing.T) {
	tests := []struct {
		phase  string
		active bool
	}{
		{phase: TeamPhaseIdle},
		{phase: TeamPhaseReviewing, active: true},
		{phase: TeamPhaseTesting, active: true},
		{phase: TeamPhaseFixing, active: true},
		{phase: TeamPhaseDone},
		{phase: TeamPhaseError},
	}

	for _, test := range tests {
		t.Run("phase "+test.phase, func(t *testing.T) {
			if got := TeamActive(TeamPolicy{Enabled: true, Phase: test.phase}); got != test.active {
				t.Fatalf("TeamActive = %v, want %v", got, test.active)
			}
		})
	}
	if TeamActive(TeamPolicy{Phase: TeamPhaseReviewing}) {
		t.Errorf("a switched-off team is never active")
	}
}

func TestAppendTeamHopBoundsTheTimeline(t *testing.T) {
	var hops []TeamHop
	for index := 0; index < MaxTeamHops+7; index++ {
		hops = AppendTeamHop(hops, TeamHop{Loop: index, Role: TeamRoleReviewer, Verdict: "SHIP"})
	}

	if len(hops) != MaxTeamHops {
		t.Fatalf("kept %d hops, want %d", len(hops), MaxTeamHops)
	}
	if hops[len(hops)-1].Loop != MaxTeamHops+6 {
		t.Errorf("the newest hop was dropped: %+v", hops[len(hops)-1])
	}
	if hops[0].Verdict != TeamVerdictShip {
		t.Errorf("verdict = %q, want it normalized on the way in", hops[0].Verdict)
	}
}

func TestNormalizeSyntheticKnowsTheTeamKinds(t *testing.T) {
	tests := map[string]string{
		"team-review":  SyntheticTeamReview,
		"TEAM-TEST":    SyntheticTeamTest,
		" team-fix ":   SyntheticTeamFix,
		"team-summary": SyntheticTeamSummary,
		"autopilot":    SyntheticAutopilot,
		"team-deploy":  "",
	}

	for input, want := range tests {
		if got := NormalizeSynthetic(input); got != want {
			t.Errorf("NormalizeSynthetic(%q) = %q, want %q", input, got, want)
		}
	}
}

// A synthetic team run is attributed to whoever switched team mode on, even in
// a chat where neither post-run policy was ever armed.
func TestPolicyActorFallsBackToTheTeamOwner(t *testing.T) {
	meta := Meta{Team: TeamPolicy{EnabledBy: "operator@example.com"}}

	if got := meta.PolicyActor(); got != "operator@example.com" {
		t.Fatalf("PolicyActor = %q", got)
	}
}

// A browser may badge one thing: its own Playwright check. Every other kind is
// platform-issued, and the team hops in particular are read back by the
// orchestrator to decide where the loop goes — a client that could send
// `team-review` could forge a verdict for its own review loop.
func TestNormalizeClientSyntheticAcceptsOnlyTheTestLabel(t *testing.T) {
	if got := NormalizeClientSynthetic(SyntheticAutoTest); got != SyntheticAutoTest {
		t.Fatalf("NormalizeClientSynthetic(%q) = %q", SyntheticAutoTest, got)
	}
	for _, kind := range []string{
		SyntheticAutopilot,
		SyntheticTeamReview,
		SyntheticTeamTest,
		SyntheticTeamFix,
		SyntheticTeamSummary,
		"anything-else",
		"",
	} {
		if got := NormalizeClientSynthetic(kind); got != "" {
			t.Errorf("NormalizeClientSynthetic(%q) = %q, want it refused", kind, got)
		}
	}
}
