package schedule

import (
	"context"
	"errors"
	"testing"
	"time"

	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

func TestValidateConditionRejectsIncompleteGates(t *testing.T) {
	t.Parallel()
	expect := func(value int) *int { return &value }

	tests := []struct {
		name      string
		condition *Condition
		want      error
	}{
		{name: "no gate", condition: nil},
		{
			name:      "outputContains with a pattern",
			condition: &Condition{Kind: ConditionOutputContains, Pattern: "^ok$", InLastRunOf: SelfTask},
		},
		{
			name:      "outputContains without a pattern",
			condition: &Condition{Kind: ConditionOutputContains, InLastRunOf: SelfTask},
			want:      ErrGatePatternRequired,
		},
		{
			name:      "outputContains with a broken regex",
			condition: &Condition{Kind: ConditionOutputContains, Pattern: "([a-z", InLastRunOf: SelfTask},
			want:      ErrGateInvalidPattern,
		},
		{
			name:      "outputContains referencing a non-id",
			condition: &Condition{Kind: ConditionOutputContains, Pattern: "ok", InLastRunOf: "not-an-id"},
			want:      ErrGateInvalidReference,
		},
		{
			name:      "httpStatus with a url",
			condition: &Condition{Kind: ConditionHTTPStatus, URL: "https://example.test/health"},
		},
		{
			name:      "httpStatus with a bare host",
			condition: &Condition{Kind: ConditionHTTPStatus, URL: "example.test"},
			want:      ErrGateInvalidURL,
		},
		{
			name:      "httpStatus with an impossible code",
			condition: &Condition{Kind: ConditionHTTPStatus, URL: "https://a.test", Expect: expect(42)},
			want:      ErrGateInvalidExpect,
		},
		{
			name:      "commandExitCode with a command",
			condition: &Condition{Kind: ConditionCommandExit, Command: "test -f build.lock"},
		},
		{
			name:      "commandExitCode without a command",
			condition: &Condition{Kind: ConditionCommandExit},
			want:      ErrGateCommandRequired,
		},
		{
			name:      "weekdays in range",
			condition: &Condition{Kind: ConditionWeekdays, Weekdays: []int{1, 3, 5}},
		},
		{
			name:      "weekdays out of range",
			condition: &Condition{Kind: ConditionWeekdays, Weekdays: []int{7}},
			want:      ErrGateWeekdaysRequired,
		},
		{
			name:      "weekdays empty",
			condition: &Condition{Kind: ConditionWeekdays},
			want:      ErrGateWeekdaysRequired,
		},
		{
			name:      "notIfRanWithin positive",
			condition: &Condition{Kind: ConditionNotIfRanWithin, Minutes: 90},
		},
		{
			name:      "notIfRanWithin zero",
			condition: &Condition{Kind: ConditionNotIfRanWithin},
			want:      ErrGateInvalidMinutes,
		},
		{
			name:      "unknown kind",
			condition: &Condition{Kind: "vibes"},
			want:      ErrGateInvalidKind,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Validation always runs on the normalized shape, because that is
			// what create and update persist.
			task := Task{Condition: normalizeCondition(test.condition)}
			if err := validateCondition(task); !errors.Is(err, test.want) {
				t.Fatalf("validateCondition error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestEvaluateGateEachKind(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC) // a Thursday
	expect := func(value int) *int { return &value }

	tests := []struct {
		name       string
		task       Task
		probe      HTTPProbe
		workspace  Workspace
		wantPassed bool
	}{
		{
			name:       "weekdays allows today",
			task:       Task{Timezone: "UTC", Condition: &Condition{Kind: ConditionWeekdays, Weekdays: []int{4}}},
			wantPassed: true,
		},
		{
			name: "weekdays blocks today",
			task: Task{Timezone: "UTC", Condition: &Condition{Kind: ConditionWeekdays, Weekdays: []int{0, 6}}},
		},
		{
			name: "weekdays follows the task timezone",
			// 12:00 UTC Thursday is 23:00 Thursday in Auckland but the same
			// weekday, so shift far enough to cross the date line westward.
			task: Task{
				Timezone:  "Pacific/Kiritimati",
				Condition: &Condition{Kind: ConditionWeekdays, Weekdays: []int{4}},
			},
			wantPassed: false,
		},
		{
			name: "notIfRanWithin blocks a recent run",
			task: Task{
				LastRunEnd: now.Add(-10 * time.Minute).UnixMilli(),
				Condition:  &Condition{Kind: ConditionNotIfRanWithin, Minutes: 60},
			},
		},
		{
			name: "notIfRanWithin allows an old run",
			task: Task{
				LastRunEnd: now.Add(-2 * time.Hour).UnixMilli(),
				Condition:  &Condition{Kind: ConditionNotIfRanWithin, Minutes: 60},
			},
			wantPassed: true,
		},
		{
			name:       "notIfRanWithin allows a first run",
			task:       Task{Condition: &Condition{Kind: ConditionNotIfRanWithin, Minutes: 60}},
			wantPassed: true,
		},
		{
			name: "outputContains matches the stored verdict",
			task: Task{
				LastResult: "DRIFT",
				Condition: &Condition{
					Kind: ConditionOutputContains, Pattern: "DRIFT", InLastRunOf: SelfTask,
				},
			},
			wantPassed: true,
		},
		{
			name: "outputContains rejects a mismatch",
			task: Task{
				LastResult: "clean",
				Condition: &Condition{
					Kind: ConditionOutputContains, Pattern: "DRIFT", InLastRunOf: SelfTask,
				},
			},
		},
		{
			name: "outputContains with nothing recorded fails closed",
			task: Task{
				Condition: &Condition{
					Kind: ConditionOutputContains, Pattern: "DRIFT", InLastRunOf: SelfTask,
				},
			},
		},
		{
			name:       "httpStatus matches",
			task:       Task{Condition: &Condition{Kind: ConditionHTTPStatus, URL: "https://a.test/health"}},
			probe:      stubProbe{status: 200},
			wantPassed: true,
		},
		{
			name: "httpStatus mismatch",
			task: Task{Condition: &Condition{
				Kind: ConditionHTTPStatus, URL: "https://a.test/health", Expect: expect(204),
			}},
			probe: stubProbe{status: 200},
		},
		{
			name:  "httpStatus transport failure fails closed",
			task:  Task{Condition: &Condition{Kind: ConditionHTTPStatus, URL: "https://a.test/health"}},
			probe: stubProbe{err: errors.New("dial timeout")},
		},
		{
			name:       "commandExitCode matches",
			task:       Task{Condition: &Condition{Kind: ConditionCommandExit, Command: "git diff --quiet"}},
			workspace:  stubWorkspace{result: CommandResult{ExitCode: 0}},
			wantPassed: true,
		},
		{
			name:      "commandExitCode mismatch",
			task:      Task{Condition: &Condition{Kind: ConditionCommandExit, Command: "git diff --quiet"}},
			workspace: stubWorkspace{result: CommandResult{ExitCode: 1, Output: "dirty"}},
		},
		{
			name: "commandExitCode with no runner fails closed",
			task: Task{Condition: &Condition{Kind: ConditionCommandExit, Command: "true"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			repo := newMemoryRepository()
			options := []Option{WithNow(func() time.Time { return now })}
			if test.probe != nil {
				options = append(options, WithHTTPProbe(test.probe))
			}
			if test.workspace != nil {
				options = append(options, WithWorkspace(test.workspace))
			}
			service := New(
				repo,
				&fakeChats{meta: validChat()},
				&fakeAccess{allowed: true},
				&fakeIdentities{registered: true},
				&fakeExecutor{},
				options...,
			)
			outcome := service.evaluateGate(context.Background(), test.task, now)
			if outcome.Passed != test.wantPassed {
				t.Fatalf("passed = %t (%s), want %t", outcome.Passed, outcome.Reason, test.wantPassed)
			}
			if !outcome.Passed && outcome.Reason == "" {
				t.Fatal("a closed gate must explain itself")
			}
		})
	}
}

func TestOutputContainsReadsAnotherTask(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	service := newTestService(repo, now)
	ctx := context.Background()

	upstream, err := service.Create(ctx, cronInput("audit", "0 3 * * *"), testOwner, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Update(ctx, upstream.ID, func(task *Task) error {
		task.LastResult = "SCORE=42"
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	gated := Task{
		ProjectID: testProjectID,
		Condition: &Condition{
			Kind:        ConditionOutputContains,
			Pattern:     `SCORE=\d+`,
			InLastRunOf: string(upstream.ID),
		},
	}
	if outcome := service.evaluateGate(ctx, gated, now); !outcome.Passed {
		t.Fatalf("gate should pass: %s", outcome.Reason)
	}

	gated.Condition.Pattern = "CLEAN"
	if outcome := service.evaluateGate(ctx, gated, now); outcome.Passed {
		t.Fatal("gate should not pass on a mismatch")
	}
}

func TestClosedGateSkipsOccurrenceWithoutStartingAnAgent(t *testing.T) {
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	executor := &fakeExecutor{repo: repo}
	history := &memoryHistory{}
	observer := &recordingObserver{}
	service := New(
		repo,
		&fakeChats{meta: validChat()},
		&fakeAccess{allowed: true},
		&fakeIdentities{registered: true},
		executor,
		WithNow(func() time.Time { return now }),
		WithHistory(history),
		WithRunObserver(observer),
	)
	ctx := context.Background()

	input := cronInput("weekly audit", "0 4 * * *")
	input.Condition = &Condition{Kind: ConditionWeekdays, Weekdays: []int{0}} // Sundays only
	task, err := service.Create(ctx, input, testOwner, false)
	if err != nil {
		t.Fatal(err)
	}

	now = now.Add(20 * time.Hour) // a Friday, gate closed
	if err := service.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	if len(executor.starts) != 0 {
		t.Fatalf("a closed gate started %d runs", len(executor.starts))
	}

	stored, _ := repo.Get(ctx, task.ID)
	if stored.LastStatus != RunStatusSkipped {
		t.Fatalf("last status = %q, want skipped", stored.LastStatus)
	}
	if stored.RunCount != 0 {
		t.Fatalf("run count = %d, a gate skip must not burn the maxRuns budget", stored.RunCount)
	}
	if stored.NextRunAt <= now.UnixMilli() {
		t.Fatalf("deadline was not advanced past now: %d", stored.NextRunAt)
	}

	records := history.records[task.ID]
	if len(records) != 1 || records[0].Status != HistorySkipped || !records[0].SkippedByGate {
		t.Fatalf("history = %#v", records)
	}
	if len(observer.results) != 1 || !observer.results[0].SkippedByGate {
		t.Fatalf("observer results = %#v", observer.results)
	}
}

func TestForcedRunBypassesGate(t *testing.T) {
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	executor := &fakeExecutor{repo: repo}
	service := newTestServiceWithExecutor(repo, &now, executor)
	ctx := context.Background()

	input := cronInput("weekly audit", "0 4 * * *")
	input.Condition = &Condition{Kind: ConditionWeekdays, Weekdays: []int{0}}
	task, err := service.Create(ctx, input, testOwner, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.RunNow(ctx, task.ID, testOwner, false); err != nil {
		t.Fatal(err)
	}
	if len(executor.starts) != 1 {
		t.Fatalf("forced run started %d runs, want 1", len(executor.starts))
	}
}

type stubProbe struct {
	status int
	err    error
}

func (p stubProbe) Status(context.Context, string) (int, error) {
	return p.status, p.err
}

type stubWorkspace struct {
	result   CommandResult
	err      error
	snapshot GitSnapshot
	show     string
}

func (w stubWorkspace) RunCommand(
	context.Context, serviceproject.ID, string, time.Duration,
) (CommandResult, error) {
	return w.result, w.err
}

func (w stubWorkspace) GitSnapshot(context.Context, serviceproject.ID) (GitSnapshot, error) {
	return w.snapshot, w.err
}

func (w stubWorkspace) GitShowStat(context.Context, serviceproject.ID, string) (string, error) {
	return w.show, w.err
}

// A notIfRanWithin gate must not count its own skip as a run: stamping
// lastRunAt on a skip would lock the task shut forever.
func TestGateSkipDoesNotCountAsARun(t *testing.T) {
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	executor := &fakeExecutor{repo: repo}
	service := New(
		repo,
		&fakeChats{meta: validChat()},
		&fakeAccess{allowed: true},
		&fakeIdentities{registered: true},
		executor,
		WithNow(func() time.Time { return now }),
	)
	ctx := context.Background()

	input := cronInput("hourly check", "0 * * * *")
	// The gate is closed only because a run happened 10 minutes ago.
	input.Condition = &Condition{Kind: ConditionNotIfRanWithin, Minutes: 30}
	task, err := service.Create(ctx, input, testOwner, false)
	if err != nil {
		t.Fatal(err)
	}
	// The first occurrence lands at 13:00; pretend a run finished at 12:50, so
	// the gate is closed by 10 minutes when that occurrence comes due.
	now = now.Add(time.Hour)
	lastRun := now.Add(-10 * time.Minute).UnixMilli()
	if _, err := repo.Update(ctx, task.ID, func(current *Task) error {
		current.LastRunAt = lastRun
		current.LastRunEnd = lastRun
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := service.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	if len(executor.starts) != 0 {
		t.Fatalf("a closed gate started %d runs", len(executor.starts))
	}
	skipped, _ := repo.Get(ctx, task.ID)
	if skipped.LastStatus != RunStatusSkipped {
		t.Fatalf("last status = %q, want skipped", skipped.LastStatus)
	}
	if skipped.LastRunAt != lastRun || skipped.LastRunEnd != lastRun {
		t.Fatalf("the skip moved lastRunAt to %d (want %d)", skipped.LastRunAt, lastRun)
	}

	// The window has now passed, so the very next occurrence must run.
	now = now.Add(time.Hour)
	if err := service.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	if len(executor.starts) != 1 {
		t.Fatalf("starts = %d, want 1 — the gate never reopened", len(executor.starts))
	}
}
