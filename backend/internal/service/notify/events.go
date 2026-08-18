package notify

import (
	"net/url"
	"strings"
)

// Status values carried by Event.Status.
const (
	StatusFinished  = "finished"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
	StatusWaiting   = "waiting"
	StatusSucceeded = "succeeded"
	// StatusSkipped marks work that deliberately did not happen — a scheduled
	// occurrence whose condition (gate) was not met.
	StatusSkipped = "skipped"
	// The health statuses mirror the health service's own traffic light, so a
	// webhook consumer can switch on the same three words the UI shows.
	StatusHealthWarn = "warn"
	StatusHealthCrit = "crit"
	StatusHealthOK   = "ok"
	// StatusStarted marks the platform lifecycle event published when the
	// backend process comes up.
	StatusStarted = "started"
)

// summaryLimit keeps agent output short enough to read on a phone.
const summaryLimit = 600

// EventHeadline is the one-line title used by message-shaped sinks.
func EventHeadline(event Event) string {
	switch event.Event {
	case KindRunFinished:
		return "Agent run finished"
	case KindRunFailed:
		return "Agent run failed"
	case KindNeedsAttention:
		return "Agent needs your attention"
	case KindScheduledRun:
		switch event.Status {
		case StatusFailed:
			return "Scheduled task failed"
		case StatusSkipped:
			return "Scheduled task skipped"
		}
		return "Scheduled task ran"
	case KindProjectHealth:
		switch event.Status {
		case StatusHealthCrit:
			return "Project health critical"
		case StatusHealthOK:
			return "Project health recovered"
		default:
			return "Project health warning"
		}
	case KindSystem:
		return "Remote system event"
	case KindDigest:
		return "Weekly usage report"
	case KindScreenshot:
		return "Preview screenshot"
	case KindClientMessage:
		return "Message for the client"
	case KindTest:
		return "Test notification"
	default:
		return "Remote notification"
	}
}

// RunKind maps a finished run's error to the event kind and status. A
// cancelled run is reported as a failure so an operator still learns the run
// stopped without finishing.
func RunKind(err error, cancelled bool) (Kind, string) {
	switch {
	case cancelled:
		return KindRunFailed, StatusCancelled
	case err != nil:
		return KindRunFailed, StatusFailed
	default:
		return KindRunFinished, StatusFinished
	}
}

// Summary trims agent output (or an error message) into a phone-sized blurb:
// single blank-line-free text, capped at summaryLimit runes.
func Summary(text string) string {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, " \t\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		kept = append(kept, line)
	}
	return truncate(strings.Join(kept, "\n"), summaryLimit)
}

// ChatURL builds the deep link an operator taps to land on the chat. The SPA
// has no path router, so the chat is selected through a query parameter on the
// application root (see frontend/src/state/workspace/chatDeepLink.ts).
func ChatURL(baseURL, chatID string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return ""
	}
	if strings.TrimSpace(chatID) == "" {
		return baseURL + "/"
	}
	return baseURL + "/?chat=" + url.QueryEscape(chatID)
}

// ProjectURL builds the deep link an operator taps to land on a project's
// settings page. Like ChatURL it goes through a query parameter on the
// application root, because the SPA has no path router (see
// frontend/src/state/workspace/projectDeepLink.ts).
func ProjectURL(baseURL, projectID string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return ""
	}
	if strings.TrimSpace(projectID) == "" {
		return baseURL + "/"
	}
	return baseURL + "/?project=" + url.QueryEscape(projectID)
}

// UsageURL is the deep link a digest points at. The SPA has no path router
// and no settings deep link, so it lands on the application root; the message
// body names the page to open (Settings -> Usage).
func UsageURL(baseURL string) string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return ""
	}
	return baseURL + "/"
}

// attentionTools are the agent tool calls that hand control back to the human:
// a question, a plan waiting for approval, or an explicit permission request.
// Names are compared after lowercasing and dropping separators, so both
// AskUserQuestion and ask_user_question match.
var attentionTools = map[string]struct{}{
	"askuserquestion":     {},
	"askusertool":         {},
	"askfollowupquestion": {},
	"askpermission":       {},
	"exitplanmode":        {},
	"requestpermission":   {},
	"requestapproval":     {},
	"userinput":           {},
	"waitforuser":         {},
}

// NeedsAttentionTool reports whether a tool call means the run is blocked on a
// human decision.
func NeedsAttentionTool(toolName string) bool {
	_, ok := attentionTools[normalizeToolName(toolName)]
	return ok
}

func normalizeToolName(name string) string {
	var out strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch r {
		case '_', '-', ' ', '.', ':':
			continue
		}
		out.WriteRune(r)
	}
	// MCP tools arrive namespaced (mcp__server__tool); keep the trailing part.
	normalized := out.String()
	if index := strings.LastIndex(normalized, "/"); index >= 0 {
		normalized = normalized[index+1:]
	}
	return normalized
}
