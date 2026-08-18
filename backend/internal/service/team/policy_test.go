package team

import (
	"strings"
	"testing"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

func reviewSignal(output string) Signal {
	return Signal{
		Role:      servicechat.TeamRoleReviewer,
		Output:    output,
		Synthetic: servicechat.SyntheticTeamReview,
	}
}

func testSignal(output string) Signal {
	return Signal{
		Role:      servicechat.TeamRoleTester,
		Output:    output,
		Synthetic: servicechat.SyntheticTeamTest,
	}
}

func armedPolicy() servicechat.TeamPolicy {
	return servicechat.TeamPolicy{
		Enabled:  true,
		MaxLoops: 2,
		AutoFix:  true,
		Roles: servicechat.TeamRoles{
			Implementer: servicechat.TeamRole{Provider: servicechat.ProviderClaude, Enabled: true},
			Reviewer:    servicechat.TeamRole{Provider: servicechat.ProviderCodex, Enabled: true},
			Tester:      servicechat.TeamRole{Provider: servicechat.ProviderCodex, Enabled: true},
		},
	}
}

func TestDecideWalksTheLoop(t *testing.T) {
	tests := []struct {
		name          string
		policy        func(servicechat.TeamPolicy) servicechat.TeamPolicy
		signal        Signal
		wantAction    Action
		wantPhase     string
		wantSynthetic string
		wantLoops     int
		wantVerdict   string
	}{
		{
			name:          "a human prompt opens the loop with a review",
			signal:        Signal{Role: servicechat.TeamRoleImplementer},
			wantAction:    ActionReview,
			wantPhase:     servicechat.TeamPhaseReviewing,
			wantSynthetic: servicechat.SyntheticTeamReview,
		},
		{
			name:          "the team's own fix round re-opens the loop",
			signal:        Signal{Role: servicechat.TeamRoleImplementer, Synthetic: servicechat.SyntheticTeamFix},
			wantAction:    ActionReview,
			wantPhase:     servicechat.TeamPhaseReviewing,
			wantSynthetic: servicechat.SyntheticTeamReview,
		},
		{
			name:       "an autopilot round does not open a second loop",
			signal:     Signal{Role: servicechat.TeamRoleImplementer, Synthetic: servicechat.SyntheticAutopilot},
			wantAction: ActionNone,
		},
		{
			name:       "an auto-test pass does not open a second loop",
			signal:     Signal{Role: servicechat.TeamRoleImplementer, Synthetic: servicechat.SyntheticAutoTest},
			wantAction: ActionNone,
		},
		{
			name: "with no reviewer the loop opens on the tester",
			policy: func(p servicechat.TeamPolicy) servicechat.TeamPolicy {
				p.Roles.Reviewer.Enabled = false
				return p
			},
			signal:        Signal{Role: servicechat.TeamRoleImplementer},
			wantAction:    ActionTest,
			wantPhase:     servicechat.TeamPhaseTesting,
			wantSynthetic: servicechat.SyntheticTeamTest,
		},
		{
			name: "with neither seat enabled there is no team to run",
			policy: func(p servicechat.TeamPolicy) servicechat.TeamPolicy {
				p.Roles.Reviewer.Enabled = false
				p.Roles.Tester.Enabled = false
				return p
			},
			signal:     Signal{Role: servicechat.TeamRoleImplementer},
			wantAction: ActionNone,
		},
		{
			name:          "SHIP hands over to the tester",
			signal:        reviewSignal("VERDICT: SHIP\nlooks right"),
			wantAction:    ActionTest,
			wantPhase:     servicechat.TeamPhaseTesting,
			wantSynthetic: servicechat.SyntheticTeamTest,
		},
		{
			name: "SHIP with no tester finishes the session",
			policy: func(p servicechat.TeamPolicy) servicechat.TeamPolicy {
				p.Roles.Tester.Enabled = false
				return p
			},
			signal:      reviewSignal("VERDICT: SHIP"),
			wantAction:  ActionFinish,
			wantPhase:   servicechat.TeamPhaseDone,
			wantVerdict: servicechat.TeamVerdictShip,
		},
		{
			name:          "FIX spends a loop and goes back to the implementer",
			signal:        reviewSignal("VERDICT: FIX\nunbounded retry"),
			wantAction:    ActionFix,
			wantPhase:     servicechat.TeamPhaseFixing,
			wantSynthetic: servicechat.SyntheticTeamFix,
			wantLoops:     1,
			wantVerdict:   servicechat.TeamVerdictFix,
		},
		{
			name: "FIX with autoFix off reports instead of looping",
			policy: func(p servicechat.TeamPolicy) servicechat.TeamPolicy {
				p.AutoFix = false
				return p
			},
			signal:      reviewSignal("VERDICT: FIX"),
			wantAction:  ActionFinish,
			wantPhase:   servicechat.TeamPhaseDone,
			wantVerdict: servicechat.TeamVerdictFix,
		},
		{
			name: "FIX on the last loop stops rather than exceeding the cap",
			policy: func(p servicechat.TeamPolicy) servicechat.TeamPolicy {
				p.LoopsUsed = 2
				return p
			},
			signal:      reviewSignal("VERDICT: FIX"),
			wantAction:  ActionFinish,
			wantPhase:   servicechat.TeamPhaseDone,
			wantVerdict: servicechat.TeamVerdictFix,
			wantLoops:   2,
		},
		{
			name:       "a reviewer with no verdict aborts rather than guessing",
			signal:     reviewSignal("Looks fine to me."),
			wantAction: ActionAbort,
			wantPhase:  servicechat.TeamPhaseError,
		},
		{
			name:        "PASS closes the session",
			signal:      testSignal("TESTS: PASS\n3 passed"),
			wantAction:  ActionFinish,
			wantPhase:   servicechat.TeamPhaseDone,
			wantVerdict: servicechat.TeamVerdictPass,
		},
		{
			name:          "FAIL spends a loop and goes back to the implementer",
			signal:        testSignal("TESTS: FAIL\nexpected 200"),
			wantAction:    ActionFix,
			wantPhase:     servicechat.TeamPhaseFixing,
			wantSynthetic: servicechat.SyntheticTeamFix,
			wantLoops:     1,
			wantVerdict:   servicechat.TeamVerdictFail,
		},
		{
			name: "FAIL with the budget spent reports instead of looping",
			policy: func(p servicechat.TeamPolicy) servicechat.TeamPolicy {
				p.LoopsUsed = 2
				return p
			},
			signal:      testSignal("TESTS: FAIL"),
			wantAction:  ActionFinish,
			wantPhase:   servicechat.TeamPhaseDone,
			wantVerdict: servicechat.TeamVerdictFail,
			wantLoops:   2,
		},
		{
			name:       "a disabled policy never acts",
			policy:     func(p servicechat.TeamPolicy) servicechat.TeamPolicy { p.Enabled = false; return p },
			signal:     Signal{Role: servicechat.TeamRoleImplementer},
			wantAction: ActionNone,
		},
		{
			name:       "a scheduled run belongs to the scheduler's loop",
			signal:     Signal{Role: servicechat.TeamRoleImplementer, Scheduled: true},
			wantAction: ActionNone,
		},
		{
			name:       "a failed companion run stops the loop",
			signal:     Signal{Role: servicechat.TeamRoleReviewer, Failed: true, Synthetic: servicechat.SyntheticTeamReview},
			wantAction: ActionAbort,
			wantPhase:  servicechat.TeamPhaseError,
		},
		{
			name:       "a failed fix round stops the loop",
			signal:     Signal{Role: servicechat.TeamRoleImplementer, Failed: true, Synthetic: servicechat.SyntheticTeamFix},
			wantAction: ActionAbort,
			wantPhase:  servicechat.TeamPhaseError,
		},
		{
			name: "a human's own prompt in the reviewer's chat is not a verdict",
			signal: Signal{
				Role:   servicechat.TeamRoleReviewer,
				Output: "Thanks, that makes sense. VERDICT: SHIP",
			},
			wantAction: ActionNone,
		},
		{
			name:       "the operator's own failed prompt is not the team's to abort",
			signal:     Signal{Role: servicechat.TeamRoleImplementer, Failed: true},
			wantAction: ActionNone,
			wantPhase:  servicechat.TeamPhaseIdle,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := armedPolicy()
			if test.policy != nil {
				policy = test.policy(policy)
			}

			got := Decide(policy, test.signal)

			if got.Action != test.wantAction {
				t.Fatalf("action = %q, want %q", got.Action, test.wantAction)
			}
			if test.wantPhase != "" && got.Phase != test.wantPhase {
				t.Errorf("phase = %q, want %q", got.Phase, test.wantPhase)
			}
			if test.wantSynthetic != "" && got.Synthetic != test.wantSynthetic {
				t.Errorf("synthetic = %q, want %q", got.Synthetic, test.wantSynthetic)
			}
			if test.wantVerdict != "" && got.Verdict != test.wantVerdict {
				t.Errorf("verdict = %q, want %q", got.Verdict, test.wantVerdict)
			}
			if got.LoopsUsed != test.wantLoops {
				t.Errorf("loopsUsed = %d, want %d", got.LoopsUsed, test.wantLoops)
			}
		})
	}
}

// A loop that never stopped spending would be the one bug in this feature that
// costs real money, so the cap is asserted end to end rather than per branch.
func TestDecideNeverExceedsTheLoopCap(t *testing.T) {
	policy := armedPolicy()
	policy.MaxLoops = 3

	fixes := 0
	for hop := 0; hop < 20; hop++ {
		decision := Decide(policy, reviewSignal("VERDICT: FIX\nstill wrong"))
		if decision.Action != ActionFix {
			if decision.Action != ActionFinish {
				t.Fatalf("hop %d: action = %q", hop, decision.Action)
			}
			break
		}
		fixes++
		policy.LoopsUsed = decision.LoopsUsed
	}
	if fixes != 3 {
		t.Fatalf("spent %d loops, want the configured cap of 3", fixes)
	}
}

func TestResolveRolesPicksASecondOpinion(t *testing.T) {
	tests := []struct {
		name         string
		chatProvider servicechat.Provider
		connected    []servicechat.Provider
		policy       servicechat.TeamPolicy
		wantReviewer servicechat.Provider
		wantTester   servicechat.Provider
	}{
		{
			name:         "codex is preferred as the second opinion",
			chatProvider: servicechat.ProviderClaude,
			connected: []servicechat.Provider{
				servicechat.ProviderClaude, servicechat.ProviderCodex, servicechat.ProviderKimi,
			},
			wantReviewer: servicechat.ProviderCodex,
			wantTester:   servicechat.ProviderCodex,
		},
		{
			name:         "kimi steps in when codex is the implementer",
			chatProvider: servicechat.ProviderCodex,
			connected: []servicechat.Provider{
				servicechat.ProviderCodex, servicechat.ProviderKimi,
			},
			wantReviewer: servicechat.ProviderKimi,
			wantTester:   servicechat.ProviderKimi,
		},
		{
			name:         "claude is the last fallback",
			chatProvider: servicechat.ProviderCodex,
			connected: []servicechat.Provider{
				servicechat.ProviderCodex, servicechat.ProviderClaude,
			},
			wantReviewer: servicechat.ProviderClaude,
			wantTester:   servicechat.ProviderClaude,
		},
		{
			name:         "a single connected provider reviews itself in a companion chat",
			chatProvider: servicechat.ProviderClaude,
			connected:    []servicechat.Provider{servicechat.ProviderClaude},
			wantReviewer: servicechat.ProviderClaude,
			wantTester:   servicechat.ProviderClaude,
		},
		{
			name:         "nothing connected still yields a runnable cast",
			chatProvider: servicechat.ProviderKimi,
			connected:    nil,
			wantReviewer: servicechat.ProviderKimi,
			wantTester:   servicechat.ProviderKimi,
		},
		{
			name:         "an explicit choice wins over the fallback",
			chatProvider: servicechat.ProviderClaude,
			connected: []servicechat.Provider{
				servicechat.ProviderClaude, servicechat.ProviderCodex,
			},
			policy: servicechat.TeamPolicy{Roles: servicechat.TeamRoles{
				Reviewer: servicechat.TeamRole{Provider: servicechat.ProviderKimi},
			}},
			wantReviewer: servicechat.ProviderKimi,
			wantTester:   servicechat.ProviderKimi,
		},
		{
			name:         "an antigravity chat is still reviewed by a real second opinion",
			chatProvider: servicechat.ProviderAntigravity,
			connected: []servicechat.Provider{
				servicechat.ProviderAntigravity, servicechat.ProviderCodex,
			},
			wantReviewer: servicechat.ProviderCodex,
			wantTester:   servicechat.ProviderCodex,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			roles := ResolveRoles(test.policy, test.chatProvider, test.connected)

			if roles.Implementer.Provider != servicechat.NormalizeProvider(test.chatProvider) {
				t.Errorf("implementer = %q, want the chat's own provider", roles.Implementer.Provider)
			}
			if roles.Reviewer.Provider != test.wantReviewer {
				t.Errorf("reviewer = %q, want %q", roles.Reviewer.Provider, test.wantReviewer)
			}
			if roles.Tester.Provider != test.wantTester {
				t.Errorf("tester = %q, want %q", roles.Tester.Provider, test.wantTester)
			}
		})
	}
}

// The switch has to produce a working team on its own, or "one button" is a
// lie: both companion seats come on, whatever is connected.
func TestDefaultRolesEnableBothCompanions(t *testing.T) {
	roles := DefaultRoles(servicechat.ProviderClaude, []servicechat.Provider{servicechat.ProviderClaude})

	if !roles.Reviewer.Enabled || !roles.Tester.Enabled {
		t.Fatalf("roles = %+v, want both companions enabled", roles)
	}
	if !roles.Implementer.Enabled {
		t.Errorf("the implementer seat must always be enabled")
	}
}

func TestCompanionSkillsNarrowToThePublishedLibrary(t *testing.T) {
	published := []string{"review-protocol", "clean-code-guard", "test-guard", "playwright-e2e", "browser"}

	reviewer := CompanionSkills(servicechat.TeamRoleReviewer, servicechat.ProviderCodex, published)
	names := skillNames(reviewer)
	for _, want := range []string{"review-protocol", "clean-code-guard", "test-guard"} {
		if !contains(names, want) {
			t.Errorf("reviewer skills %v missing %q", names, want)
		}
	}
	if contains(names, "browser") || contains(names, "playwright-e2e") {
		t.Errorf("reviewer skills %v picked up something it did not ask for", names)
	}

	tester := skillNames(CompanionSkills(servicechat.TeamRoleTester, servicechat.ProviderClaude, published))
	if len(tester) != 1 || tester[0] != "playwright-e2e" {
		t.Errorf("tester skills = %v, want just playwright-e2e", tester)
	}

	// A library that holds neither skill must not leave a chat holding chips
	// for skills nobody can open.
	empty := CompanionSkills(servicechat.TeamRoleReviewer, servicechat.ProviderCodex, []string{"browser"})
	if len(empty) != 0 {
		t.Errorf("skills = %v, want none when the library publishes none of them", skillNames(empty))
	}

	// An unavailable library is not the same as an empty one: the optimistic
	// default is more useful than no skills at all.
	unknown := skillNames(CompanionSkills(servicechat.TeamRoleTester, servicechat.ProviderClaude, nil))
	if len(unknown) != 1 || unknown[0] != PlaywrightSkill {
		t.Errorf("skills = %v, want the optimistic default", unknown)
	}
}

func TestFixPromptCarriesFindingsAndStops(t *testing.T) {
	prompt := FixPrompt("1. drop the unused mutex")

	if !strings.Contains(prompt, "1. drop the unused mutex") {
		t.Errorf("prompt lost the findings: %q", prompt)
	}
	if !strings.Contains(prompt, "then stop") {
		t.Errorf("prompt does not end the implementer's turn: %q", prompt)
	}

	// A reviewer that said FIX without saying why must still produce a usable
	// prompt rather than an empty instruction.
	if bare := FixPrompt("   "); !strings.Contains(bare, "no detail") {
		t.Errorf("empty findings produced %q", bare)
	}
}

func TestFinishSummaryNamesTheReviewerAndTheLoops(t *testing.T) {
	policy := armedPolicy()

	summary := FinishSummary(policy, servicechat.TeamVerdictPass, 2)

	for _, want := range []string{"Codex", "SHIP", "PASS", "2 loops"} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary %q missing %q", summary, want)
		}
	}
	if single := FinishSummary(policy, servicechat.TeamVerdictPass, 1); !strings.Contains(single, "1 loop.") {
		t.Errorf("summary %q did not singularise the loop count", single)
	}
}

func skillNames(refs []servicechat.SkillRef) []string {
	names := make([]string, 0, len(refs))
	for _, ref := range refs {
		names = append(names, ref.Command)
	}
	return names
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
