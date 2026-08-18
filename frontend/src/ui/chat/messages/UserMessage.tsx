import type { SyntheticKind } from "../../../models/chat";
import { PlaneTakeoff, RotateCcw, TestTube } from "../../primitives/icons";

/**
 * The badge above a prompt the platform sent on the operator's behalf. Without
 * it an autopilot round is indistinguishable from something the user typed,
 * which matters when reading back what an unattended agent was told to do.
 */
function SyntheticBadge({ kind }: { kind: SyntheticKind }) {
  const isTest = kind === "autotest";
  const Icon = isTest ? TestTube : PlaneTakeoff;
  return (
    <span
      class={`inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-[0.08em]
              ${isTest ? "bg-accent-green/[0.14] text-accent-green" : "bg-accent-blue/[0.14] text-accent-blue"}`}
      title={
        isTest
          ? "Sent automatically to verify the change with Playwright"
          : "Sent automatically by autopilot to keep the agent working"
      }
    >
      <Icon class="h-2.5 w-2.5" aria-hidden="true" />
      {isTest ? "auto-test" : "auto-continue"}
    </span>
  );
}

export function UserMessage({
  text,
  t,
  synthetic,
  onRewind,
}: {
  text: string;
  t: number;
  synthetic?: SyntheticKind;
  onRewind?: (t: number, text: string) => void;
}) {
  return (
    <div class="group flex justify-end">
      <div class="max-w-[92%] sm:max-w-[78%] flex flex-col items-end gap-1.5">
        {synthetic && <SyntheticBadge kind={synthetic} />}
        <div
          dir="auto"
          class={`codex-user-bubble bidi-auto border rounded-[18px] rounded-br-md px-3.5 py-2.5 text-[14.5px] leading-relaxed
                 whitespace-pre-wrap break-words shadow-sm
                 ${synthetic ? "border-white/10 bg-white/[0.05] text-ink-200" : "bg-accent-blue/15 border-accent-blue/30"}`}>
          {text}
        </div>
        {onRewind && (
          <button
            type="button"
            onClick={() => onRewind(t, text)}
            class="inline-flex items-center gap-1.5 h-7 px-2 rounded-md text-[12px]
                   text-ink-300 hover:text-ink-100 hover:bg-white/[0.07]
                   opacity-100 md:opacity-0 md:group-hover:opacity-100 transition"
            title="Rewind and edit from here"
          >
            <RotateCcw class="w-3.5 h-3.5" />
            Rewind
          </button>
        )}
      </div>
    </div>
  );
}
