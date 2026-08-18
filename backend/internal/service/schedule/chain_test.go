package schedule

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestValidateChainRejectsBrokenGraphs(t *testing.T) {
	t.Parallel()
	catalog := []Task{
		{ID: "aaaaaaaaaaaaaaaaaaaaaaaa", ProjectID: testProjectID},
		{ID: "bbbbbbbbbbbbbbbbbbbbbbbb", ProjectID: testProjectID},
		{ID: "cccccccccccccccccccccccc", ProjectID: "ffff0000"},
	}

	tests := []struct {
		name    string
		task    Task
		catalog []Task
		want    error
	}{
		{
			name:    "no chain is always valid",
			task:    Task{ID: "aaaaaaaaaaaaaaaaaaaaaaaa", ProjectID: testProjectID},
			catalog: catalog,
		},
		{
			name: "valid single link",
			task: Task{
				ID: "aaaaaaaaaaaaaaaaaaaaaaaa", ProjectID: testProjectID,
				Next: []ChainLink{{TaskID: "bbbbbbbbbbbbbbbbbbbbbbbb", When: ChainWhenSuccess}},
			},
			catalog: catalog,
		},
		{
			name: "self reference",
			task: Task{
				ID: "aaaaaaaaaaaaaaaaaaaaaaaa", ProjectID: testProjectID,
				Next: []ChainLink{{TaskID: "aaaaaaaaaaaaaaaaaaaaaaaa", When: ChainWhenAlways}},
			},
			catalog: catalog,
			want:    ErrChainCycle,
		},
		{
			name: "two-task cycle",
			task: Task{
				ID: "aaaaaaaaaaaaaaaaaaaaaaaa", ProjectID: testProjectID,
				Next: []ChainLink{{TaskID: "bbbbbbbbbbbbbbbbbbbbbbbb", When: ChainWhenSuccess}},
			},
			catalog: []Task{
				{
					ID: "bbbbbbbbbbbbbbbbbbbbbbbb", ProjectID: testProjectID,
					Next: []ChainLink{{TaskID: "aaaaaaaaaaaaaaaaaaaaaaaa", When: ChainWhenSuccess}},
				},
			},
			want: ErrChainCycle,
		},
		{
			name: "missing target",
			task: Task{
				ID: "aaaaaaaaaaaaaaaaaaaaaaaa", ProjectID: testProjectID,
				Next: []ChainLink{{TaskID: "dddddddddddddddddddddddd", When: ChainWhenSuccess}},
			},
			catalog: catalog,
			want:    ErrChainTargetNotFound,
		},
		{
			name: "cross-project target",
			task: Task{
				ID: "aaaaaaaaaaaaaaaaaaaaaaaa", ProjectID: testProjectID,
				Next: []ChainLink{{TaskID: "cccccccccccccccccccccccc", When: ChainWhenSuccess}},
			},
			catalog: catalog,
			want:    ErrChainCrossProject,
		},
		{
			name: "unknown condition",
			task: Task{
				ID: "aaaaaaaaaaaaaaaaaaaaaaaa", ProjectID: testProjectID,
				Next: []ChainLink{{TaskID: "bbbbbbbbbbbbbbbbbbbbbbbb", When: "maybe"}},
			},
			catalog: catalog,
			want:    ErrInvalidChainWhen,
		},
		{
			name: "delay beyond a week",
			task: Task{
				ID: "aaaaaaaaaaaaaaaaaaaaaaaa", ProjectID: testProjectID,
				Next: []ChainLink{{
					TaskID: "bbbbbbbbbbbbbbbbbbbbbbbb", When: ChainWhenAlways, DelayMin: 20000,
				}},
			},
			catalog: catalog,
			want:    ErrInvalidChainDelay,
		},
		{
			name: "chain longer than the depth bound",
			task: Task{
				ID: "aaaaaaaaaaaaaaaaaaaaaaaa", ProjectID: testProjectID,
				Next: []ChainLink{{TaskID: chainID(0), When: ChainWhenSuccess}},
			},
			catalog: longChain(MaxChainDepth + 2),
			want:    ErrChainTooDeep,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := validateChain(test.task, test.catalog)
			if !errors.Is(err, test.want) {
				t.Fatalf("validateChain error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestChainTargetsFilterByOutcome(t *testing.T) {
	t.Parallel()
	task := Task{Next: []ChainLink{
		{TaskID: "aaaaaaaaaaaaaaaaaaaaaaaa", When: ChainWhenSuccess},
		{TaskID: "bbbbbbbbbbbbbbbbbbbbbbbb", When: ChainWhenFailure},
		{TaskID: "cccccccccccccccccccccccc", When: ChainWhenAlways},
	}}

	tests := []struct {
		name   string
		failed bool
		want   []ID
	}{
		{name: "success", failed: false, want: []ID{"aaaaaaaaaaaaaaaaaaaaaaaa", "cccccccccccccccccccccccc"}},
		{name: "failure", failed: true, want: []ID{"bbbbbbbbbbbbbbbbbbbbbbbb", "cccccccccccccccccccccccc"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got := chainTargets(task, test.failed)
			if len(got) != len(test.want) {
				t.Fatalf("targets = %#v, want %v", got, test.want)
			}
			for index, link := range got {
				if link.TaskID != test.want[index] {
					t.Fatalf("targets = %#v, want %v", got, test.want)
				}
			}
		})
	}
}

func TestCreateRejectsCycleAndUpdateAcceptsChain(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	service := newTestService(repo, now)
	ctx := context.Background()

	first, err := service.Create(ctx, cronInput("first", "0 3 * * *"), testOwner, false)
	if err != nil {
		t.Fatal(err)
	}
	second, err := service.Create(ctx, cronInput("second", "0 4 * * *"), testOwner, false)
	if err != nil {
		t.Fatal(err)
	}

	links := []ChainLink{{TaskID: second.ID, When: ChainWhenSuccess}}
	if _, err := service.Update(ctx, first.ID, UpdateInput{Next: &links}, testOwner, false); err != nil {
		t.Fatal(err)
	}

	back := []ChainLink{{TaskID: first.ID, When: ChainWhenAlways}}
	_, err = service.Update(ctx, second.ID, UpdateInput{Next: &back}, testOwner, false)
	if !errors.Is(err, ErrChainCycle) {
		t.Fatalf("closing the loop error = %v, want ErrChainCycle", err)
	}

	stored, _ := repo.Get(ctx, second.ID)
	if len(stored.Next) != 0 {
		t.Fatalf("a rejected chain was persisted: %#v", stored.Next)
	}
}

func TestSettledRunArmsChainedTaskAndBoundsDepth(t *testing.T) {
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	executor := &fakeExecutor{repo: repo}
	service := newTestServiceWithExecutor(repo, &now, executor)
	ctx := context.Background()

	target, err := service.Create(ctx, cronInput("nightly backup verify", "0 5 1 1 *"), testOwner, false)
	if err != nil {
		t.Fatal(err)
	}
	source, err := service.Create(ctx, cronInput("nightly backup", "0 4 * * *"), testOwner, false)
	if err != nil {
		t.Fatal(err)
	}
	links := []ChainLink{{TaskID: target.ID, When: ChainWhenSuccess, DelayMin: 5}}
	if _, err := service.Update(ctx, source.ID, UpdateInput{Next: &links}, testOwner, false); err != nil {
		t.Fatal(err)
	}

	now = now.Add(20 * time.Hour)
	if err := service.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	if len(executor.starts) != 1 {
		t.Fatalf("starts = %d, want 1", len(executor.starts))
	}
	executor.starts[0].handle.done <- RunResult{Output: "Backup written.\n<<RESULT: ok>>"}

	armed := waitForTask(t, repo, target.ID, func(task Task) bool { return task.PendingRun })
	if armed.PendingChain == nil || armed.PendingChain.Depth != 2 || armed.PendingChain.FromTaskID != source.ID {
		t.Fatalf("chain context not recorded: %#v", armed.PendingChain)
	}
	if armed.RetryAt != now.UnixMilli()+5*60_000 {
		t.Fatalf("delay not applied: retryAt=%d now=%d", armed.RetryAt, now.UnixMilli())
	}

	settled := waitForTask(t, repo, source.ID, func(task Task) bool { return task.ActiveRunID == "" })
	if settled.LastResult != "ok" {
		t.Fatalf("verdict not stored: %q", settled.LastResult)
	}
}

func TestChainSkipsDisabledTarget(t *testing.T) {
	now := time.Date(2026, time.July, 23, 12, 0, 0, 0, time.UTC)
	repo := newMemoryRepository()
	executor := &fakeExecutor{repo: repo}
	service := newTestServiceWithExecutor(repo, &now, executor)
	ctx := context.Background()

	target, err := service.Create(ctx, cronInput("parked", "0 5 1 1 *"), testOwner, false)
	if err != nil {
		t.Fatal(err)
	}
	source, err := service.Create(ctx, cronInput("source", "0 4 * * *"), testOwner, false)
	if err != nil {
		t.Fatal(err)
	}
	links := []ChainLink{{TaskID: target.ID, When: ChainWhenAlways}}
	if _, err := service.Update(ctx, source.ID, UpdateInput{Next: &links}, testOwner, false); err != nil {
		t.Fatal(err)
	}
	disabled := false
	if _, err := service.Update(ctx, target.ID, UpdateInput{Enabled: &disabled}, testOwner, false); err != nil {
		t.Fatal(err)
	}

	now = now.Add(20 * time.Hour)
	if err := service.RunDue(ctx); err != nil {
		t.Fatal(err)
	}
	executor.starts[0].handle.done <- RunResult{Output: "done"}
	waitForTask(t, repo, source.ID, func(task Task) bool { return task.ActiveRunID == "" })

	parked, _ := repo.Get(ctx, target.ID)
	if parked.PendingRun {
		t.Fatal("a paused task was armed through a chain")
	}
}

func chainID(index int) ID {
	digits := "0123456789abcdef"
	raw := make([]byte, 24)
	for position := range raw {
		raw[position] = digits[(index+position)%16]
	}
	return ID(raw)
}

// longChain builds a straight line of tasks so depth validation has something
// deeper than MaxChainDepth to reject.
func longChain(length int) []Task {
	tasks := make([]Task, 0, length)
	for index := 0; index < length; index++ {
		task := Task{ID: chainID(index), ProjectID: testProjectID}
		if index+1 < length {
			task.Next = []ChainLink{{TaskID: chainID(index + 1), When: ChainWhenAlways}}
		}
		tasks = append(tasks, task)
	}
	return tasks
}

func cronInput(name, cron string) CreateInput {
	return CreateInput{
		Name:      name,
		ProjectID: testProjectID,
		ChatID:    testChatID,
		Prompt:    "Do the standing work.",
		Kind:      KindCron,
		Cron:      cron,
		Timezone:  "UTC",
	}
}
