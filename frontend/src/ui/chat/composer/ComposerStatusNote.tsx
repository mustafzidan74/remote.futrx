import type { SlashStatus } from "../../../state/hooks/chat/useSlashCommands";
import { AlertCircle, Info, Loader, X } from "../../primitives/icons";

/**
 * The composer's status line: what a slash command did, in the one place a
 * user is already looking.
 *
 * Commands like `/snapshot` and `/screenshot` never start an agent run, so
 * they have no transcript to report into — without this they would appear to
 * do nothing at all.
 */
export function ComposerStatusNote({
  status,
  onDismiss,
}: {
  status: SlashStatus;
  onDismiss: () => void;
}) {
  const tone = TONES[status.tone];
  const Icon = tone.Icon;
  return (
    <div class={`mx-3 mt-2 flex items-start gap-2 rounded-md border px-2.5 py-2 ${tone.box}`}>
      <Icon
        class={`mt-0.5 h-3.5 w-3.5 flex-none ${tone.icon} ${status.tone === "busy" ? "animate-spin" : ""}`}
        aria-hidden="true"
      />
      <div class="min-w-0 flex-1" role="status" aria-live="polite">
        <div class="break-words text-[11.5px] leading-4 text-ink-100">{status.text}</div>
        {status.detail && (
          <pre class="mt-1 max-h-32 overflow-auto whitespace-pre-wrap break-words font-mono text-[10.5px] leading-4 text-ink-300">
            {status.detail}
          </pre>
        )}
      </div>
      <button
        type="button"
        onClick={onDismiss}
        class="grid h-5 w-5 flex-none place-items-center rounded text-ink-300 hover:bg-white/[0.08] hover:text-ink-50"
        aria-label="Dismiss"
      >
        <X class="h-3 w-3" />
      </button>
    </div>
  );
}

const TONES = {
  info: {
    box: "border-accent-blue/30 bg-accent-blue/[0.08]",
    icon: "text-accent-blue",
    Icon: Info,
  },
  busy: {
    box: "border-white/10 bg-white/[0.04]",
    icon: "text-ink-300",
    Icon: Loader,
  },
  error: {
    box: "border-accent-red/30 bg-accent-red/[0.08]",
    icon: "text-accent-red",
    Icon: AlertCircle,
  },
} as const;
