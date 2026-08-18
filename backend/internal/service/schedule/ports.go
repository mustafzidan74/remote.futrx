package schedule

import (
	"context"
	"time"

	servicechat "github.com/futrx-com/remote.futrx.com/internal/service/chat"
	serviceproject "github.com/futrx-com/remote.futrx.com/internal/service/project"
)

type Repository interface {
	List(ctx context.Context) ([]Task, error)
	Create(ctx context.Context, task Task) (Task, error)
	Get(ctx context.Context, id ID) (Task, error)
	Update(ctx context.Context, id ID, fn func(*Task) error) (Task, error)
	Delete(ctx context.Context, id ID) error
}

// HistoryRepository persists the bounded per-task run log. It is separate
// from Repository because history is append-only and lives in its own files:
// a task definition rewrite must never rewrite its history.
type HistoryRepository interface {
	Append(ctx context.Context, taskID ID, record RunRecord) error
	List(ctx context.Context, taskID ID) ([]RunRecord, error)
	Delete(ctx context.Context, taskID ID) error
}

// UsageLookup reports the token and cost accounting the usage ledger recorded
// for one chat inside a run's time window. It is optional: a deployment
// without the ledger simply records no cost.
type UsageLookup interface {
	RunUsage(ctx context.Context, chatID servicechat.ID, fromMS, toMS int64) (RunUsage, bool)
}

type ChatLookup interface {
	Get(ctx context.Context, id servicechat.ID) (servicechat.Meta, error)
}

type ProjectAccess interface {
	HasAccess(ctx context.Context, id serviceproject.ID, email string) (bool, error)
}

type IdentityDirectory interface {
	IsRegistered(ctx context.Context, email string) (bool, error)
	IsAdmin(ctx context.Context, email string) (bool, error)
}

// RunResult is delivered exactly once by a RunHandle. Output is inspected for
// the completion marker as a fallback when the executor cannot classify it.
//
// The observer-facing fields below are filled in by the schedule service, not
// by the executor: a gate skip produces a RunResult no executor ever saw.
type RunResult struct {
	Output       string
	TaskComplete bool
	Err          error

	// SkippedByGate marks an occurrence that never started because the task's
	// condition was not met. Reason carries the gate's explanation.
	SkippedByGate bool
	GateReason    string
	// Chain is the position of this run inside a task chain, nil when the run
	// is not part of one.
	Chain *ChainRun
	// Result is the verdict marker the run printed, if any.
	Result string
}

// RunHandle represents an accepted, asynchronously executing agent prompt.
type RunHandle interface {
	Done() <-chan RunResult
}

// Executor must return only after the run has either been accepted or
// rejected. Accepted work completes asynchronously through RunHandle.
type Executor interface {
	StartScheduledPrompt(
		ctx context.Context,
		task Task,
		prompt string,
	) (RunHandle, error)
}

type CronParser interface {
	Next(expression string, after time.Time, location *time.Location) (time.Time, error)
}

// RunObserver receives the outcome of every scheduled run for out-of-band
// reporting such as outbound notifications. Implementations must not block the
// scheduler.
type RunObserver interface {
	ScheduledRunFinished(ctx context.Context, task Task, result RunResult)
}
