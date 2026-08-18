import type { ComponentChildren, ComponentType } from "preact";
import { AlertCircle, RotateCcw } from "./icons";

/**
 * The two states every list, table and panel in the app can end up in, so they
 * look the same wherever they appear: nothing here yet, and something broke.
 *
 * `EmptyState` is deliberately one icon, one line, and at most one action —
 * anything longer belongs in the panel description above it.
 */
export function EmptyState({
  Icon,
  title,
  hint,
  action,
}: {
  Icon: ComponentType<{ class?: string }>;
  title: string;
  hint?: string;
  action?: ComponentChildren;
}) {
  return (
    <div class="flex flex-col items-center gap-2 rounded-md border border-white/10 bg-white/[0.03] px-4 py-8 text-center">
      <span class="grid h-10 w-10 place-items-center rounded-full border border-white/10 bg-white/[0.04] text-ink-300">
        <Icon class="h-4 w-4" />
      </span>
      <div class="text-[13px] text-ink-200">{title}</div>
      {hint && <div class="max-w-sm text-[12px] leading-snug text-ink-300">{hint}</div>}
      {action}
    </div>
  );
}

/**
 * A failure the operator can act on. When a retry is genuinely available the
 * banner offers it inline rather than making them hunt for the Refresh button
 * in the panel header.
 */
export function ErrorBanner({
  message,
  onRetry,
  retryLabel = "Retry",
  retrying = false,
}: {
  message: string;
  onRetry?: () => void;
  retryLabel?: string;
  retrying?: boolean;
}) {
  return (
    <div
      role="alert"
      class="flex items-start gap-2.5 rounded-md border border-accent-red/30 bg-accent-red/[0.08] px-3 py-2.5 text-[13px]"
    >
      <AlertCircle class="mt-0.5 h-4 w-4 flex-none text-accent-red" aria-hidden="true" />
      <div dir="auto" class="bidi-auto min-w-0 flex-1 break-words text-accent-red">{message}</div>
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          disabled={retrying}
          class="inline-flex h-8 flex-none items-center gap-1.5 rounded-md border border-accent-red/30
                 px-2.5 text-[12px] font-medium text-accent-red transition-colors
                 hover:bg-accent-red/[0.14] disabled:opacity-60"
        >
          <RotateCcw class={`h-3.5 w-3.5 ${retrying ? "animate-spin" : ""}`} aria-hidden="true" />
          {retryLabel}
        </button>
      )}
    </div>
  );
}
