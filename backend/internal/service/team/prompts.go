package team

import (
	"strconv"
	"strings"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

// The prompts the orchestrator sends on the operator's behalf.
//
// Two things shape the wording. Nobody is there to answer a question, so none
// may be asked. And every companion prompt has to end in a machine-readable
// verdict, because the verdict is the only signal that decides where the loop
// goes next — an agent that just describes what it found leaves the
// orchestrator with nothing to branch on, which is why the marker instruction
// is repeated in the imperative and shown literally.
const (
	// ReviewPrompt runs in the reviewer's companion chat. It reviews the
	// workspace as it stands, because the reviewer's chat has no history of
	// the implementer's turn — the diff is the shared ground truth.
	ReviewPrompt = "Review the latest changes in /workspace made by another agent. " +
		"Start from `git status` and `git diff` for uncommitted work, and `git log -p -3` for what was just committed. " +
		"Apply the review-protocol skill. " +
		"Read the code you are judging — do not review from the diff summary alone. " +
		"Do not edit any files: you are reviewing, not implementing.\n\n" +
		"End your message with the exact line `VERDICT: SHIP` (the change is correct and safe to keep) " +
		"or `VERDICT: FIX` (it needs work), and put your findings after that line as a short numbered list, " +
		"each item naming the file and what to change."

	// TestPrompt runs in the tester's companion chat.
	TestPrompt = "Verify the current state of the app with Playwright (playwright-e2e skill). " +
		"Work out which user journey the latest changes in /workspace affect (`git diff`, `git log -p -3`), " +
		"write or update a minimal e2e spec for it against the app on its local port, and run it headless. " +
		"Do not loosen assertions to get a green result, and do not change application code.\n\n" +
		"End your message with the exact line `TESTS: PASS` or `TESTS: FAIL`, " +
		"and put the assertion output after that line."
)

// FixPrompt carries a companion's findings back to the implementer. It ends
// the turn deliberately: the implementer must not treat a review as licence to
// keep going, or the loop cap stops bounding anything.
func FixPrompt(findings string) string {
	var builder strings.Builder
	builder.WriteString("Address these review findings, then stop:\n\n")
	if trimmed := strings.TrimSpace(findings); trimmed != "" {
		builder.WriteString(trimmed)
	} else {
		builder.WriteString("(the reviewer returned no detail — re-read your last change and fix what is wrong)")
	}
	builder.WriteString(
		"\n\nFix only what is listed above. Do not start new work, do not ask questions, " +
			"and stop when the listed items are done.",
	)
	return builder.String()
}

// FinishSummary is the closing message. It names who reviewed, what they said,
// and how many loops it took, because "the team finished" alone tells an
// operator nothing about whether to read the diff themselves.
func FinishSummary(policy servicechat.TeamPolicy, verdict string, loops int) string {
	reviewer := ProviderLabel(policy.Roles.Reviewer.Provider)
	tester := ProviderLabel(policy.Roles.Tester.Provider)

	var builder strings.Builder
	switch verdict {
	case servicechat.TeamVerdictPass:
		builder.WriteString("✅ Team: reviewed by ")
		builder.WriteString(reviewer)
		builder.WriteString(" (SHIP), tests PASS")
	case servicechat.TeamVerdictShip:
		builder.WriteString("✅ Team: reviewed by ")
		builder.WriteString(reviewer)
		builder.WriteString(" (SHIP), no test pass configured")
	case servicechat.TeamVerdictFail:
		builder.WriteString("⚠️ Team: tests FAIL after ")
		builder.WriteString(tester)
		builder.WriteString(" ran them — a human is needed")
	case servicechat.TeamVerdictFix:
		builder.WriteString("⚠️ Team: ")
		builder.WriteString(reviewer)
		builder.WriteString(" still says FIX — a human is needed")
	default:
		builder.WriteString("👥 Team: finished")
	}
	builder.WriteString(" in ")
	builder.WriteString(plural(loops, "loop"))
	builder.WriteString(".")
	return builder.String()
}

// AbortSummary explains why a loop stopped early.
func AbortSummary(role, reason string) string {
	seat := RoleLabel(role)
	switch reason {
	case ReasonNoVerdict:
		return "⚠️ Team: the " + seat + " did not end with a verdict line, so the loop stopped. " +
			"Open its chat to read what it said."
	case ReasonRunFailed:
		return "⚠️ Team: the " + seat + " run did not finish cleanly, so the loop stopped."
	default:
		return "⚠️ Team: the loop stopped at the " + seat + "."
	}
}

// RoleLabel is the human word for a seat.
func RoleLabel(role string) string {
	switch role {
	case servicechat.TeamRoleReviewer:
		return "reviewer"
	case servicechat.TeamRoleTester:
		return "tester"
	case servicechat.TeamRoleImplementer:
		return "implementer"
	default:
		return "team"
	}
}

// ProviderLabel is the display name of a provider, matching what the composer
// shows so the summary and the pill name the same agent.
func ProviderLabel(provider servicechat.Provider) string {
	switch servicechat.NormalizeTeamProvider(provider) {
	case servicechat.ProviderClaude:
		return "Claude"
	case servicechat.ProviderCodex:
		return "Codex"
	case servicechat.ProviderKimi:
		return "Kimi"
	case servicechat.ProviderAntigravity:
		return "Antigravity"
	default:
		return "the agent"
	}
}

// CompanionTitle is the title of a role's companion chat. The emoji is what
// makes it findable at a glance once it is opened from the Team panel.
func CompanionTitle(role, parentTitle string) string {
	parent := strings.TrimSpace(parentTitle)
	if parent == "" {
		parent = "Untitled chat"
	}
	switch role {
	case servicechat.TeamRoleReviewer:
		return "🧐 Reviewer — " + parent
	case servicechat.TeamRoleTester:
		return "🧪 Tester — " + parent
	default:
		return "👥 Team — " + parent
	}
}

func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(count) + " " + noun + "s"
}
