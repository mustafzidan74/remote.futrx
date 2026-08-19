import { useState } from "preact/hooks";
import type { AssistantMessagePart } from "../../../models/chatMessage";
import {
  buildTurnTimeline,
  timelineSummary,
  type TimelineStep,
} from "../../../state/chat/agentActivity";
import { AlertCircle, ChevronDown, ChevronRight, Loader } from "../../primitives/icons";
import { ToolCall } from "../tool-calls/ToolCall";
import { ToolShell } from "../tool-calls/ToolShell";

type ToolPart = Extract<AssistantMessagePart, { kind: "tool" }>;

/**
 * The steps of one assistant turn, as a log instead of a count.
 *
 * "3 tools used" is a number you cannot act on: it hides which file was
 * edited, which command took forty seconds, and which call is the one that
 * failed. The same three events already answer all of that, so this reshapes
 * them rather than asking the backend for anything new. Collapsed it still
 * takes one line; expanded it reads like a terminal transcript, with each row
 * opening into the existing specialised renderer on demand.
 */
export function TurnTimeline({
  parts,
  startIndex,
  chatId,
  onAnswerQuestion,
}: {
  parts: ToolPart[];
  startIndex: number;
  chatId?: string;
  onAnswerQuestion?: (text: string) => void;
}) {
  const steps = buildTurnTimeline(parts);
  const status = parts.some((part) => part.status === "running") ? "running" : "done";
  const isError = steps.some((step) => step.isError);

  return (
    <ToolShell
      icon={<span class="text-[13px] leading-none">🧭</span>}
      label={<span class="font-medium">{timelineSummary(steps)}</span>}
      badge={headline(steps)}
      status={status}
      isError={isError}
    >
      <ol class="codex-turn-timeline divide-y divide-white/[0.06]">
        {steps.map((step, offset) => (
          <TimelineRow
            key={step.key || `${startIndex}-${offset}`}
            step={step}
            part={parts[offset]}
            chatId={chatId}
            onAnswerQuestion={onAnswerQuestion}
          />
        ))}
      </ol>
    </ToolShell>
  );
}

/** The one step worth naming on the collapsed row: the failure, or the last. */
function headline(steps: TimelineStep[]): string | undefined {
  const step = steps.find((candidate) => candidate.isError) ?? steps[steps.length - 1];
  if (!step) return undefined;
  return step.target ? `${step.label} ${step.target}` : step.label;
}

function TimelineRow({
  step,
  part,
  chatId,
  onAnswerQuestion,
}: {
  step: TimelineStep;
  part: ToolPart | undefined;
  chatId?: string;
  onAnswerQuestion?: (text: string) => void;
}) {
  const [open, setOpen] = useState(false);

  return (
    <li class={step.isError ? "bg-accent-red/[0.06]" : ""}>
      <button
        type="button"
        onClick={() => setOpen((current) => !current)}
        class="flex w-full items-center gap-2 px-3 py-1.5 text-left hover:bg-white/[0.04]"
        aria-expanded={open}
      >
        {open ? (
          <ChevronDown class="h-3 w-3 flex-none text-ink-400" />
        ) : (
          <ChevronRight class="h-3 w-3 flex-none text-ink-400" />
        )}
        <span class="flex-none text-[12px] leading-none" aria-hidden="true">
          {step.icon}
        </span>
        <span class="min-w-0 flex-1 truncate text-[12px]" title={step.title || step.label}>
          <span class="text-ink-200">{step.label}</span>
          {step.target && (
            <>
              {" "}
              <code dir="auto" class="bidi-auto font-mono text-ink-100">
                {step.target}
              </code>
            </>
          )}
          {step.detail && (
            <span class="ms-1 font-mono text-[11px] text-ink-300">{step.detail}</span>
          )}
        </span>
        {step.status === "running" ? (
          <Loader class="h-3 w-3 flex-none animate-spin text-ink-300" />
        ) : step.isError ? (
          <AlertCircle class="h-3 w-3 flex-none text-accent-red" />
        ) : null}
        {step.duration && (
          <span class="flex-none tabular-nums text-[11px] text-ink-400">{step.duration}</span>
        )}
      </button>

      {step.note && (
        <p
          dir="auto"
          class={`bidi-auto truncate px-3 pb-1.5 ps-[3.35rem] font-mono text-[11px] leading-4
                  ${step.isError ? "text-accent-red" : "text-ink-400"}`}
        >
          {step.note}
        </p>
      )}

      {open && part && (
        <div class="px-2 pb-2">
          <ToolCall
            toolUseId={part.id}
            chatId={chatId}
            name={part.name}
            input={part.input}
            output={part.output}
            isError={part.isError}
            status={part.status}
            defaultOpen
            onAnswerQuestion={onAnswerQuestion}
          />
        </div>
      )}
    </li>
  );
}
