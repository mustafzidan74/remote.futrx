import type { ComponentChildren } from "preact";
import type {
  ChainLink,
  ChainWhen,
  ScheduledTask,
  ScheduleCondition,
  ScheduleConditionKind,
} from "../../../models/schedule";
import { Plus, Trash } from "../../primitives/icons";
import { chainCandidates, weekdayName } from "./scheduleHistoryView";

const CHAIN_WHEN: ChainWhen[] = ["success", "failure", "always"];

const CONDITION_KINDS: { value: ScheduleConditionKind; label: string }[] = [
  { value: "outputContains", label: "Last run output matches…" },
  { value: "httpStatus", label: "URL answers a status…" },
  { value: "commandExitCode", label: "Command exits with…" },
  { value: "weekdays", label: "Only on weekdays…" },
  { value: "notIfRanWithin", label: "Not if it ran within…" },
];

export function RuleField({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: ComponentChildren;
}) {
  return (
    <label class="block">
      <span class="mb-1 block text-[10px] uppercase tracking-wide text-ink-300">{label}</span>
      {children}
      {hint && <span class="mt-1 block text-[10.5px] leading-4 text-ink-400">{hint}</span>}
    </label>
  );
}

const inputClass =
  "h-8 w-full rounded-md border border-white/10 bg-[#0b0d11] px-2 text-[12px] text-ink-100 focus:outline-none focus:border-accent-blue/60";

// ChainEditor is the "Then run…" picker: which other task in this chat runs
// after this one settles, on which outcome, and after how long.
export function ChainEditor({
  links,
  tasks,
  editingId,
  onChange,
}: {
  links: ChainLink[];
  tasks: ScheduledTask[];
  editingId: string;
  onChange: (links: ChainLink[]) => void;
}) {
  const candidates = chainCandidates(tasks, editingId);

  function update(index: number, patch: Partial<ChainLink>) {
    onChange(links.map((link, position) =>
      position === index ? { ...link, ...patch } : link));
  }

  return (
    <div class="space-y-1.5">
      <span class="block text-[10px] uppercase tracking-wide text-ink-300">Then run…</span>
      {candidates.length === 0 ? (
        <p class="text-[10.5px] leading-4 text-ink-400">
          Chains link two tasks in this chat. Create a second task first.
        </p>
      ) : (
        <>
          {links.map((link, index) => (
            <div key={index} class="flex flex-wrap items-center gap-1.5">
              <select
                value={link.taskId}
                onChange={(event) =>
                  update(index, { taskId: (event.currentTarget as HTMLSelectElement).value })}
                class={`${inputClass} min-w-[8rem] flex-1`}
                aria-label="Chained task"
              >
                <option value="">Select a task…</option>
                {candidates.map((candidate) => (
                  <option key={candidate.id} value={candidate.id}>
                    {candidate.name}
                  </option>
                ))}
              </select>
              <select
                value={link.when}
                onChange={(event) =>
                  update(index, {
                    when: (event.currentTarget as HTMLSelectElement).value as ChainWhen,
                  })}
                class={`${inputClass} w-[6.5rem] flex-none`}
                aria-label="Chain condition"
              >
                {CHAIN_WHEN.map((when) => (
                  <option key={when} value={when}>{when}</option>
                ))}
              </select>
              <input
                type="text"
                inputMode="numeric"
                value={link.delayMin ? String(link.delayMin) : ""}
                placeholder="+min"
                onInput={(event) =>
                  update(index, {
                    delayMin: Number((event.currentTarget as HTMLInputElement).value) || 0,
                  })}
                class={`${inputClass} w-[4.5rem] flex-none`}
                aria-label="Chain delay in minutes"
              />
              <button
                type="button"
                onClick={() => onChange(links.filter((_, position) => position !== index))}
                class="h-8 w-8 flex-none rounded-md border border-white/10 bg-white/[0.03] text-ink-400 grid place-items-center hover:bg-accent-red/[0.08] hover:text-accent-red"
                title="Remove this chain link"
                aria-label="Remove chain link"
              >
                <Trash class="w-3.5 h-3.5" />
              </button>
            </div>
          ))}
          <button
            type="button"
            onClick={() =>
              onChange([...links, { taskId: candidates[0].id, when: "success" }])}
            class="inline-flex h-7 items-center gap-1 rounded-md border border-white/10 bg-white/[0.03] px-2 text-[11.5px] text-ink-300 hover:bg-white/[0.08]"
          >
            <Plus class="w-3 h-3" /> Add a follow-up
          </button>
          <p class="text-[10.5px] leading-4 text-ink-400">
            A follow-up fires as a one-off run and is skipped while its own task
            is paused. Chains stop after 10 links.
          </p>
        </>
      )}
    </div>
  );
}

// ConditionEditor edits the single optional gate. A closed gate records a
// skipped occurrence and starts no agent turn, so the copy stays explicit
// about what "no" costs.
export function ConditionEditor({
  condition,
  tasks,
  editingId,
  onChange,
}: {
  condition: ScheduleCondition | undefined;
  tasks: ScheduledTask[];
  editingId: string;
  onChange: (condition: ScheduleCondition | undefined) => void;
}) {
  const kind = condition?.kind;

  function setKind(next: string) {
    if (!next) {
      onChange(undefined);
      return;
    }
    onChange(defaultCondition(next as ScheduleConditionKind));
  }

  function patch(changes: Partial<ScheduleCondition>) {
    if (!condition) return;
    onChange({ ...condition, ...changes });
  }

  return (
    <div class="space-y-1.5">
      <RuleField
        label="Run only if…"
        hint="Checked before every scheduled fire. A closed condition records a skipped run and spends no tokens. Run now ignores it."
      >
        <select
          value={kind ?? ""}
          onChange={(event) => setKind((event.currentTarget as HTMLSelectElement).value)}
          class={inputClass}
        >
          <option value="">No condition — always run</option>
          {CONDITION_KINDS.map((entry) => (
            <option key={entry.value} value={entry.value}>{entry.label}</option>
          ))}
        </select>
      </RuleField>

      {kind === "outputContains" && (
        <div class="grid grid-cols-2 gap-2">
          <RuleField label="Pattern (regex)">
            <input
              type="text"
              value={condition?.pattern ?? ""}
              placeholder="DRIFT|FAILED"
              onInput={(event) =>
                patch({ pattern: (event.currentTarget as HTMLInputElement).value })}
              class={`${inputClass} font-mono`}
            />
          </RuleField>
          <RuleField label="In the last run of">
            <select
              value={condition?.inLastRunOf ?? "self"}
              onChange={(event) =>
                patch({ inLastRunOf: (event.currentTarget as HTMLSelectElement).value })}
              class={inputClass}
            >
              <option value="self">This task</option>
              {chainCandidates(tasks, editingId).map((candidate) => (
                <option key={candidate.id} value={candidate.id}>{candidate.name}</option>
              ))}
            </select>
          </RuleField>
        </div>
      )}

      {kind === "httpStatus" && (
        <div class="grid grid-cols-[1fr_5rem] gap-2">
          <RuleField label="URL">
            <input
              type="text"
              value={condition?.url ?? ""}
              placeholder="https://example.com/health"
              onInput={(event) =>
                patch({ url: (event.currentTarget as HTMLInputElement).value })}
              class={inputClass}
            />
          </RuleField>
          <RuleField label="Expect">
            <input
              type="text"
              inputMode="numeric"
              value={String(condition?.expect ?? 200)}
              onInput={(event) =>
                patch({ expect: Number((event.currentTarget as HTMLInputElement).value) || 0 })}
              class={inputClass}
            />
          </RuleField>
        </div>
      )}

      {kind === "commandExitCode" && (
        <div class="grid grid-cols-[1fr_5rem] gap-2">
          <RuleField label="Command (runs in /workspace)">
            <input
              type="text"
              value={condition?.command ?? ""}
              placeholder="git diff --quiet"
              onInput={(event) =>
                patch({ command: (event.currentTarget as HTMLInputElement).value })}
              class={`${inputClass} font-mono`}
            />
          </RuleField>
          <RuleField label="Exit code">
            <input
              type="text"
              inputMode="numeric"
              value={String(condition?.expect ?? 0)}
              onInput={(event) =>
                patch({ expect: Number((event.currentTarget as HTMLInputElement).value) || 0 })}
              class={inputClass}
            />
          </RuleField>
        </div>
      )}

      {kind === "weekdays" && (
        <div class="flex flex-wrap gap-1">
          {[0, 1, 2, 3, 4, 5, 6].map((day) => {
            const selected = (condition?.weekdays ?? []).includes(day);
            return (
              <button
                key={day}
                type="button"
                aria-pressed={selected}
                onClick={() =>
                  patch({
                    weekdays: selected
                      ? (condition?.weekdays ?? []).filter((value) => value !== day)
                      : [...(condition?.weekdays ?? []), day].sort((a, b) => a - b),
                  })}
                class={`h-7 rounded-md border px-2 text-[11.5px]
                        ${selected
                          ? "border-accent-blue/35 bg-accent-blue/[0.14] text-accent-blue"
                          : "border-white/10 bg-white/[0.03] text-ink-300 hover:bg-white/[0.08]"}`}
              >
                {weekdayName(day)}
              </button>
            );
          })}
        </div>
      )}

      {kind === "notIfRanWithin" && (
        <RuleField label="Minutes">
          <input
            type="text"
            inputMode="numeric"
            value={String(condition?.minutes ?? 0)}
            onInput={(event) =>
              patch({ minutes: Number((event.currentTarget as HTMLInputElement).value) || 0 })}
            class={inputClass}
          />
        </RuleField>
      )}
    </div>
  );
}

function defaultCondition(kind: ScheduleConditionKind): ScheduleCondition {
  switch (kind) {
    case "outputContains":
      return { kind, pattern: "", inLastRunOf: "self" };
    case "httpStatus":
      return { kind, url: "", expect: 200 };
    case "commandExitCode":
      return { kind, command: "", expect: 0 };
    case "weekdays":
      return { kind, weekdays: [1] };
    default:
      return { kind, minutes: 60 };
  }
}
