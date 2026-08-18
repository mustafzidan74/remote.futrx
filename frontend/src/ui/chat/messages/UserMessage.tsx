import type { SyntheticKind } from "../../../models/chat";
import { Eye, FileText, PlaneTakeoff, RotateCcw, TestTube, Users } from "../../primitives/icons";

/**
 * How each platform-issued prompt announces itself. Without a badge an
 * autopilot round or a team hand-off is indistinguishable from something the
 * user typed, which matters when reading back what an unattended agent was
 * told to do — and, for a team loop, *which* agent was told.
 */
const SYNTHETIC_BADGES: Record<
  SyntheticKind,
  { label: string; title: string; Icon: typeof PlaneTakeoff; tone: string }
> = {
  autopilot: {
    label: "auto-continue",
    title: "Sent automatically by autopilot to keep the agent working",
    Icon: PlaneTakeoff,
    tone: "bg-accent-blue/[0.14] text-accent-blue",
  },
  autotest: {
    label: "auto-test",
    title: "Sent automatically to verify the change with Playwright",
    Icon: TestTube,
    tone: "bg-accent-green/[0.14] text-accent-green",
  },
  "team-review": {
    label: "team review",
    title: "Sent by team mode: review the change another agent just made",
    Icon: Eye,
    tone: "bg-accent-purple/[0.14] text-accent-purple",
  },
  "team-test": {
    label: "team test",
    title: "Sent by team mode: run the Playwright pass over the change",
    Icon: TestTube,
    tone: "bg-accent-purple/[0.14] text-accent-purple",
  },
  "team-fix": {
    label: "team fix",
    title: "Sent by team mode: the review findings, handed back to the implementer",
    Icon: Users,
    tone: "bg-accent-purple/[0.14] text-accent-purple",
  },
  "team-summary": {
    label: "team",
    title: "Posted by team mode when the loop finished",
    Icon: Users,
    tone: "bg-accent-purple/[0.14] text-accent-purple",
  },
};

function SyntheticBadge({ kind }: { kind: SyntheticKind }) {
  const badge = SYNTHETIC_BADGES[kind];
  if (!badge) return null;
  const { Icon } = badge;
  return (
    <span
      class={`inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-[0.08em]
              ${badge.tone}`}
      title={badge.title}
    >
      <Icon class="h-2.5 w-2.5" aria-hidden="true" />
      {badge.label}
    </span>
  );
}

export function UserMessage({
  text,
  t,
  synthetic,
  onRewind,
  onSaveSnippet,
}: {
  text: string;
  t: number;
  synthetic?: SyntheticKind;
  onRewind?: (t: number, text: string) => void;
  /** Opens the snippet editor with this message as its body. */
  onSaveSnippet?: (text: string) => void;
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
        {(onRewind || onSaveSnippet) && (
          <div class="flex items-center gap-1 opacity-100 md:opacity-0 md:group-hover:opacity-100 transition">
            {onRewind && (
              <button
                type="button"
                onClick={() => onRewind(t, text)}
                class="inline-flex items-center gap-1.5 h-8 px-2 rounded-md text-[12px]
                       text-ink-300 hover:text-ink-100 hover:bg-white/[0.07]"
                title="Rewind and edit from here"
              >
                <RotateCcw class="w-3.5 h-3.5" />
                Rewind
              </button>
            )}
            {onSaveSnippet && (
              <button
                type="button"
                onClick={() => onSaveSnippet(text)}
                class="inline-flex items-center gap-1.5 h-8 px-2 rounded-md text-[12px]
                       text-ink-300 hover:text-ink-100 hover:bg-white/[0.07]"
                title="Save this message as a snippet"
              >
                <FileText class="w-3.5 h-3.5" />
                Save as snippet
              </button>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
