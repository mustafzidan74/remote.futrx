---
name: scheduled-tasks
description: "Create and manage one-time or recurring Remote tasks in the current chat. Use when the user explicitly asks to schedule, repeat, defer, monitor, or resume agent work later. Includes bounded monitoring tasks that stop themselves when their standing goal is complete."
---

# Scheduled tasks

Remote owns the scheduler. Use `/workspace/scripts/remote-schedule`; do not
create container cron jobs, background loops, or systemd timers. A scheduled
fire returns to this chat and starts a normal agent turn with the stored
prompt.

Only create a schedule when the user explicitly asks for future or recurring
execution. Do not infer a schedule from requests such as "keep working."
Before creating it, resolve the intended time, recurrence, timezone, goal, and
stopping condition from the user's request and the current context.

## Write a durable prompt

Every stored prompt must be self-contained even though scheduled runs retain
this chat's context. Include:

- the standing goal and relevant project, deployment, issue, or resource IDs;
- what to inspect or do on each fire;
- an observable completion condition;
- what to report when work remains;
- important safety constraints and any maximum-run behavior.

Do not store vague prompts such as `resume`, `continue`, or "check the thing."
A useful monitoring prompt looks like:

```text
[Scheduled task: watch deploy]

Goal: Monitor deployment dep_123 until its status is terminal (Ready or Failed).
On each run, inspect deployment dep_123 and report its current status.
If it is Ready, call `remote-schedule complete-current`, report the final
result, and do no further deployment work. If it is Failed, report the error
and call `remote-schedule complete-current` to stop future checks. Otherwise
report what remains.
```

## Create

Use RFC3339, including a `Z` or numeric UTC offset, for a one-time task:

```sh
/workspace/scripts/remote-schedule create \
  --name "review build" \
  --prompt "Inspect build build_123. Report its result and relevant failures." \
  --at "2026-07-24T14:00:00-04:00" \
  --timezone "America/Toronto"
```

Recurring schedules use standard **five-field** cron:

```text
minute hour day-of-month month day-of-week
```

There is no seconds field. Always pass an explicit IANA timezone such as
`America/Toronto`, `Europe/London`, or `UTC`; never rely on the container's
local timezone.

```sh
/workspace/scripts/remote-schedule create \
  --name "watch deploy" \
  --prompt "[Scheduled task: watch deploy] Goal: monitor deployment dep_123 until Ready. Inspect it, report status, and call remote-schedule complete-current only after it is Ready." \
  --cron "*/10 * * * *" \
  --timezone "America/Toronto" \
  --max-runs 12
```

Use `--max-runs` for bounded monitoring or any recurrence where unlimited
runs are not clearly intended.

**Schedules you create start disabled.** The backend parks every
agent-created schedule until the user arms it from the Schedules drawer in
the chat header. After creating one, read the returned JSON, then tell the
user its name, timing, timezone, and ID, and ask them to open Schedules and
press Resume to arm it. Do not treat the task as running until they do.

The backend also enforces a minimum recurrence interval; if `create` is
rejected for firing too often, widen the cron step instead of retrying.

## Report a verdict

End a scheduled run with a **verdict marker** whenever the task feeds a chain
or a condition. The marker must be the **last non-empty line** of your reply
and nothing else may share that line:

```text
<<RESULT: DRIFT>>
```

The backend stores the text between the delimiters as the task's `lastRunResult`
and shows it in the run History drawer. Other tasks match it with an
`outputContains` condition, and a chained task can be armed only when this
run's verdict says so, so keep verdicts short, stable, and machine-readable —
`OK`, `DRIFT`, `FAILED`, `SCORE=94`. A run that omits the marker simply has no
verdict; never invent one.

A run may print both markers; put the verdict line first and
`SCHEDULE_STATUS=COMPLETE` last, and the backend reads both.

## Manage

```sh
/workspace/scripts/remote-schedule list
/workspace/scripts/remote-schedule pause SCHEDULE_ID
/workspace/scripts/remote-schedule run-now SCHEDULE_ID
/workspace/scripts/remote-schedule delete SCHEDULE_ID
```

Pause is the default way to stop a schedule because it preserves its history.
Delete only when the user explicitly asks to remove it. Enabling a schedule
(arming or resuming) is reserved for the user in the Schedules drawer — the
agent API cannot enable tasks, so never promise to resume one yourself.

## Complete the current standing task

During a scheduled run, once the standing goal itself—not merely the current
check—is truly complete, run:

```sh
/workspace/scripts/remote-schedule complete-current
```

This operation is scoped to the schedule that launched the current turn and
disables future fires while retaining history. Never call it from an ordinary
interactive turn, never call it only because one run finished successfully,
and never guess that a goal is complete. If completion is ambiguous, report
the evidence and leave the schedule active.
