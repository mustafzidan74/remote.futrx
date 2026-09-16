// Package journal keeps one dated record per agent run for each project: what
// was asked, what the agent reported back, and which files it wrote. It is the
// project's change history, readable long after the chat it happened in has
// scrolled away.
//
// The usage ledger already records that a run happened and what it cost, and
// the audit log records that an action was taken. Neither keeps the prose, and
// the transcript that does keep it is organised by chat, not by project.
package journal

import "strings"

// Status is how a run ended.
const (
	StatusDone      = "done"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Entry is one run: a request, its outcome, and the files it touched.
type Entry struct {
	// At is when the run settled, in unix milliseconds.
	At        int64  `json:"at"`
	ProjectID string `json:"projectId"`
	ChatID    string `json:"chatId"`
	ChatTitle string `json:"chatTitle,omitempty"`
	RunID     string `json:"runId,omitempty"`
	Provider  string `json:"provider,omitempty"`
	Model     string `json:"model,omitempty"`
	// Request is the message that started the run, as the user wrote it.
	Request string `json:"request"`
	// Summary is what the agent reported when it finished.
	Summary string `json:"summary,omitempty"`
	// Files are workspace-relative paths the run wrote or edited, from the
	// agent's own tool calls rather than from the state of the checkout, so a
	// file someone else left dirty is never attributed to this run.
	Files []string `json:"files,omitempty"`
	// Commands are the shell commands the run executed, first line only.
	Commands []string `json:"commands,omitempty"`
	Status   string   `json:"status"`
	// Error is the failure message when Status is failed.
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	// Scheduled marks a run started by a scheduled task rather than by a person.
	Scheduled bool `json:"scheduled,omitempty"`
	// Synthetic names the platform loop behind a run nobody typed (autopilot,
	// auto-test, team mode).
	Synthetic string `json:"synthetic,omitempty"`
}

// Matches reports whether the entry contains text, case-insensitively, in any
// field a person would search: the request, the summary, the chat title, the
// touched files, and the commands.
func (e Entry) Matches(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return true
	}
	haystacks := []string{e.Request, e.Summary, e.ChatTitle, e.Error, e.Model, e.Provider}
	haystacks = append(haystacks, e.Files...)
	haystacks = append(haystacks, e.Commands...)
	for _, haystack := range haystacks {
		if strings.Contains(strings.ToLower(haystack), text) {
			return true
		}
	}
	return false
}

// Query filters a project's journal. Zero values mean "no bound".
type Query struct {
	ProjectID string
	// Text matches any searchable field of an entry.
	Text string
	// From and To bound the run time, in unix milliseconds, inclusive.
	From int64
	To   int64
	// Limit caps how many entries a page carries.
	Limit int
	// Cursor continues a previous page; it is opaque and comes from Page.
	Cursor string
}

// Page is one answer to a Query, newest entry first.
type Page struct {
	Entries []Entry `json:"entries"`
	// NextCursor is empty when the page is the last one.
	NextCursor string `json:"nextCursor,omitempty"`
}

// DefaultLimit and MaxLimit bound a page.
const (
	DefaultLimit = 50
	MaxLimit     = 200
)

// NormalizeLimit clamps a requested page size.
func NormalizeLimit(limit int) int {
	switch {
	case limit <= 0:
		return DefaultLimit
	case limit > MaxLimit:
		return MaxLimit
	default:
		return limit
	}
}
