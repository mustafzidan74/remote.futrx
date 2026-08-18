// Package team turns a single chat into a coordinated multi-agent workflow.
//
// The chat the human types in is the *implementer*. When one of its runs
// settles cleanly, the orchestrator starts a *reviewer* in a companion chat —
// a different connected provider where possible, so the review really is a
// second opinion rather than the same model grading its own homework — and
// reads a verdict line out of what it says. A SHIP verdict hands over to the
// *tester*, which runs the Playwright pass and reports its own verdict; a FIX
// verdict goes back to the implementer as findings, and the loop runs again up
// to a hard cap.
//
// Everything in this file is a pure function over the chat's stored policy and
// the settled run, so the whole state machine — verdict parsing, loop caps,
// provider fallback — is a table test. The parts that touch a clock, a store,
// the prompt service, or the run hub live in Driver.
package team

import (
	"strings"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

// Action is the one thing the driver should do about a settled run.
type Action string

const (
	// ActionNone leaves the chat alone.
	ActionNone Action = "none"
	// ActionReview starts the reviewer in its companion chat.
	ActionReview Action = "review"
	// ActionTest starts the tester in its companion chat.
	ActionTest Action = "test"
	// ActionFix sends review findings back to the implementer chat.
	ActionFix Action = "fix"
	// ActionFinish closes the loop and posts the summary.
	ActionFinish Action = "finish"
	// ActionAbort stops the loop because something went wrong that a human
	// needs to look at.
	ActionAbort Action = "abort"
)

// Signal is the settled run the decision is about, narrowed to the fields the
// rules read.
type Signal struct {
	// Role is the seat of the chat whose run settled: implementer for the
	// parent chat, reviewer or tester for a companion.
	Role string
	// Output is the concatenated assistant text of the run.
	Output string
	// Failed is true when the run errored, hit a provider limit, or was
	// cancelled. A team loop never doubles down on a failure.
	Failed bool
	// Scheduled is true when the scheduler injected the run; those chats have
	// their own cadence and the team loop keeps out of them.
	Scheduled bool
	// Synthetic is the label the settled run itself carried, so a review pass
	// does not trigger another review pass.
	Synthetic string
}

// Decision is the driver's instruction.
type Decision struct {
	Action Action
	// Role is the seat the hop runs in, for ActionReview/ActionTest/ActionFix.
	Role string
	// Prompt is the text to send, and Synthetic the label it carries.
	Prompt    string
	Synthetic string
	// Phase is what the parent chat's policy should record.
	Phase string
	// Verdict is the verdict this decision settled on, empty while the loop
	// is still running.
	Verdict string
	// Findings is the reviewer's or tester's own words, carried into the fix
	// prompt and onto the timeline.
	Findings string
	// LoopsUsed is the loop counter after this decision. ActionFix is the
	// only decision that spends one.
	LoopsUsed int
	// Reason explains an ActionAbort in the summary the operator reads.
	Reason string
}

// Reasons an abort carries.
const (
	// ReasonRunFailed means the hop's own run errored or was cancelled.
	ReasonRunFailed = "the run did not finish cleanly"
	// ReasonNoVerdict means the agent ignored the verdict-line instruction.
	// The loop stops rather than guessing: reading "no verdict" as SHIP would
	// let a silent agent wave anything through.
	ReasonNoVerdict = "the agent did not end with a verdict line"
)

// Decide reads a settled run against the chat's team policy and returns the
// one thing to do next.
//
// The guards come first and in the order that matters: a scheduled run belongs
// to another loop, a failed run stops this one, and only then do the verdicts
// get read.
func Decide(policy servicechat.TeamPolicy, signal Signal) Decision {
	policy = servicechat.NormalizeTeam(policy)
	if !policy.Enabled || signal.Scheduled {
		return Decision{Action: ActionNone, Phase: policy.Phase, LoopsUsed: policy.LoopsUsed}
	}

	role := strings.TrimSpace(signal.Role)
	if signal.Failed {
		// Only a run the loop started is the loop's to abort. A failed prompt
		// the operator typed themselves — in the implementer chat or in a
		// companion — means no hop was lost.
		if !teamStarted(role, signal.Synthetic) {
			return Decision{Action: ActionNone, Phase: policy.Phase, LoopsUsed: policy.LoopsUsed}
		}
		return abort(policy, role, ReasonRunFailed)
	}

	switch role {
	case servicechat.TeamRoleImplementer:
		return decideAfterImplementer(policy, signal)
	case servicechat.TeamRoleReviewer:
		return decideAfterCompanion(policy, signal, servicechat.SyntheticTeamReview, decideAfterReviewer)
	case servicechat.TeamRoleTester:
		return decideAfterCompanion(policy, signal, servicechat.SyntheticTeamTest, decideAfterTester)
	default:
		return Decision{Action: ActionNone, Phase: policy.Phase, LoopsUsed: policy.LoopsUsed}
	}
}

// decideAfterCompanion reads a verdict only out of the run the loop itself
// started. A companion chat is a normal chat the operator can open and type
// into, and a human asking the reviewer a follow-up question is not a verdict
// — reading it as one would abort the loop for saying "thanks".
func decideAfterCompanion(
	policy servicechat.TeamPolicy,
	signal Signal,
	want string,
	decide func(servicechat.TeamPolicy, Signal) Decision,
) Decision {
	if servicechat.NormalizeSynthetic(signal.Synthetic) != want {
		return Decision{Action: ActionNone, Phase: policy.Phase, LoopsUsed: policy.LoopsUsed}
	}
	return decide(policy, signal)
}

// decideAfterImplementer opens a loop. Only a human prompt and the team's own
// fix round qualify: an autopilot or auto-test follow-up would otherwise open
// a second loop on top of the one already running.
func decideAfterImplementer(policy servicechat.TeamPolicy, signal Signal) Decision {
	switch servicechat.NormalizeSynthetic(signal.Synthetic) {
	case "", servicechat.SyntheticTeamFix:
	default:
		return Decision{Action: ActionNone, Phase: policy.Phase, LoopsUsed: policy.LoopsUsed}
	}
	return openLoop(policy)
}

// openLoop picks the first enabled seat. A team with neither seat enabled is
// not a team, so it does nothing rather than posting a summary about it.
func openLoop(policy servicechat.TeamPolicy) Decision {
	if policy.Roles.Reviewer.Enabled {
		return Decision{
			Action:    ActionReview,
			Role:      servicechat.TeamRoleReviewer,
			Prompt:    ReviewPrompt,
			Synthetic: servicechat.SyntheticTeamReview,
			Phase:     servicechat.TeamPhaseReviewing,
			LoopsUsed: policy.LoopsUsed,
		}
	}
	if policy.Roles.Tester.Enabled {
		return testHop(policy)
	}
	return Decision{Action: ActionNone, Phase: servicechat.TeamPhaseIdle, LoopsUsed: policy.LoopsUsed}
}

// teamStarted reports whether the settled run is one the orchestrator issued.
func teamStarted(role, synthetic string) bool {
	switch servicechat.NormalizeSynthetic(synthetic) {
	case servicechat.SyntheticTeamFix:
		return role == servicechat.TeamRoleImplementer
	case servicechat.SyntheticTeamReview:
		return role == servicechat.TeamRoleReviewer
	case servicechat.SyntheticTeamTest:
		return role == servicechat.TeamRoleTester
	default:
		return false
	}
}

func decideAfterReviewer(policy servicechat.TeamPolicy, signal Signal) Decision {
	verdict := ParseVerdict(servicechat.TeamRoleReviewer, signal.Output)
	switch verdict.Kind {
	case servicechat.TeamVerdictShip:
		if policy.Roles.Tester.Enabled {
			hop := testHop(policy)
			hop.Findings = verdict.Findings
			return hop
		}
		return finish(policy, servicechat.TeamVerdictShip, verdict.Findings)
	case servicechat.TeamVerdictFix:
		return fixOrStop(policy, servicechat.TeamVerdictFix, verdict.Findings)
	default:
		return abort(policy, servicechat.TeamRoleReviewer, ReasonNoVerdict)
	}
}

func decideAfterTester(policy servicechat.TeamPolicy, signal Signal) Decision {
	verdict := ParseVerdict(servicechat.TeamRoleTester, signal.Output)
	switch verdict.Kind {
	case servicechat.TeamVerdictPass:
		return finish(policy, servicechat.TeamVerdictPass, verdict.Findings)
	case servicechat.TeamVerdictFail:
		return fixOrStop(policy, servicechat.TeamVerdictFail, verdict.Findings)
	default:
		return abort(policy, servicechat.TeamRoleTester, ReasonNoVerdict)
	}
}

// fixOrStop spends a loop if there is one left and the operator asked for
// automatic fixes; otherwise it closes the loop with the negative verdict so
// the summary says a human is needed.
func fixOrStop(policy servicechat.TeamPolicy, verdict, findings string) Decision {
	if !policy.AutoFix || policy.LoopsUsed >= policy.MaxLoops {
		return finish(policy, verdict, findings)
	}
	return Decision{
		Action:    ActionFix,
		Role:      servicechat.TeamRoleImplementer,
		Prompt:    FixPrompt(findings),
		Synthetic: servicechat.SyntheticTeamFix,
		Phase:     servicechat.TeamPhaseFixing,
		Verdict:   verdict,
		Findings:  findings,
		LoopsUsed: policy.LoopsUsed + 1,
	}
}

func testHop(policy servicechat.TeamPolicy) Decision {
	return Decision{
		Action:    ActionTest,
		Role:      servicechat.TeamRoleTester,
		Prompt:    TestPrompt,
		Synthetic: servicechat.SyntheticTeamTest,
		Phase:     servicechat.TeamPhaseTesting,
		LoopsUsed: policy.LoopsUsed,
	}
}

func finish(policy servicechat.TeamPolicy, verdict, findings string) Decision {
	return Decision{
		Action:    ActionFinish,
		Phase:     servicechat.TeamPhaseDone,
		Verdict:   verdict,
		Findings:  findings,
		LoopsUsed: policy.LoopsUsed,
	}
}

func abort(policy servicechat.TeamPolicy, role, reason string) Decision {
	return Decision{
		Action:    ActionAbort,
		Role:      role,
		Phase:     servicechat.TeamPhaseError,
		LoopsUsed: policy.LoopsUsed,
		Reason:    reason,
	}
}

// ResolveRoles fills in whatever the operator left to the platform.
//
// The implementer is the chat's own provider. The reviewer is a *different*
// connected provider where one exists, because a model reviewing its own
// output is the failure this whole feature exists to avoid; with only one
// provider connected it falls back to that provider, and the fresh eyes come
// from the companion chat's empty context instead. The tester follows the
// reviewer, falling back to Claude.
//
// Explicit choices always win, even a choice that puts the same provider in
// two seats — the operator may know something the fallback does not.
func ResolveRoles(
	policy servicechat.TeamPolicy,
	chatProvider servicechat.Provider,
	connected []servicechat.Provider,
) servicechat.TeamRoles {
	available := connectedSet(connected)
	roles := servicechat.NormalizeTeam(policy).Roles

	implementer := roles.Implementer.Provider
	if implementer == "" {
		implementer = servicechat.NormalizeProvider(chatProvider)
	}
	roles.Implementer.Provider = implementer
	roles.Implementer.Enabled = true

	if roles.Reviewer.Provider == "" {
		roles.Reviewer.Provider = reviewerFallback(implementer, available)
	}
	if roles.Tester.Provider == "" {
		roles.Tester.Provider = testerFallback(roles.Reviewer.Provider, available)
	}
	return roles
}

// DefaultRoles is the cast a chat gets the moment team mode is switched on
// with nothing configured: both companion seats enabled when a second provider
// is connected, and the reviewer still enabled on a single-provider box —
// there it runs in a companion chat of its own, which is where the fresh eyes
// come from.
func DefaultRoles(
	chatProvider servicechat.Provider,
	connected []servicechat.Provider,
) servicechat.TeamRoles {
	roles := ResolveRoles(servicechat.TeamPolicy{}, chatProvider, connected)
	roles.Reviewer.Enabled = true
	roles.Tester.Enabled = true
	return roles
}

// reviewerPreference is the order a second opinion is picked in. Codex leads
// because it is the provider most often connected alongside Claude on these
// boxes, and Antigravity is absent on purpose: its print mode returns plain
// text without structured events, which is a poor fit for a verdict pass.
var reviewerPreference = []servicechat.Provider{
	servicechat.ProviderCodex,
	servicechat.ProviderKimi,
	servicechat.ProviderClaude,
}

func reviewerFallback(
	implementer servicechat.Provider,
	available map[servicechat.Provider]bool,
) servicechat.Provider {
	for _, candidate := range reviewerPreference {
		if candidate != implementer && available[candidate] {
			return candidate
		}
	}
	// No second opinion available: the reviewer runs on the implementer's
	// provider, in its own companion chat.
	return implementer
}

func testerFallback(
	reviewer servicechat.Provider,
	available map[servicechat.Provider]bool,
) servicechat.Provider {
	if reviewer != "" {
		return reviewer
	}
	if available[servicechat.ProviderClaude] {
		return servicechat.ProviderClaude
	}
	return servicechat.ProviderClaude
}

func connectedSet(connected []servicechat.Provider) map[servicechat.Provider]bool {
	available := make(map[servicechat.Provider]bool, len(connected))
	for _, provider := range connected {
		if normalized := servicechat.NormalizeTeamProvider(provider); normalized != "" {
			available[normalized] = true
		}
	}
	return available
}
