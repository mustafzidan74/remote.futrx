import type { ChatEventRouting, SyntheticKind } from "../../../models/chat";
import { Eye, FileText, GitFork, PlaneTakeoff, RotateCcw, TestTube, Users, Zap } from "../../primitives/icons";

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
  "github-review": {
    label: "from GitHub",
    title:
      "Composed from a GitHub pull request, issue, or review. The text inside came from " +
      "outside this server.",
    Icon: GitFork,
    tone: "bg-accent-orange/[0.14] text-accent-orange",
  },
};

function SyntheticBadge({ kind }: { kind: SyntheticKind }) {
  const badge = SYNTHETIC_BADGES[kind] ?? SYNTHETIC_BADGES.autopilot;
  const { Icon } = badge;
  return (
    <span
      class={`inline-flex items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] font-semibold uppercase tracking-[0.08em] ${badge.tone}`}
      title={badge.title}
    >
      <Icon class="h-2.5 w-2.5" aria-hidden="true" />
      {badge.label}
    </span>
  );
}

/**
 * Which model actually answered this turn, and which rule sent it there.
 *
 * Automatic routing means the model that answered is not always the one the
 * pill named when the prompt was typed, so the turn has to say so itself —
 * otherwise a reader comparing a cheap answer against an expensive bill has
 * nothing to go on. The rule name is the audit trail; the full reason,
 * including any fallback, is the tooltip.
 */
function RoutedBadge({ routing }: { routing: ChatEventRouting }) {
  const model = routing.model || "Auto";
  const rule = (routing.rule || "").trim();
  return (
    <span
      class="inline-flex items-center gap-1 rounded-full bg-accent-blue/[0.12] px-1.5 py-0.5
             text-[10px] font-semibold uppercase tracking-[0.06em] text-accent-blue"
      title={routing.reason || `Routed to ${routing.provider} ${model}`}
    >
      <Zap class="h-2.5 w-2.5" aria-hidden="true" />
      <span class="normal-case tracking-normal">
        {routing.provider} {model}
        {rule && <span class="text-ink-300"> · {rule}</span>}
      </span>
    </span>
  );
}

export function UserMessage({
  text,
  t,
  synthetic,
  routing,
  onRewind,
  onSaveSnippet,
}: {
  text: string;
  t: number;
  synthetic?: SyntheticKind;
  /** Present only when automatic routing chose this turn's model. */
  routing?: ChatEventRouting;
  onRewind?: (t: number, text: string) => void;
  /** Opens the snippet editor with this message as its body. */
  onSaveSnippet?: (text: string) => void;
}) {
  return (
    <div class="group flex justify-end">
      <div class="max-w-[92%] sm:max-w-[78%] flex flex-col items-end gap-1.5">
        {(synthetic || routing) && (
          <div class="flex flex-wrap items-center justify-end gap-1">
            {synthetic && <SyntheticBadge kind={synthetic} />}
            {routing && <RoutedBadge routing={routing} />}
          </div>
        )}
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
