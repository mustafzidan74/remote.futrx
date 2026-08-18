package schedule

import (
	"context"
	"regexp"
	"strings"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
)

// MaxHistoryRuns is how many run records one task keeps. Older records are
// trimmed on append, so the file is bounded without a sweeper.
const MaxHistoryRuns = 50

// maxSummaryChars is the tail of assistant text kept per run.
const maxSummaryChars = 500

// HistoryStatus is the coarse per-run outcome the history table shows. It is
// deliberately smaller than RunStatus: a reader wants "did it work", not the
// scheduler's internal claim state.
type HistoryStatus string

const (
	HistoryOK        HistoryStatus = "ok"
	HistoryFailed    HistoryStatus = "failed"
	HistorySkipped   HistoryStatus = "skipped"
	HistoryCancelled HistoryStatus = "cancelled"
)

// RunRecord is one line of DATA_DIR/scheduled-tasks/history/<taskId>.jsonl.
type RunRecord struct {
	RunID      string         `json:"runId"`
	TaskID     ID             `json:"taskId"`
	ChatID     servicechat.ID `json:"chatId,omitempty"`
	StartedAt  int64          `json:"startedAt"`
	FinishedAt int64          `json:"finishedAt"`
	Status     HistoryStatus  `json:"status"`
	// Summary is the last 500 characters of the turn's assistant text.
	Summary string `json:"summary,omitempty"`
	// Result is the verdict marker the run printed, if any.
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
	// SkippedByGate separates "the gate said no" from every other skip.
	SkippedByGate bool      `json:"skippedByGate,omitempty"`
	GateReason    string    `json:"gateReason,omitempty"`
	Chain         *ChainRun `json:"chain,omitempty"`
	// ChainTriggered lists the tasks this run put on the clock.
	ChainTriggered []ID `json:"chainTriggered,omitempty"`
	Forced         bool `json:"forced,omitempty"`

	Tokens  int64    `json:"tokens,omitempty"`
	CostUSD *float64 `json:"costUsd,omitempty"`

	FilesChanged []string `json:"filesChanged,omitempty"`
	DiffStat     string   `json:"diffStat,omitempty"`
	// CommitSHA is set when the run moved HEAD, so the diff endpoint can
	// enrich the stored stat with `git show --stat`.
	CommitSHA string `json:"commitSha,omitempty"`
}

// DurationMs is how long the run took, zero when it never started.
func (r RunRecord) DurationMs() int64 {
	if r.StartedAt <= 0 || r.FinishedAt <= r.StartedAt {
		return 0
	}
	return r.FinishedAt - r.StartedAt
}

// RunDiff is the payload of the per-run diff endpoint.
type RunDiff struct {
	RunID        string   `json:"runId"`
	TaskID       ID       `json:"taskId"`
	FilesChanged []string `json:"filesChanged,omitempty"`
	// DiffStat is what was captured inside the container right after the run.
	DiffStat string `json:"diffStat,omitempty"`
	// CommitStat is `git show --stat` for the commit the run created, read
	// live. Empty when the run made no commit or the workspace is gone.
	CommitStat string `json:"commitStat,omitempty"`
	CommitSHA  string `json:"commitSha,omitempty"`
	// Unavailable explains an empty CommitStat without failing the request.
	Unavailable string `json:"unavailable,omitempty"`
}

// RunUsage is the optional token/cost accounting for one settled run.
type RunUsage struct {
	Tokens  int64
	CostUSD *float64
}

// verdictPattern matches the documented marker convention. The whole line
// must be the marker so ordinary prose that merely mentions it is ignored.
var verdictPattern = regexp.MustCompile(`^<<RESULT:\s*(.*?)\s*>>$`)

// isCompletionMarkerLine reports whether a line is the "standing task is done"
// marker. It lives next to the verdict reader because the two markers step
// over each other: a turn may declare both.
func isCompletionMarkerLine(line string) bool {
	return line == "SCHEDULE_STATUS=COMPLETE" || line == "TASK_COMPLETE"
}

// ExtractResult returns the verdict a run declared: the last meaningful line
// of its output, when that line is exactly `<<RESULT: ...>>`. Anything else
// returns "" — a run that forgot the marker has no verdict rather than a
// guessed one. A trailing completion marker is stepped over, so one turn can
// both declare its verdict and end the standing task.
func ExtractResult(output string) string {
	for _, line := range reverseTrimmedLines(output) {
		if line == "" || isCompletionMarkerLine(line) {
			continue
		}
		if match := verdictPattern.FindStringSubmatch(line); match != nil {
			return match[1]
		}
		return ""
	}
	return ""
}

// summarize keeps the tail of the assistant text: the end of a turn is where
// the conclusion lives.
func summarize(output string) string {
	trimmed := strings.TrimSpace(strings.ReplaceAll(output, "\r\n", "\n"))
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	if len(runes) <= maxSummaryChars {
		return trimmed
	}
	return "…" + string(runes[len(runes)-maxSummaryChars:])
}

// changedFiles diffs two `git status --porcelain` captures and returns the
// paths that appeared or changed state. Renames report their destination.
func changedFiles(before, after GitSnapshot) []string {
	if !after.Repository {
		return nil
	}
	previous := porcelainEntries(before.Status)
	changed := make([]string, 0, 8)
	for path, code := range porcelainEntries(after.Status) {
		if previous[path] == code {
			continue
		}
		changed = append(changed, path)
	}
	sortStrings(changed)
	return changed
}

func porcelainEntries(status string) map[string]string {
	entries := map[string]string{}
	for _, line := range strings.Split(strings.ReplaceAll(status, "\r\n", "\n"), "\n") {
		if len(line) < 4 {
			continue
		}
		code := strings.TrimSpace(line[:2])
		path := strings.TrimSpace(line[3:])
		if path == "" {
			continue
		}
		// `R  old -> new` reports the destination, which is the file a reader
		// would open.
		if arrow := strings.Index(path, " -> "); arrow >= 0 {
			path = strings.TrimSpace(path[arrow+4:])
		}
		path = strings.Trim(path, `"`)
		if path != "" {
			entries[path] = code
		}
	}
	return entries
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

// History returns the recorded runs of one task, newest first.
func (s *Service) History(
	ctx context.Context,
	id ID,
	callerEmail string,
	isAdmin bool,
) ([]RunRecord, error) {
	if _, err := s.Get(ctx, id, callerEmail, isAdmin); err != nil {
		return nil, err
	}
	if s.history == nil {
		return []RunRecord{}, nil
	}
	records, err := s.history.List(ctx, id)
	if err != nil {
		return nil, err
	}
	reversed := make([]RunRecord, 0, len(records))
	for index := len(records) - 1; index >= 0; index-- {
		reversed = append(reversed, records[index])
	}
	return reversed, nil
}

// RunDiff returns the stored diff stat of one recorded run, enriched with
// `git show --stat` when that run left a commit behind and the workspace is
// still readable.
func (s *Service) RunDiff(
	ctx context.Context,
	id ID,
	runID string,
	callerEmail string,
	isAdmin bool,
) (RunDiff, error) {
	task, err := s.Get(ctx, id, callerEmail, isAdmin)
	if err != nil {
		return RunDiff{}, err
	}
	if s.history == nil {
		return RunDiff{}, ErrRunNotFound
	}
	records, err := s.history.List(ctx, id)
	if err != nil {
		return RunDiff{}, err
	}
	runID = strings.TrimSpace(runID)
	for _, record := range records {
		if record.RunID != runID {
			continue
		}
		diff := RunDiff{
			RunID:        record.RunID,
			TaskID:       id,
			FilesChanged: record.FilesChanged,
			DiffStat:     record.DiffStat,
			CommitSHA:    record.CommitSHA,
		}
		switch {
		case record.CommitSHA == "":
			diff.Unavailable = "this run left no commit"
		case s.workspace == nil:
			diff.Unavailable = "the workspace is not reachable from this deployment"
		default:
			stat, showErr := s.workspace.GitShowStat(ctx, task.ProjectID, record.CommitSHA)
			if showErr != nil {
				diff.Unavailable = "could not read the commit: " + showErr.Error()
			} else {
				diff.CommitStat = stat
			}
		}
		return diff, nil
	}
	return RunDiff{}, ErrRunNotFound
}
