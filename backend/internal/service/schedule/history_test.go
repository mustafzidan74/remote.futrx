package schedule

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

func TestExtractResult(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{name: "empty output", output: "", want: ""},
		{name: "no marker", output: "All good.", want: ""},
		{
			name:   "marker on the last line",
			output: "Checked 12 pages.\n<<RESULT: SCORE=94>>",
			want:   "SCORE=94",
		},
		{
			name:   "marker with trailing blank lines",
			output: "done\n<<RESULT: clean>>\n\n   \n",
			want:   "clean",
		},
		{
			name:   "marker with windows line endings",
			output: "done\r\n<<RESULT: clean>>\r\n",
			want:   "clean",
		},
		{
			name:   "marker without inner spaces",
			output: "<<RESULT:clean>>",
			want:   "clean",
		},
		{
			name:   "empty marker payload",
			output: "<<RESULT: >>",
			want:   "",
		},
		{
			name:   "marker that is not the last line is ignored",
			output: "<<RESULT: stale>>\nand then more prose",
			want:   "",
		},
		{
			name:   "marker mentioned inside prose is ignored",
			output: "print <<RESULT: x>> at the end",
			want:   "",
		},
		{
			name:   "verdict survives a trailing completion marker",
			output: "done\n<<RESULT: OK>>\nSCHEDULE_STATUS=COMPLETE",
			want:   "OK",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := ExtractResult(test.output); got != test.want {
				t.Fatalf("ExtractResult(%q) = %q, want %q", test.output, got, test.want)
			}
		})
	}
}

func TestSummarizeKeepsTheTail(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("x", maxSummaryChars+120)
	got := summarize(long)
	if len([]rune(got)) != maxSummaryChars+1 {
		t.Fatalf("summary length = %d runes", len([]rune(got)))
	}
	if !strings.HasPrefix(got, "…") {
		t.Fatalf("a trimmed summary should be marked: %q", got[:8])
	}
	if summarize("  hello  ") != "hello" {
		t.Fatal("short output should pass through trimmed")
	}
}

func TestChangedFilesDiffsPorcelainCaptures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		before GitSnapshot
		after  GitSnapshot
		want   []string
	}{
		{
			name:   "not a repository",
			before: GitSnapshot{Repository: true},
			after:  GitSnapshot{},
			want:   nil,
		},
		{
			name:   "new modification",
			before: GitSnapshot{Repository: true, Status: " M src/a.ts\n"},
			after:  GitSnapshot{Repository: true, Status: " M src/a.ts\n?? src/b.ts\n"},
			want:   []string{"src/b.ts"},
		},
		{
			name:   "state change on the same path",
			before: GitSnapshot{Repository: true, Status: "?? src/a.ts\n"},
			after:  GitSnapshot{Repository: true, Status: "A  src/a.ts\n"},
			want:   []string{"src/a.ts"},
		},
		{
			name:   "rename reports its destination",
			before: GitSnapshot{Repository: true},
			after:  GitSnapshot{Repository: true, Status: "R  old.ts -> new.ts\n"},
			want:   []string{"new.ts"},
		},
		{
			name:   "nothing changed",
			before: GitSnapshot{Repository: true, Status: " M a\n M b\n"},
			after:  GitSnapshot{Repository: true, Status: " M a\n M b\n"},
			want:   []string{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := changedFiles(test.before, test.after)
			if len(got) != len(test.want) {
				t.Fatalf("changedFiles = %#v, want %#v", got, test.want)
			}
			for index, path := range got {
				if path != test.want[index] {
					t.Fatalf("changedFiles = %#v, want %#v", got, test.want)
				}
			}
		})
	}
}

func TestSettledRunAppendsHistoryWithFilesAndUsage(t *testing.T) {
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	executor := &fakeExecutor{repo: repo}
	history := &memoryHistory{}
	workspace := &sequencedWorkspace{snapshots: []GitSnapshot{
		{Repository: true, Head: "aaaaaaa", Status: ""},
		{Repository: true, Head: "bbbbbbb", Status: "?? notes.md\n", DiffStat: " notes.md | 3 +++"},
	}}
	cost := 0.42
	service := New(
		repo,
		&fakeChats{meta: validChat()},
		&fakeAccess{allowed: true},
		&fakeIdentities{registered: true},
		executor,
		WithNow(func() time.Time { return now }),
		WithHistory(history),
		WithWorkspace(workspace),
		WithUsageLookup(stubUsage{usage: RunUsage{Tokens: 1234, CostUSD: &cost}}),
	)
	ctx := context.Background()

	task, err := service.Create(ctx, validOnceInput(now), testOwner, false)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Hour)
	if err := service.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	executor.starts[0].handle.done <- RunResult{Output: "Wrote notes.\n<<RESULT: SCORE=94>>"}
	waitForTask(t, repo, task.ID, func(task Task) bool { return task.ActiveRunID == "" })

	records := waitForHistory(t, history, task.ID, 1)
	record := records[0]
	if record.Status != HistoryOK || record.Result != "SCORE=94" {
		t.Fatalf("record = %#v", record)
	}
	if len(record.FilesChanged) != 1 || record.FilesChanged[0] != "notes.md" {
		t.Fatalf("filesChanged = %#v", record.FilesChanged)
	}
	if record.DiffStat != "notes.md | 3 +++" {
		t.Fatalf("diffStat = %q", record.DiffStat)
	}
	if record.CommitSHA != "bbbbbbb" {
		t.Fatalf("commit = %q", record.CommitSHA)
	}
	if record.Tokens != 1234 || record.CostUSD == nil || *record.CostUSD != cost {
		t.Fatalf("usage not recorded: %#v", record)
	}
	if record.DurationMs() < 0 {
		t.Fatalf("duration = %d", record.DurationMs())
	}
}

func TestHistoryAndRunDiffAuthorizeTheCaller(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	history := &memoryHistory{}
	service := New(
		repo,
		&fakeChats{meta: validChat()},
		&fakeAccess{allowed: true},
		&fakeIdentities{registered: true},
		&fakeExecutor{},
		WithNow(func() time.Time { return now }),
		WithHistory(history),
		WithWorkspace(stubWorkspace{show: " a.ts | 2 +-"}),
	)
	ctx := context.Background()

	task, err := service.Create(ctx, validOnceInput(now), testOwner, false)
	if err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 3; index++ {
		if err := history.Append(ctx, task.ID, RunRecord{
			RunID:     string(rune('a'+index)) + "run",
			StartedAt: now.UnixMilli(),
			Status:    HistoryOK,
			CommitSHA: "abcdef1234",
		}); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := service.History(ctx, task.ID, "stranger@example.com", false); err == nil {
		t.Fatal("history must not be readable by a non-owner")
	}
	records, err := service.History(ctx, task.ID, testOwner, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 || records[0].RunID != "crun" {
		t.Fatalf("history should be newest first: %#v", records)
	}

	diff, err := service.RunDiff(ctx, task.ID, "brun", testOwner, false)
	if err != nil {
		t.Fatal(err)
	}
	if diff.CommitStat != " a.ts | 2 +-" {
		t.Fatalf("commit stat = %q", diff.CommitStat)
	}
	if _, err := service.RunDiff(ctx, task.ID, "missing", testOwner, false); err != ErrRunNotFound {
		t.Fatalf("unknown run error = %v, want ErrRunNotFound", err)
	}
}

type memoryHistory struct {
	mu      sync.Mutex
	records map[ID][]RunRecord
}

func (h *memoryHistory) Append(_ context.Context, taskID ID, record RunRecord) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.records == nil {
		h.records = map[ID][]RunRecord{}
	}
	record.TaskID = taskID
	entries := append(h.records[taskID], record)
	if len(entries) > MaxHistoryRuns {
		entries = entries[len(entries)-MaxHistoryRuns:]
	}
	h.records[taskID] = entries
	return nil
}

func (h *memoryHistory) List(_ context.Context, taskID ID) ([]RunRecord, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]RunRecord(nil), h.records[taskID]...), nil
}

func (h *memoryHistory) Delete(_ context.Context, taskID ID) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.records, taskID)
	return nil
}

type recordingObserver struct {
	mu      sync.Mutex
	tasks   []Task
	results []RunResult
}

func (o *recordingObserver) ScheduledRunFinished(_ context.Context, task Task, result RunResult) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.tasks = append(o.tasks, task)
	o.results = append(o.results, result)
}

// sequencedWorkspace hands out one snapshot per call so a test can describe
// the workspace before and after a run.
type sequencedWorkspace struct {
	mu        sync.Mutex
	snapshots []GitSnapshot
	calls     int
}

func (w *sequencedWorkspace) RunCommand(
	context.Context, serviceproject.ID, string, time.Duration,
) (CommandResult, error) {
	return CommandResult{}, nil
}

func (w *sequencedWorkspace) GitSnapshot(context.Context, serviceproject.ID) (GitSnapshot, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.calls >= len(w.snapshots) {
		return w.snapshots[len(w.snapshots)-1], nil
	}
	snapshot := w.snapshots[w.calls]
	w.calls++
	return snapshot, nil
}

func (w *sequencedWorkspace) GitShowStat(context.Context, serviceproject.ID, string) (string, error) {
	return "", nil
}

type stubUsage struct {
	usage RunUsage
}

func (u stubUsage) RunUsage(context.Context, servicechat.ID, int64, int64) (RunUsage, bool) {
	return u.usage, true
}

func waitForHistory(t *testing.T, history *memoryHistory, id ID, want int) []RunRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		records, _ := history.List(context.Background(), id)
		if len(records) >= want {
			return records
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("history never reached %d records", want)
	return nil
}

func TestCompletionMarkerStepsOverAVerdict(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		output string
		want   bool
	}{
		{name: "plain completion", output: "done\nSCHEDULE_STATUS=COMPLETE", want: true},
		{
			name:   "completion after a verdict",
			output: "done\n<<RESULT: OK>>\nSCHEDULE_STATUS=COMPLETE",
			want:   true,
		},
		{
			name:   "completion before a verdict",
			output: "done\nSCHEDULE_STATUS=COMPLETE\n<<RESULT: OK>>",
			want:   true,
		},
		{name: "verdict only", output: "done\n<<RESULT: OK>>", want: false},
		{name: "neither marker", output: "still working", want: false},
		{name: "empty", output: "   \n\n", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if got := HasCompletionMarker(test.output); got != test.want {
				t.Fatalf("HasCompletionMarker(%q) = %t, want %t", test.output, got, test.want)
			}
		})
	}
}
