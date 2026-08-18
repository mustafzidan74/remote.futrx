package postrun

import (
	"strconv"
	"strings"
)

// The prompts the driver sends on the operator's behalf.
//
// They are written as instructions to an unattended agent, which changes the
// wording in two ways worth preserving: no questions may be asked, because
// nobody is there to answer, and the verification pass is explicitly forbidden
// from weakening its own assertions to get a green result.
//
// AutoTestPrompt is also what the composer's "Test the last change" menu item
// sends, so a human-triggered check and an automatic one produce the same
// work. The frontend keeps a matching copy in
// frontend/src/state/chat/testPrompts.ts — change both together.
const (
	// AutoTestPrompt runs after a turn that changed something.
	AutoTestPrompt = "Verify the change you just made with Playwright (playwright-e2e skill): " +
		"write or update a minimal e2e spec for the affected user journey against the app on its local port, " +
		"run it headless, and report PASS/FAIL with the assertion output. " +
		"If it fails because of your change, fix the change and re-run once. " +
		"Do not loosen assertions to pass."

	// ContinuePrompt is one autopilot round.
	ContinuePrompt = "Continue autonomously — the operator is away. " +
		"Make reasonable decisions yourself, do not ask questions, and keep working toward the original goal. " +
		"Summarize progress briefly at the end."

	// MarkerInstruction is appended to every autopilot prompt. It is the only
	// way the loop learns that the goal is met: without a marker the driver
	// cannot distinguish "finished" from "ended a step", so it would keep
	// going until the round budget ran out.
	MarkerInstruction = "When the whole task is truly complete, end your message with the exact line `<<DONE>>`. " +
		"If you are blocked and need the operator, end with `<<BLOCKED: reason>>`."
)

// StopSummary is the notification body published when autopilot disarms
// itself. Every stop names the reason and the rounds actually spent, because
// "it stopped" alone tells an operator nothing about whether to look.
func StopSummary(rounds int, reason Reason, detail string) string {
	var builder strings.Builder
	builder.WriteString("Autopilot finished after ")
	builder.WriteString(plural(rounds, "round"))
	switch reason {
	case ReasonDone:
		builder.WriteString(" — the agent reported the task complete.")
	case ReasonBlocked:
		builder.WriteString(" — the agent is blocked and needs you.")
		if trimmed := strings.TrimSpace(detail); trimmed != "" {
			builder.WriteString("\n")
			builder.WriteString(trimmed)
		}
	case ReasonRounds:
		builder.WriteString(" — the round limit was reached before the agent reported completion.")
	case ReasonDuration:
		builder.WriteString(" — the time limit was reached before the agent reported completion.")
	case ReasonFailed:
		builder.WriteString(" — the last run did not finish cleanly, so the loop stopped.")
	default:
		builder.WriteString(".")
	}
	return builder.String()
}

func plural(count int, noun string) string {
	if count == 1 {
		return "1 " + noun
	}
	return strconv.Itoa(count) + " " + noun + "s"
}
