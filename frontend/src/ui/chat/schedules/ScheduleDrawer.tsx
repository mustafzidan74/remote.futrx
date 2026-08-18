import type { ComponentChildren } from "preact";
import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import { chatApi } from "../../../api/chatApi";
import type {
  ChainLink,
  CreateScheduledTaskInput,
  ScheduleCondition,
  ScheduledTask,
  UpdateScheduledTaskInput,
} from "../../../models/schedule";
import {
  AlertCircle,
  CalendarClock,
  Clock,
  Edit,
  GitFork,
  Loader,
  Pause,
  Play,
  Plus,
  RotateCcw,
  Trash,
  X,
  Zap,
} from "../../primitives/icons";
import {
  canResumeScheduledTask,
  formatTimestamp,
  isAwaitingArm,
  scheduleDefinition,
  scheduleRunCount,
  sortScheduledTasks,
  toggleActionLabel,
} from "./scheduledTaskView";
import { describeChain, describeCondition } from "./scheduleHistoryView";
import { ScheduleHistoryPanel } from "./ScheduleHistoryPanel";
import { ChainEditor, ConditionEditor, RuleField } from "./ScheduleRuleFields";
import {
  SCHEDULE_TEMPLATES,
  browserTimezone,
  templateToCreateInput,
} from "./scheduleTemplates";

type TaskAction = "toggle" | "run" | "delete" | "save";

export function ScheduleDrawer({
  chatId,
  open,
  onClose,
}: {
  chatId: string;
  open: boolean;
  onClose: () => void;
}) {
  const [tasks, setTasks] = useState<ScheduledTask[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState<{ id: string; action: TaskAction } | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [historyId, setHistoryId] = useState<string | null>(null);
  const [creating, setCreating] = useState<CreateScheduledTaskInput | null>(null);
  const requestSequence = useRef(0);
  const sortedTasks = useMemo(() => sortScheduledTasks(tasks), [tasks]);
  const enabledCount = tasks.filter((task) => task.enabled).length;

  useEffect(() => {
    requestSequence.current += 1;
    setTasks([]);
    setError(null);
    setNotice(null);
    setBusy(null);
    setEditingId(null);
    setHistoryId(null);
    setCreating(null);
  }, [chatId]);

  useEffect(() => {
    if (!open) {
      requestSequence.current += 1;
      setNotice(null);
      return;
    }
    void load();
    return () => {
      requestSequence.current += 1;
    };
  }, [chatId, open]);

  async function load() {
    const sequence = ++requestSequence.current;
    setLoading(true);
    setError(null);
    try {
      const response = await chatApi.fetchSchedules(chatId);
      if (sequence !== requestSequence.current) return;
      setTasks(response);
    } catch (err) {
      if (sequence !== requestSequence.current) return;
      setError(errorMessage(err));
    } finally {
      if (sequence === requestSequence.current) setLoading(false);
    }
  }

  async function toggle(task: ScheduledTask) {
    await perform(task, "toggle", async () => {
      await chatApi.updateSchedule(task.id, { enabled: !task.enabled });
      if (!task.enabled && isAwaitingArm(task)) {
        setNotice(`Armed “${task.name}” — it is now on the clock.`);
      }
    });
  }

  async function createTask(input: CreateScheduledTaskInput) {
    if (busy) return;
    setBusy({ id: "new", action: "save" });
    setError(null);
    setNotice(null);
    try {
      await chatApi.createSchedule(chatId, input);
      setCreating(null);
      setNotice(`Created “${input.name}”.`);
      await load();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  async function saveEdit(task: ScheduledTask, changes: UpdateScheduledTaskInput) {
    await perform(task, "save", async () => {
      await chatApi.updateSchedule(task.id, changes);
      setEditingId(null);
      setNotice(`Updated “${changes.name ?? task.name}”.`);
    });
  }

  async function runNow(task: ScheduledTask) {
    await perform(task, "run", async () => {
      await chatApi.runSchedule(task.id);
      setNotice(`Run requested for “${task.name}”.`);
    });
  }

  async function remove(task: ScheduledTask) {
    if (!confirm(`Delete scheduled task "${task.name}"? Its run history will also be removed.`)) return;
    await perform(task, "delete", async () => {
      await chatApi.deleteSchedule(task.id);
      setNotice(`Deleted “${task.name}”.`);
    });
  }

  async function perform(
    task: ScheduledTask,
    action: TaskAction,
    mutation: () => Promise<void>
  ) {
    if (busy) return;
    setBusy({ id: task.id, action });
    setError(null);
    setNotice(null);
    try {
      await mutation();
      await load();
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(null);
    }
  }

  const subtitle = loading && tasks.length === 0
    ? "Loading…"
    : tasks.length === 0
      ? "No tasks"
      : `${enabledCount} active · ${tasks.length} total`;

  return (
    <aside
      id="workspace-schedules-pane"
      class={`workspace-pane workspace-schedules-pane relative z-20 h-full flex-none overflow-hidden bg-[#101318] border-l border-white/10 shadow-2xl
              transition-[width,opacity] duration-200 ease-out ${open ? "opacity-100" : "opacity-0 border-l-0 pointer-events-none"}`}
      aria-hidden={!open}
      aria-label="Scheduled tasks"
    >
      <div
        class={`h-full min-h-0 w-full flex flex-col transition-transform duration-200 ease-out
                ${open ? "translate-x-0" : "translate-x-full"}`}
      >
        <header class="workspace-pane-header codex-header flex-none bg-[#191a1f] border-b border-white/10 px-3 md:px-4 py-2.5 flex items-center gap-2">
          <div class="h-9 w-9 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none">
            <CalendarClock class="w-4 h-4 text-accent-blue" />
          </div>
          <div class="min-w-0 flex-1">
            <h2 class="truncate text-[15px] md:text-base font-semibold text-ink-50">
              Scheduled tasks
            </h2>
            <div class="truncate text-[12px] text-ink-300">{subtitle}</div>
          </div>
          <button
            type="button"
            onClick={() =>
              setCreating((current) =>
                current
                  ? null
                  : { name: "", prompt: "", kind: "cron", cron: "0 6 * * 1", timezone: browserTimezone(), maxRuns: 0 })}
            disabled={!!busy}
            aria-pressed={!!creating}
            class={`h-9 rounded-md border px-2.5 text-[12px] inline-flex items-center gap-1.5 disabled:opacity-50
                    ${creating
                      ? "border-accent-blue/35 bg-accent-blue/[0.14] text-accent-blue"
                      : "border-white/10 bg-white/5 text-ink-200 hover:bg-white/[0.09]"}`}
            title="Create a scheduled task"
          >
            <Plus class="w-3.5 h-3.5" />
            New
          </button>
          <button
            type="button"
            onClick={() => void load()}
            disabled={loading || !!busy}
            class="h-9 w-9 rounded-md bg-white/5 hover:bg-white/[0.09] border border-white/10 text-ink-200 grid place-items-center disabled:opacity-50"
            title="Refresh scheduled tasks"
            aria-label="Refresh scheduled tasks"
          >
            {loading ? <Loader class="w-4 h-4 animate-spin" /> : <RotateCcw class="w-4 h-4" />}
          </button>
          <button
            type="button"
            onClick={onClose}
            class="h-9 w-9 rounded-md bg-white/5 hover:bg-white/[0.09] border border-white/10 text-ink-200 grid place-items-center"
            title="Close scheduled tasks"
            aria-label="Close scheduled tasks"
            data-workspace-pane-close
          >
            <X class="w-4 h-4" />
          </button>
        </header>

        <div class="flex-1 min-h-0 overflow-y-auto touch-scroll px-3 md:px-4 py-3">
          {error && (
            <div class="mb-3 flex items-start gap-2.5 rounded-md border border-accent-red/30 bg-accent-red/[0.08] px-3 py-2.5 text-[13px]">
              <AlertCircle class="mt-0.5 h-4 w-4 flex-none text-accent-red" />
              <div class="min-w-0 flex-1 break-words text-accent-red">{error}</div>
              <button
                type="button"
                onClick={() => void load()}
                class="flex-none text-[12px] text-ink-200 underline decoration-white/30 underline-offset-2 hover:text-white"
              >
                Retry
              </button>
            </div>
          )}
          {notice && (
            <div class="mb-3 rounded-md border border-accent-green/25 bg-accent-green/[0.08] px-3 py-2 text-[12.5px] text-accent-green">
              {notice}
            </div>
          )}

          {creating && (
            <NewTaskPanel
              draft={creating}
              tasks={tasks}
              saving={busy?.id === "new"}
              onDraft={setCreating}
              onCancel={() => setCreating(null)}
              onSubmit={(input) => void createTask(input)}
            />
          )}

          {loading && tasks.length === 0 ? (
            <div class="h-36 grid place-items-center text-[13px] text-ink-300">
              <div class="flex items-center gap-2">
                <Loader class="w-4 h-4 animate-spin" />
                Loading scheduled tasks…
              </div>
            </div>
          ) : tasks.length === 0 && !error ? (
            <EmptyScheduleState />
          ) : (
            <div class="space-y-2.5">
              {sortedTasks.map((task) => (
                <ScheduledTaskCard
                  key={task.id}
                  task={task}
                  allTasks={tasks}
                  busyAction={busy?.id === task.id ? busy.action : null}
                  actionsDisabled={!!busy}
                  editing={editingId === task.id}
                  historyOpen={historyId === task.id}
                  onToggle={() => void toggle(task)}
                  onRun={() => void runNow(task)}
                  onDelete={() => void remove(task)}
                  onEditToggle={() =>
                    setEditingId((current) => (current === task.id ? null : task.id))}
                  onHistoryToggle={() =>
                    setHistoryId((current) => (current === task.id ? null : task.id))}
                  onSave={(changes) => void saveEdit(task, changes)}
                />
              ))}
            </div>
          )}
        </div>
      </div>
    </aside>
  );
}

function EmptyScheduleState() {
  return (
    <div class="mt-8 rounded-lg border border-dashed border-white/15 bg-white/[0.025] px-5 py-7 text-center">
      <div class="mx-auto mb-3 h-10 w-10 rounded-lg border border-white/10 bg-white/[0.05] grid place-items-center">
        <CalendarClock class="w-5 h-5 text-accent-blue" />
      </div>
      <h3 class="text-[14px] font-medium text-ink-100">No scheduled tasks</h3>
      <p class="mx-auto mt-1.5 max-w-sm text-[12.5px] leading-5 text-ink-400">
        Ask the agent to schedule work in this chat. It can create a one-time reminder or a recurring cron task.
      </p>
      <div class="mx-auto mt-4 max-w-sm rounded-md border border-white/[0.08] bg-black/20 px-3 py-2 text-left text-[12px] leading-5 text-ink-300">
        “Watch the deploy every 5 minutes and stop when it is healthy.”
      </div>
    </div>
  );
}

function ScheduledTaskCard({
  task,
  allTasks,
  busyAction,
  actionsDisabled,
  editing,
  historyOpen,
  onToggle,
  onRun,
  onDelete,
  onEditToggle,
  onHistoryToggle,
  onSave,
}: {
  task: ScheduledTask;
  allTasks: ScheduledTask[];
  busyAction: TaskAction | null;
  actionsDisabled: boolean;
  editing: boolean;
  historyOpen: boolean;
  onToggle: () => void;
  onRun: () => void;
  onDelete: () => void;
  onEditToggle: () => void;
  onHistoryToggle: () => void;
  onSave: (changes: UpdateScheduledTaskInput) => void;
}) {
  const resumeAllowed = canResumeScheduledTask(task);
  const toggleDisabled = actionsDisabled || (!task.enabled && !resumeAllowed);
  const awaitingArm = isAwaitingArm(task);
  const toggleLabel = toggleActionLabel(task);
  const chainSummary = describeChain(task.next, allTasks);
  const conditionSummary = describeCondition(task.condition);

  return (
    <article class={`rounded-lg border px-3 py-3 ${task.enabled ? "border-white/10 bg-white/[0.035]" : "border-white/[0.07] bg-white/[0.018]"}`}>
      <div class="flex items-start gap-2">
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2 min-w-0">
            <h3 class="truncate text-[14px] font-medium text-ink-50">{task.name}</h3>
            <TaskStatus task={task} />
          </div>
          <div class="mt-1 truncate font-mono text-[11.5px] text-accent-blue/90" title={scheduleDefinition(task)}>
            {scheduleDefinition(task)}
          </div>
        </div>
        <span class="flex-none text-[11px] text-ink-400">{scheduleRunCount(task)}</span>
      </div>

      {(chainSummary || conditionSummary) && (
        <div class="mt-1.5 flex flex-wrap gap-1.5">
          {conditionSummary && (
            <span
              class="inline-flex max-w-full items-center gap-1 rounded-full border border-amber-400/25 bg-amber-400/[0.07] px-1.5 py-0.5 text-[10.5px] text-amber-300/90"
              title={conditionSummary}
            >
              <Zap class="w-3 h-3 flex-none" />
              <span class="truncate">{conditionSummary}</span>
            </span>
          )}
          {chainSummary && (
            <span
              class="inline-flex max-w-full items-center gap-1 rounded-full border border-accent-blue/25 bg-accent-blue/[0.07] px-1.5 py-0.5 text-[10.5px] text-accent-blue/90"
              title={chainSummary}
            >
              <GitFork class="w-3 h-3 flex-none" />
              <span class="truncate">{chainSummary}</span>
            </span>
          )}
        </div>
      )}

      <p class="mt-2 line-clamp-3 whitespace-pre-wrap break-words text-[12.5px] leading-[1.55] text-ink-300" title={task.prompt}>
        {task.prompt}
      </p>

      <dl class="mt-3 grid grid-cols-2 gap-x-3 gap-y-2 rounded-md border border-white/[0.06] bg-black/15 px-2.5 py-2">
        <TaskDetail
          label="Next"
          value={task.enabled
            ? formatTimestamp(task.nextRunAt)
            : humanize(task.status || "paused")}
        />
        <TaskDetail label="Last run" value={formatTimestamp(task.lastRunAt)} />
        <TaskDetail label="Last result" value={task.lastRunStatus ? humanize(task.lastRunStatus) : "None"} tone={lastRunTone(task.lastRunStatus)} />
        <TaskDetail label="Verdict" value={task.lastRunResult || "None"} />
      </dl>

      {task.lastError && (
        <div class="mt-2 rounded-md border border-accent-red/20 bg-accent-red/[0.06] px-2.5 py-2 text-[11.5px] leading-4 text-accent-red break-words">
          {task.lastError}
        </div>
      )}

      {awaitingArm && (
        <div class="mt-2 rounded-md border border-amber-400/25 bg-amber-400/[0.07] px-2.5 py-2 text-[11.5px] leading-4 text-amber-300/90">
          Created by the agent and parked. Press Arm to put it on the clock.
        </div>
      )}

      <div class="mt-3 flex items-center gap-2">
        <button
          type="button"
          onClick={onToggle}
          disabled={toggleDisabled}
          class={`h-8 inline-flex items-center gap-1.5 rounded-md border px-2.5 text-[12px] disabled:opacity-45
                  ${awaitingArm
                    ? "border-accent-green/30 bg-accent-green/[0.08] text-accent-green hover:bg-accent-green/[0.14]"
                    : "border-white/10 bg-white/[0.04] text-ink-200 hover:bg-white/[0.08]"}`}
          title={
            task.enabled
              ? "Pause schedule"
              : resumeAllowed
                ? awaitingArm ? "Arm this schedule" : "Resume schedule"
                : `${humanize(task.status || "terminal")} tasks cannot be resumed`
          }
        >
          {busyAction === "toggle"
            ? <Loader class="w-3.5 h-3.5 animate-spin" />
            : task.enabled
              ? <Pause class="w-3.5 h-3.5" />
              : <Play class="w-3.5 h-3.5" />}
          {toggleLabel}
        </button>
        <button
          type="button"
          onClick={onRun}
          disabled={actionsDisabled}
          class="h-8 inline-flex items-center gap-1.5 rounded-md border border-white/10 bg-white/[0.04] px-2.5 text-[12px] text-ink-200 hover:bg-white/[0.08] disabled:opacity-45"
          title="Run this task now"
        >
          {busyAction === "run" ? <Loader class="w-3.5 h-3.5 animate-spin" /> : <Play class="w-3.5 h-3.5" />}
          Run now
        </button>
        <button
          type="button"
          onClick={onHistoryToggle}
          disabled={actionsDisabled}
          aria-pressed={historyOpen}
          class={`h-8 inline-flex items-center gap-1.5 rounded-md border px-2.5 text-[12px] disabled:opacity-45
                  ${historyOpen
                    ? "border-accent-blue/35 bg-accent-blue/[0.14] text-accent-blue"
                    : "border-white/10 bg-white/[0.04] text-ink-200 hover:bg-white/[0.08]"}`}
          title="Show this task's run history"
        >
          <Clock class="w-3.5 h-3.5" />
          History
        </button>
        <button
          type="button"
          onClick={onEditToggle}
          disabled={actionsDisabled}
          aria-pressed={editing}
          class={`h-8 w-8 rounded-md border grid place-items-center disabled:opacity-45
                  ${editing
                    ? "border-accent-blue/35 bg-accent-blue/[0.14] text-accent-blue"
                    : "border-white/10 bg-white/[0.03] text-ink-400 hover:bg-white/[0.08] hover:text-ink-100"}`}
          title={editing ? "Close editor" : "Edit scheduled task"}
          aria-label={`Edit ${task.name}`}
        >
          <Edit class="w-3.5 h-3.5" />
        </button>
        <button
          type="button"
          onClick={onDelete}
          disabled={actionsDisabled}
          class="ml-auto h-8 w-8 rounded-md border border-white/10 bg-white/[0.03] text-ink-400 grid place-items-center hover:bg-accent-red/[0.08] hover:text-accent-red disabled:opacity-45"
          title="Delete scheduled task"
          aria-label={`Delete ${task.name}`}
        >
          {busyAction === "delete" ? <Loader class="w-3.5 h-3.5 animate-spin" /> : <Trash class="w-3.5 h-3.5" />}
        </button>
      </div>

      {historyOpen && <ScheduleHistoryPanel task={task} onClose={onHistoryToggle} />}

      {editing && (
        <EditTaskForm
          task={task}
          allTasks={allTasks}
          saving={busyAction === "save"}
          onCancel={onEditToggle}
          onSave={onSave}
        />
      )}
    </article>
  );
}

// Inline definition editor. Only fields the user changed are sent, so an
// untouched schedule field never resets scheduler state.
function EditTaskForm({
  task,
  allTasks,
  saving,
  onCancel,
  onSave,
}: {
  task: ScheduledTask;
  allTasks: ScheduledTask[];
  saving: boolean;
  onCancel: () => void;
  onSave: (changes: UpdateScheduledTaskInput) => void;
}) {
  const [name, setName] = useState(task.name);
  const [prompt, setPrompt] = useState(task.prompt);
  const [cron, setCron] = useState(task.cron ?? "");
  const [at, setAt] = useState(task.at ? toLocalDateTimeInput(task.at) : "");
  const [timezone, setTimezone] = useState(task.timezone);
  const [maxRuns, setMaxRuns] = useState(task.maxRuns ? String(task.maxRuns) : "");
  const [next, setNext] = useState<ChainLink[]>(task.next ?? []);
  const [condition, setCondition] = useState<ScheduleCondition | undefined>(task.condition);
  const [formError, setFormError] = useState<string | null>(null);

  function submit(event: Event) {
    event.preventDefault();
    const changes: UpdateScheduledTaskInput = {};
    if (name.trim() !== task.name) changes.name = name.trim();
    if (prompt.trim() !== task.prompt) changes.prompt = prompt.trim();
    if (timezone.trim() !== task.timezone) changes.timezone = timezone.trim();
    if (task.kind === "cron" && cron.trim() !== (task.cron ?? "")) {
      changes.cron = cron.trim();
    }
    if (task.kind === "once" && at) {
      const millis = new Date(at).getTime();
      if (Number.isNaN(millis)) {
        setFormError("Enter a valid date and time.");
        return;
      }
      if (millis !== task.at) changes.at = millis;
    }
    const parsedMaxRuns = maxRuns.trim() === "" ? 0 : Number(maxRuns);
    if (!Number.isInteger(parsedMaxRuns) || parsedMaxRuns < 0) {
      setFormError("Max runs must be a non-negative whole number.");
      return;
    }
    if (parsedMaxRuns !== (task.maxRuns ?? 0)) changes.maxRuns = parsedMaxRuns;

    // Chain and condition are replace-whole-value edits, so they are only sent
    // when they actually differ — an untouched gate is never re-validated.
    const cleanedNext = next.filter((link) => link.taskId);
    if (JSON.stringify(cleanedNext) !== JSON.stringify(task.next ?? [])) {
      changes.next = cleanedNext;
    }
    if (JSON.stringify(condition ?? null) !== JSON.stringify(task.condition ?? null)) {
      // A cleared gate is sent as an empty kind, which the backend reads as
      // "remove the condition".
      changes.condition = condition ?? ({ kind: "" } as unknown as ScheduleCondition);
    }

    if (Object.keys(changes).length === 0) {
      onCancel();
      return;
    }
    setFormError(null);
    onSave(changes);
  }

  return (
    <form onSubmit={submit} class="mt-3 space-y-2 rounded-md border border-white/[0.08] bg-black/20 p-2.5">
      <EditField label="Name">
        <input
          type="text"
          value={name}
          onInput={(event) => setName((event.currentTarget as HTMLInputElement).value)}
          class="h-8 w-full rounded-md border border-white/10 bg-[#0b0d11] px-2 text-[12.5px] text-ink-100 focus:outline-none focus:border-accent-blue/60"
        />
      </EditField>
      <EditField label="Prompt">
        <textarea
          value={prompt}
          onInput={(event) => setPrompt((event.currentTarget as HTMLTextAreaElement).value)}
          rows={4}
          class="w-full rounded-md border border-white/10 bg-[#0b0d11] px-2 py-1.5 text-[12.5px] leading-5 text-ink-100 focus:outline-none focus:border-accent-blue/60"
        />
      </EditField>
      {task.kind === "cron" ? (
        <EditField label="Cron (five fields)">
          <input
            type="text"
            value={cron}
            onInput={(event) => setCron((event.currentTarget as HTMLInputElement).value)}
            placeholder="*/10 * * * *"
            class="h-8 w-full rounded-md border border-white/10 bg-[#0b0d11] px-2 font-mono text-[12px] text-ink-100 focus:outline-none focus:border-accent-blue/60"
          />
        </EditField>
      ) : (
        <EditField label="Run at">
          <input
            type="datetime-local"
            value={at}
            onInput={(event) => setAt((event.currentTarget as HTMLInputElement).value)}
            class="h-8 w-full rounded-md border border-white/10 bg-[#0b0d11] px-2 text-[12px] text-ink-100 focus:outline-none focus:border-accent-blue/60"
          />
        </EditField>
      )}
      <div class="grid grid-cols-2 gap-2">
        <EditField label="Timezone">
          <input
            type="text"
            value={timezone}
            onInput={(event) => setTimezone((event.currentTarget as HTMLInputElement).value)}
            placeholder="UTC"
            class="h-8 w-full rounded-md border border-white/10 bg-[#0b0d11] px-2 text-[12px] text-ink-100 focus:outline-none focus:border-accent-blue/60"
          />
        </EditField>
        <EditField label="Max runs (0 = unlimited)">
          <input
            type="text"
            inputMode="numeric"
            value={maxRuns}
            onInput={(event) => setMaxRuns((event.currentTarget as HTMLInputElement).value)}
            placeholder="0"
            class="h-8 w-full rounded-md border border-white/10 bg-[#0b0d11] px-2 text-[12px] text-ink-100 focus:outline-none focus:border-accent-blue/60"
          />
        </EditField>
      </div>
      <ConditionEditor
        condition={condition}
        tasks={allTasks}
        editingId={task.id}
        onChange={setCondition}
      />
      <ChainEditor
        links={next}
        tasks={allTasks}
        editingId={task.id}
        onChange={setNext}
      />
      {formError && (
        <div class="text-[11.5px] text-accent-red">{formError}</div>
      )}
      <div class="flex items-center justify-end gap-2 pt-0.5">
        <button
          type="button"
          onClick={onCancel}
          disabled={saving}
          class="h-8 rounded-md border border-white/10 bg-white/[0.03] px-2.5 text-[12px] text-ink-300 hover:bg-white/[0.07] disabled:opacity-45"
        >
          Cancel
        </button>
        <button
          type="submit"
          disabled={saving}
          class="h-8 inline-flex items-center gap-1.5 rounded-md border border-accent-blue/35 bg-accent-blue/[0.14] px-3 text-[12px] font-medium text-accent-blue hover:bg-accent-blue/[0.2] disabled:opacity-45"
        >
          {saving && <Loader class="w-3.5 h-3.5 animate-spin" />}
          Save changes
        </button>
      </div>
    </form>
  );
}

function EditField({ label, children }: { label: string; children: ComponentChildren }) {
  return (
    <label class="block">
      <span class="mb-1 block text-[10px] uppercase tracking-wide text-ink-300">{label}</span>
      {children}
    </label>
  );
}

// datetime-local inputs want local wall-clock time without a zone suffix.
function toLocalDateTimeInput(timestamp: number): string {
  const date = new Date(timestamp);
  const pad = (value: number) => String(value).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

function TaskStatus({ task }: { task: ScheduledTask }) {
  const label = !task.enabled && task.status === "scheduled" ? "paused" : task.status || (task.enabled ? "scheduled" : "paused");
  const classes = task.enabled
    ? "border-accent-green/25 bg-accent-green/[0.08] text-accent-green"
    : "border-white/10 bg-white/[0.04] text-ink-400";
  return (
    <span class={`flex-none rounded-full border px-1.5 py-0.5 text-[9.5px] font-medium uppercase tracking-wide ${classes}`}>
      {humanize(label)}
    </span>
  );
}

function TaskDetail({
  label,
  value,
  tone = "text-ink-200",
}: {
  label: string;
  value: string;
  tone?: string;
}) {
  return (
    <div class="min-w-0">
      <dt class="text-[10px] uppercase tracking-wide text-ink-300">{label}</dt>
      <dd class={`mt-0.5 truncate text-[11.5px] ${tone}`} title={value}>{value}</dd>
    </div>
  );
}

function lastRunTone(status?: string): string {
  const normalized = (status || "").toLowerCase();
  if (["failed", "error"].includes(normalized)) return "text-accent-red";
  if (["success", "succeeded", "complete", "completed"].includes(normalized)) return "text-accent-green";
  return "text-ink-200";
}

function humanize(value: string): string {
  return value.replaceAll("_", " ");
}

function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

// NewTaskPanel is the "New task from template" flow: pick a ready-made job,
// then adjust its name, prompt, cron, condition and chain before saving. The
// draft lives in the drawer so switching templates never loses an edit that
// was already made on purpose.
function NewTaskPanel({
  draft,
  tasks,
  saving,
  onDraft,
  onCancel,
  onSubmit,
}: {
  draft: CreateScheduledTaskInput;
  tasks: ScheduledTask[];
  saving: boolean;
  onDraft: (draft: CreateScheduledTaskInput) => void;
  onCancel: () => void;
  onSubmit: (input: CreateScheduledTaskInput) => void;
}) {
  const [formError, setFormError] = useState<string | null>(null);

  function patch(changes: Partial<CreateScheduledTaskInput>) {
    onDraft({ ...draft, ...changes });
  }

  function submit(event: Event) {
    event.preventDefault();
    if (!draft.name.trim()) {
      setFormError("Give the task a name.");
      return;
    }
    if (!draft.prompt.trim()) {
      setFormError("Give the task a prompt — it is what the agent will run.");
      return;
    }
    if (!(draft.cron ?? "").trim()) {
      setFormError("Enter a five-field cron expression.");
      return;
    }
    setFormError(null);
    onSubmit({
      ...draft,
      name: draft.name.trim(),
      prompt: draft.prompt.trim(),
      cron: (draft.cron ?? "").trim(),
      timezone: (draft.timezone || "UTC").trim(),
      next: (draft.next ?? []).filter((link) => link.taskId),
    });
  }

  return (
    <form
      onSubmit={submit}
      class="mb-3 space-y-2 rounded-lg border border-accent-blue/25 bg-accent-blue/[0.04] p-3"
    >
      <div>
        <span class="mb-1.5 block text-[10px] uppercase tracking-wide text-ink-300">
          Start from a template
        </span>
        <div class="grid grid-cols-1 gap-1.5 sm:grid-cols-2">
          {SCHEDULE_TEMPLATES.map((template) => (
            <button
              key={template.id}
              type="button"
              onClick={() => {
                onDraft(templateToCreateInput(template, draft.timezone || browserTimezone()));
                setFormError(null);
              }}
              class="rounded-md border border-white/10 bg-black/25 px-2.5 py-2 text-left hover:border-accent-blue/35 hover:bg-accent-blue/[0.08]"
            >
              <span class="block text-[12px] font-medium text-ink-100">{template.name}</span>
              <span class="mt-0.5 block text-[10.5px] leading-4 text-ink-400">
                {template.blurb}
              </span>
              <span class="mt-1 block font-mono text-[10px] text-accent-blue/80">
                {template.cron} · skills: {template.skills.join(", ")}
              </span>
            </button>
          ))}
        </div>
      </div>

      <RuleField label="Name">
        <input
          type="text"
          value={draft.name}
          onInput={(event) => patch({ name: (event.currentTarget as HTMLInputElement).value })}
          class="h-8 w-full rounded-md border border-white/10 bg-[#0b0d11] px-2 text-[12.5px] text-ink-100 focus:outline-none focus:border-accent-blue/60"
        />
      </RuleField>
      <RuleField label="Prompt">
        <textarea
          value={draft.prompt}
          onInput={(event) =>
            patch({ prompt: (event.currentTarget as HTMLTextAreaElement).value })}
          rows={6}
          class="w-full rounded-md border border-white/10 bg-[#0b0d11] px-2 py-1.5 text-[12.5px] leading-5 text-ink-100 focus:outline-none focus:border-accent-blue/60"
        />
      </RuleField>
      <div class="grid grid-cols-2 gap-2">
        <RuleField label="Cron (five fields)">
          <input
            type="text"
            value={draft.cron ?? ""}
            onInput={(event) => patch({ cron: (event.currentTarget as HTMLInputElement).value })}
            placeholder="0 6 * * 1"
            class="h-8 w-full rounded-md border border-white/10 bg-[#0b0d11] px-2 font-mono text-[12px] text-ink-100 focus:outline-none focus:border-accent-blue/60"
          />
        </RuleField>
        <RuleField label="Timezone">
          <input
            type="text"
            value={draft.timezone}
            onInput={(event) =>
              patch({ timezone: (event.currentTarget as HTMLInputElement).value })}
            placeholder="UTC"
            class="h-8 w-full rounded-md border border-white/10 bg-[#0b0d11] px-2 text-[12px] text-ink-100 focus:outline-none focus:border-accent-blue/60"
          />
        </RuleField>
      </div>

      <ConditionEditor
        condition={draft.condition}
        tasks={tasks}
        editingId=""
        onChange={(condition) => patch({ condition })}
      />
      <ChainEditor
        links={draft.next ?? []}
        tasks={tasks}
        editingId=""
        onChange={(next) => patch({ next })}
      />

      {formError && <div class="text-[11.5px] text-accent-red">{formError}</div>}
      <div class="flex items-center justify-end gap-2 pt-0.5">
        <button
          type="button"
          onClick={onCancel}
          disabled={saving}
          class="h-8 rounded-md border border-white/10 bg-white/[0.03] px-2.5 text-[12px] text-ink-300 hover:bg-white/[0.07] disabled:opacity-45"
        >
          Cancel
        </button>
        <button
          type="submit"
          disabled={saving}
          class="h-8 inline-flex items-center gap-1.5 rounded-md border border-accent-blue/35 bg-accent-blue/[0.14] px-3 text-[12px] font-medium text-accent-blue hover:bg-accent-blue/[0.2] disabled:opacity-45"
        >
          {saving && <Loader class="w-3.5 h-3.5 animate-spin" />}
          Create task
        </button>
      </div>
    </form>
  );
}
