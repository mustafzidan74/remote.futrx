package schedule

import (
	"context"
	"log"
	"strings"
	"time"
)

// snapshotTimeout bounds one before/after git capture. The scheduler loop
// waits on the "before" half, so it stays well under the gate budget.
const snapshotTimeout = 10 * time.Second

// runTrace is what the dispatch path knows about a run before it starts: when
// it started, which claim it belongs to, and what the workspace looked like.
// It travels into finish so history can diff the two captures.
type runTrace struct {
	RunID     string
	StartedAt int64
	Forced    bool
	Chain     *ChainRun
	Before    GitSnapshot
}

func (s *Service) beginTrace(ctx context.Context, task Task) runTrace {
	trace := runTrace{
		RunID:     task.ActiveRunID,
		StartedAt: task.ActiveRunStarted,
		Forced:    task.ActiveRunForced,
		Chain:     task.ActiveChain,
	}
	if trace.StartedAt == 0 {
		trace.StartedAt = s.now().UnixMilli()
	}
	trace.Before = s.gitSnapshot(ctx, task)
	return trace
}

// gitSnapshot captures the workspace state, best effort. A deployment with no
// container runtime, a stopped container, or a workspace that is not a git
// checkout all produce an empty snapshot rather than an error.
func (s *Service) gitSnapshot(ctx context.Context, task Task) GitSnapshot {
	if s.workspace == nil || task.ProjectID == "" {
		return GitSnapshot{}
	}
	ctx, cancel := context.WithTimeout(ctx, snapshotTimeout)
	defer cancel()
	snapshot, err := s.workspace.GitSnapshot(ctx, task.ProjectID)
	if err != nil {
		return GitSnapshot{}
	}
	return snapshot
}

// settleReporting records the run in history and starts any chained tasks. It
// returns the chain position of the run that just settled so the notification
// can read "chain 2/3".
func (s *Service) settleReporting(
	ctx context.Context,
	claimed Task,
	settled Task,
	result RunResult,
	trace runTrace,
	nowMS int64,
) *ChainRun {
	catalog, err := s.repo.List(ctx)
	if err != nil {
		log.Printf("schedules: read catalog after %s: %v", settled.ID, err)
		catalog = nil
	}
	// The settled snapshot has already cleared ActiveChain, so the position
	// is read from the claim the run actually held.
	positioned := settled
	positioned.ActiveChain = trace.Chain
	chain := chainContext(positioned, catalog)

	after := s.gitSnapshot(ctx, settled)
	record := RunRecord{
		RunID:      trace.RunID,
		TaskID:     settled.ID,
		ChatID:     settled.ChatID,
		StartedAt:  trace.StartedAt,
		FinishedAt: nowMS,
		Status:     historyStatus(result),
		Summary:    summarize(result.Output),
		Result:     result.Result,
		Chain:      chain,
		Forced:     trace.Forced,
	}
	if result.Err != nil {
		record.Error = result.Err.Error()
	}
	if after.Repository {
		record.FilesChanged = changedFiles(trace.Before, after)
		record.DiffStat = strings.TrimSpace(after.DiffStat)
		if after.Head != "" && after.Head != trace.Before.Head {
			record.CommitSHA = after.Head
		}
	}
	if s.usage != nil {
		if usage, ok := s.usage.RunUsage(ctx, settled.ChatID, trace.StartedAt, nowMS); ok {
			record.Tokens = usage.Tokens
			record.CostUSD = usage.CostUSD
		}
	}

	record.ChainTriggered = s.triggerChain(ctx, settled, catalog, chain, result)
	s.appendHistory(ctx, record)
	if len(record.ChainTriggered) > 0 {
		s.notify()
	}
	return chain
}

func historyStatus(result RunResult) HistoryStatus {
	switch {
	case result.SkippedByGate:
		return HistorySkipped
	case result.Err != nil:
		if isCancellation(result.Err) {
			return HistoryCancelled
		}
		return HistoryFailed
	default:
		return HistoryOK
	}
}

func isCancellation(err error) bool {
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "cancel") || strings.Contains(message, "context canceled")
}

func (s *Service) appendHistory(ctx context.Context, record RunRecord) {
	if s.history == nil || record.TaskID == "" {
		return
	}
	if err := s.history.Append(ctx, record.TaskID, record); err != nil {
		log.Printf("schedules: append history %s: %v", record.TaskID, err)
	}
}

// triggerChain arms every "then run…" edge whose condition the settled outcome
// satisfies. Targets are queued as one-off runs: they respect their own
// overlap policy, their own enabled state, and the chain depth bound.
//
// A disabled target is never triggered. That is deliberate — an agent-created
// task parked awaiting a user's arm must not be reachable through a chain, or
// the arm handshake would be trivially bypassable.
func (s *Service) triggerChain(
	ctx context.Context,
	settled Task,
	catalog []Task,
	chain *ChainRun,
	result RunResult,
) []ID {
	if len(settled.Next) == 0 {
		return nil
	}
	depth := 1
	if chain != nil && chain.Depth > 0 {
		depth = chain.Depth
	}
	if depth >= MaxChainDepth {
		log.Printf(
			"schedules: chain from %s stopped at the depth limit of %d",
			settled.ID, MaxChainDepth,
		)
		return nil
	}
	total := depth + 1
	if chain != nil && chain.Total > total {
		total = chain.Total
	}

	failed := result.Err != nil || result.SkippedByGate
	nowMS := s.now().UnixMilli()
	triggered := make([]ID, 0, len(settled.Next))
	for _, link := range chainTargets(settled, failed) {
		next := ChainRun{FromTaskID: settled.ID, Depth: depth + 1, Total: total}
		started, err := s.armChainTarget(ctx, link, next, nowMS)
		if err != nil {
			log.Printf("schedules: chain %s → %s: %v", settled.ID, link.TaskID, err)
			continue
		}
		if started {
			triggered = append(triggered, link.TaskID)
		}
	}
	if len(triggered) == 0 {
		return nil
	}
	return triggered
}

func (s *Service) armChainTarget(
	ctx context.Context,
	link ChainLink,
	position ChainRun,
	nowMS int64,
) (bool, error) {
	armed := false
	_, err := s.repo.Update(ctx, link.TaskID, func(task *Task) error {
		armed = false
		if !task.Enabled {
			return nil
		}
		switch task.Status {
		case StatusCompleted, StatusExhausted, StatusError:
			return nil
		}
		if task.MaxRuns > 0 && task.RunCount >= task.MaxRuns {
			return nil
		}
		if task.ActiveRunID != "" && task.Overlap == OverlapSkip {
			// A skip-policy target that is already busy drops the trigger,
			// exactly as it drops an overlapping cron occurrence.
			return nil
		}
		task.PendingRun = true
		if task.PendingSince == 0 {
			task.PendingSince = nowMS
		}
		task.PendingChain = &position
		if link.DelayMin > 0 {
			task.RetryAt = nowMS + int64(link.DelayMin)*60_000
		} else {
			task.RetryAt = 0
		}
		if task.ActiveRunID == "" {
			task.LastStatus = RunStatusQueued
		}
		task.UpdatedAt = nowMS
		armed = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return armed, nil
}

// skipForGate consumes one occurrence without starting an agent. The task's
// deadline advances exactly as it would after a real run, RunCount is left
// alone (a gate skip must not burn the maxRuns budget), and the skip is
// reported to history and to the notification observer.
func (s *Service) skipForGate(
	ctx context.Context,
	snapshot Task,
	now time.Time,
	outcome GateOutcome,
) error {
	nowMS := now.UnixMilli()
	reason := strings.TrimSpace(outcome.Reason)
	if reason == "" {
		reason = "condition not met"
	}
	skipped := false
	updated, err := s.repo.Update(ctx, snapshot.ID, func(task *Task) error {
		// Re-check against the freshly read task: the occurrence may have been
		// claimed, paused, or re-armed between the list and this write.
		if !isDue(*task, nowMS) || task.ActiveRunID != "" {
			return nil
		}
		skipped = true
		task.PendingRun = false
		task.PendingRunForced = false
		task.PendingSince = 0
		task.PendingChain = nil
		task.RetryAt = 0
		// LastRunAt and LastRunEnd are deliberately left alone: nothing ran.
		// Stamping them here would also make a notIfRanWithin gate consider
		// its own skip a recent run and lock the task shut forever.
		task.LastStatus = RunStatusSkipped
		task.LastError = "gate: " + reason
		task.UpdatedAt = nowMS
		if task.Kind == KindOnce {
			// A once task has exactly one occurrence; a closed gate consumes
			// it rather than leaving the task due forever.
			task.Enabled = false
			task.Status = StatusExhausted
			task.NextRunAt = 0
			return nil
		}
		next, nextErr := s.nextAfterClaim(*task, now)
		if nextErr != nil {
			return nextErr
		}
		task.NextRunAt = next
		if task.Enabled {
			task.Status = StatusActive
		}
		return nil
	})
	if err != nil {
		return err
	}
	if !skipped {
		return nil
	}
	s.notify()

	result := RunResult{SkippedByGate: true, GateReason: reason}
	catalog, listErr := s.repo.List(ctx)
	if listErr != nil {
		catalog = nil
	}
	positioned := updated
	positioned.ActiveChain = snapshot.PendingChain
	chain := chainContext(positioned, catalog)
	result.Chain = chain

	s.appendHistory(ctx, RunRecord{
		RunID:         newRunID(),
		TaskID:        updated.ID,
		ChatID:        updated.ChatID,
		StartedAt:     nowMS,
		FinishedAt:    nowMS,
		Status:        HistorySkipped,
		SkippedByGate: true,
		GateReason:    reason,
		Chain:         chain,
	})
	if s.observer != nil {
		s.observer.ScheduledRunFinished(ctx, updated, result)
	}
	return nil
}

// validateGraph checks the parts of a definition that depend on the rest of
// the catalog: chain targets must exist, share the project, and never lead
// back to the task being saved.
func (s *Service) validateGraph(ctx context.Context, task Task) error {
	if len(task.Next) == 0 {
		return nil
	}
	catalog, err := s.repo.List(ctx)
	if err != nil {
		return err
	}
	return validateChain(task, catalog)
}
