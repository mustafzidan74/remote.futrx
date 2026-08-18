// Package postrun decides what the platform does on its own once an agent
// turn settles.
//
// Agent runs already outlive the browser: prompt.Service.Start roots them in
// context.Background(), so closing the tab does not stop one. What stops work
// is the agent *ending its turn* — asking a question, or finishing a step —
// while the operator is away. This package closes that gap with two per-chat
// policies:
//
//   - Autopilot sends one more "keep working" prompt every time a turn settles
//     without the agent declaring the goal done, bounded by a round count and a
//     wall-clock budget.
//   - Auto-test asks for a Playwright verification pass after every turn that
//     changed something.
//
// The decision itself is a pure function (Decide) over the settled outcome and
// the chat's stored policy. Everything that touches a clock, a store, or the
// prompt service lives in Driver, so the rules can be tested as a table.
package postrun

import (
	"strings"
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

// Action is what the driver should do about a settled run.
type Action string

const (
	// ActionNone leaves the chat alone.
	ActionNone Action = "none"
	// ActionTest starts a Playwright verification follow-up.
	ActionTest Action = "test"
	// ActionContinue starts another autopilot round.
	ActionContinue Action = "continue"
	// ActionStop disarms autopilot and reports why.
	ActionStop Action = "stop"
)

// Reason explains an ActionStop. It is the text an operator reads in the
// notification, so the values are chosen to be self-describing.
type Reason string

const (
	// ReasonDone means the agent emitted the completion marker.
	ReasonDone Reason = "done"
	// ReasonBlocked means the agent emitted the blocked marker and named
	// something it needs from a human.
	ReasonBlocked Reason = "blocked"
	// ReasonRounds means the round budget is spent.
	ReasonRounds Reason = "rounds"
	// ReasonDuration means the wall-clock budget is spent.
	ReasonDuration Reason = "duration"
	// ReasonFailed means the run errored or was cancelled. Autopilot never
	// retries into a failure — a loop that cannot tell a broken build from a
	// finished one is worse than no loop.
	ReasonFailed Reason = "failed"
)

// Markers the driver asks an autopilot agent to end its final message with.
// They are deliberately unlikely to appear in ordinary prose, and are matched
// on the trailing line only so an agent quoting the instruction back does not
// stop its own loop.
const (
	DoneMarker    = "<<DONE>>"
	blockedPrefix = "<<BLOCKED:"
)

// Outcome is the settled run the decision is about, narrowed to the fields the
// rules actually read.
type Outcome struct {
	// Output is the concatenated assistant text of the run.
	Output string
	// Failed is true when the run errored, hit a provider limit, or was
	// cancelled.
	Failed bool
	// Scheduled is true when the scheduler injected the run; the schedule
	// service owns its own follow-up loop.
	Scheduled bool
	// Synthetic is the label the settled run itself carried, so a test run
	// does not trigger another test run.
	Synthetic string
}

// Conditions are the facts about the chat that the rules need but the outcome
// does not carry.
type Conditions struct {
	// RunActive is true when the chat already has a run in flight. One run
	// per chat is a hub invariant; the driver must not race it.
	RunActive bool
	// ScheduledChat is true when a scheduled task drives this chat. Those
	// chats have their own cadence and must never be auto-continued.
	ScheduledChat bool
	// Now is the clock the duration budget is measured against.
	Now time.Time
}

// Decision is the driver's instruction.
type Decision struct {
	Action Action
	// Prompt is the text to send for ActionTest and ActionContinue.
	Prompt string
	// Synthetic is the label the follow-up run carries.
	Synthetic string
	// Reason explains an ActionStop.
	Reason Reason
	// Detail carries the agent's own words for ReasonBlocked.
	Detail string
}

// Decide reads the settled run against the chat's policies and returns the one
// thing to do next.
//
// The order matters. Guards that make *any* follow-up unsafe come first, then
// auto-test, then autopilot — a chat with both policies armed verifies the
// change before it asks for more change, so a broken step is caught in the
// round that caused it rather than three rounds later.
func Decide(meta servicechat.Meta, outcome Outcome, conditions Conditions) Decision {
	autopilot := servicechat.NormalizeAutopilot(meta.Autopilot)

	// A scheduled run, or any run inside a chat a scheduler owns, belongs to
	// the schedule service's loop. Two loops driving one chat would fight.
	if outcome.Scheduled || conditions.ScheduledChat {
		return Decision{Action: ActionNone}
	}

	// A failure is where autopilot stops rather than doubles down, and it
	// stops even if the chat is busy: disarming is a metadata write, not a run.
	if outcome.Failed {
		if autopilot.Enabled {
			return Decision{Action: ActionStop, Reason: ReasonFailed}
		}
		return Decision{Action: ActionNone}
	}

	// One run per chat. If something already started — a queued human prompt,
	// a scheduled turn — the driver yields and will be called again when that
	// run settles.
	if conditions.RunActive {
		return Decision{Action: ActionNone}
	}

	if meta.AutoTest.Enabled && outcome.Synthetic != servicechat.SyntheticAutoTest {
		return Decision{
			Action:    ActionTest,
			Prompt:    AutoTestPrompt,
			Synthetic: servicechat.SyntheticAutoTest,
		}
	}

	if !autopilot.Enabled {
		return Decision{Action: ActionNone}
	}

	// The agent's own verdict outranks the budgets: a task that is finished
	// should report "done", not "ran out of rounds".
	if marker := findMarker(outcome.Output); marker.found {
		if marker.blocked {
			return Decision{Action: ActionStop, Reason: ReasonBlocked, Detail: marker.detail}
		}
		return Decision{Action: ActionStop, Reason: ReasonDone}
	}

	if autopilot.RoundsUsed >= autopilot.MaxRounds {
		return Decision{Action: ActionStop, Reason: ReasonRounds}
	}
	if exceededDuration(autopilot, conditions.Now) {
		return Decision{Action: ActionStop, Reason: ReasonDuration}
	}

	return Decision{
		Action:    ActionContinue,
		Prompt:    ContinuePrompt + "\n\n" + MarkerInstruction,
		Synthetic: servicechat.SyntheticAutopilot,
	}
}

// exceededDuration reports whether the wall-clock budget is spent. A policy
// with no start stamp has not started counting yet, which is what a chat armed
// by an older build looks like.
func exceededDuration(policy servicechat.AutopilotPolicy, now time.Time) bool {
	if policy.StartedAt <= 0 || now.IsZero() {
		return false
	}
	deadline := time.UnixMilli(policy.StartedAt).Add(time.Duration(policy.MaxDurationMin) * time.Minute)
	return !now.Before(deadline)
}

type markerMatch struct {
	found   bool
	blocked bool
	detail  string
}

// findMarker inspects the last non-empty line of the agent's message. Matching
// only the trailing line is what keeps an agent that quotes the instruction
// mid-answer ("I will end with <<DONE>> when finished") from stopping itself.
func findMarker(output string) markerMatch {
	line := lastNonEmptyLine(output)
	if line == "" {
		return markerMatch{}
	}
	if strings.HasSuffix(line, DoneMarker) {
		return markerMatch{found: true}
	}
	start := strings.Index(line, blockedPrefix)
	if start < 0 || !strings.HasSuffix(line, ">>") {
		return markerMatch{}
	}
	detail := line[start+len(blockedPrefix):]
	detail = strings.TrimSuffix(detail, ">>")
	return markerMatch{found: true, blocked: true, detail: strings.TrimSpace(detail)}
}

func lastNonEmptyLine(text string) string {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		if line := strings.TrimSpace(lines[index]); line != "" {
			return line
		}
	}
	return ""
}
