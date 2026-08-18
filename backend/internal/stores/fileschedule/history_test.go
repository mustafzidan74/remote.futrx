package fileschedule

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	serviceschedule "github.com/futrx-com/remote.futrx.com/internal/service/schedule"
)

const historyTaskID serviceschedule.ID = "0123456789abcdef01234567"

func TestHistoryAppendTrimsToTheBound(t *testing.T) {
	t.Parallel()
	store := newTestHistory(t)
	ctx := context.Background()

	total := serviceschedule.MaxHistoryRuns + 15
	for index := 0; index < total; index++ {
		if err := store.Append(ctx, historyTaskID, serviceschedule.RunRecord{
			RunID:     strconv.Itoa(index),
			StartedAt: int64(index),
			Status:    serviceschedule.HistoryOK,
		}); err != nil {
			t.Fatal(err)
		}
	}

	records, err := store.List(ctx, historyTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != serviceschedule.MaxHistoryRuns {
		t.Fatalf("records = %d, want %d", len(records), serviceschedule.MaxHistoryRuns)
	}
	if records[0].RunID != strconv.Itoa(total-serviceschedule.MaxHistoryRuns) {
		t.Fatalf("oldest kept record = %q", records[0].RunID)
	}
	if records[len(records)-1].RunID != strconv.Itoa(total-1) {
		t.Fatalf("newest record = %q", records[len(records)-1].RunID)
	}
	if records[0].TaskID != historyTaskID {
		t.Fatalf("task id was not stamped: %q", records[0].TaskID)
	}
}

func TestHistoryRoundTripsEveryField(t *testing.T) {
	t.Parallel()
	store := newTestHistory(t)
	ctx := context.Background()
	cost := 1.25

	want := serviceschedule.RunRecord{
		RunID:          "run-1",
		ChatID:         "abcd1234",
		StartedAt:      1000,
		FinishedAt:     4000,
		Status:         serviceschedule.HistorySkipped,
		Summary:        "nothing to do",
		Result:         "SCORE=94",
		SkippedByGate:  true,
		GateReason:     "weekday Friday is not in the allowed set",
		Chain:          &serviceschedule.ChainRun{Depth: 2, Total: 3},
		ChainTriggered: []serviceschedule.ID{"aaaaaaaaaaaaaaaaaaaaaaaa"},
		Tokens:         42,
		CostUSD:        &cost,
		FilesChanged:   []string{"a.ts", "b.ts"},
		DiffStat:       " a.ts | 2 +-",
		CommitSHA:      "abcdef1234567",
	}
	if err := store.Append(ctx, historyTaskID, want); err != nil {
		t.Fatal(err)
	}

	records, err := store.List(ctx, historyTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 {
		t.Fatalf("records = %d, want 1", len(records))
	}
	got := records[0]
	if got.Status != want.Status || got.Result != want.Result || got.GateReason != want.GateReason {
		t.Fatalf("record = %#v", got)
	}
	if got.Chain == nil || got.Chain.Depth != 2 || got.Chain.Total != 3 {
		t.Fatalf("chain = %#v", got.Chain)
	}
	if got.CostUSD == nil || *got.CostUSD != cost || got.Tokens != 42 {
		t.Fatalf("usage = %#v", got)
	}
	if len(got.FilesChanged) != 2 || got.DiffStat != want.DiffStat || got.CommitSHA != want.CommitSHA {
		t.Fatalf("diff fields = %#v", got)
	}
	if got.DurationMs() != 3000 {
		t.Fatalf("duration = %d, want 3000", got.DurationMs())
	}
}

func TestHistorySkipsCorruptLinesAndRejectsBadIDs(t *testing.T) {
	t.Parallel()
	store := newTestHistory(t)
	ctx := context.Background()

	if err := store.Append(ctx, historyTaskID, serviceschedule.RunRecord{RunID: "good"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(store.dir, string(historyTaskID)+".jsonl")
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("{\"runId\": \"trunc\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()

	records, err := store.List(ctx, historyTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].RunID != "good" {
		t.Fatalf("a damaged tail must not hide the readable records: %#v", records)
	}

	for _, id := range []serviceschedule.ID{"", "short", "../../escape", "ZZZZZZZZZZZZZZZZZZZZZZZZ"} {
		if _, err := store.List(ctx, id); !errors.Is(err, serviceschedule.ErrInvalidID) {
			t.Fatalf("List(%q) error = %v, want ErrInvalidID", id, err)
		}
		if err := store.Append(ctx, id, serviceschedule.RunRecord{}); !errors.Is(err, serviceschedule.ErrInvalidID) {
			t.Fatalf("Append(%q) error = %v, want ErrInvalidID", id, err)
		}
	}
}

func TestHistoryDeleteIsIdempotent(t *testing.T) {
	t.Parallel()
	store := newTestHistory(t)
	ctx := context.Background()

	if err := store.Append(ctx, historyTaskID, serviceschedule.RunRecord{RunID: "one"}); err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		if err := store.Delete(ctx, historyTaskID); err != nil {
			t.Fatalf("delete attempt %d: %v", attempt, err)
		}
	}
	records, err := store.List(ctx, historyTaskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 0 {
		t.Fatalf("records after delete = %d", len(records))
	}
}

func newTestHistory(t *testing.T) *HistoryStore {
	t.Helper()
	store, err := NewHistory(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return store
}
