import { useState } from "preact/hooks";
import type { ProjectScreenshot, ScreenshotDelivery } from "../../models/screenshot";
import { Bell, Check, Copy, Download, ExternalLink, Link2, X } from "../primitives/icons";

/**
 * The card a capture appears in: a thumbnail plus what you can do with it.
 *
 * The same component serves the composer's status area and the preview
 * popover, because a screenshot means the same thing in both places — the
 * only difference is where the caller wants it rendered.
 */
export function ScreenshotCard({
  screenshot,
  delivered,
  publicUrl,
  sending,
  canSend,
  onInsert,
  onSend,
  onDismiss,
}: {
  screenshot: ProjectScreenshot;
  delivered?: ScreenshotDelivery[];
  /** A 24h login-less link, present only when a text-only sink needed one. */
  publicUrl?: string;
  sending?: boolean;
  /** False when no notification sink is configured on this server. */
  canSend?: boolean;
  onInsert?: () => void;
  onSend?: () => void;
  onDismiss?: () => void;
}) {
  const [copied, setCopied] = useState<"none" | "read" | "public">("none");
  const absolute = `${window.location.origin}${screenshot.url}`;

  async function copy(value: string, which: "read" | "public") {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(which);
    } catch {
      setCopied("none");
    }
  }

  return (
    <div class="rounded-md border border-white/[0.08] bg-white/[0.03] p-2">
      <div class="flex items-start gap-2.5">
        <a
          href={screenshot.url}
          target="_blank"
          rel="noopener noreferrer"
          class="block h-14 w-24 flex-none overflow-hidden rounded border border-white/10 bg-black/40"
          title="Open the full-size capture"
        >
          <img
            src={screenshot.url}
            alt={`Preview of port ${screenshot.port}${screenshot.path}`}
            loading="lazy"
            class="h-full w-full object-cover object-top"
          />
        </a>
        <div class="min-w-0 flex-1">
          <div class="flex items-center gap-2">
            <span class="font-mono text-[12px] font-semibold text-ink-50">
              :{screenshot.port}
              {screenshot.path}
            </span>
            {onDismiss && (
              <button
                type="button"
                onClick={onDismiss}
                class="ml-auto grid h-5 w-5 flex-none place-items-center rounded text-ink-300
                       hover:bg-white/[0.08] hover:text-ink-50"
                aria-label="Dismiss the screenshot"
              >
                <X class="h-3 w-3" />
              </button>
            )}
          </div>
          <div class="mt-0.5 text-[10.5px] text-ink-400">
            {screenshot.width}×{screenshot.height} · {formatBytes(screenshot.bytes)} ·{" "}
            {new Date(screenshot.createdAt).toLocaleTimeString()}
          </div>

          <div class="mt-1.5 flex flex-wrap items-center gap-1.5">
            {onInsert && (
              <CardButton onClick={onInsert} label="Insert into chat">
                <ExternalLink class="h-3 w-3" />
              </CardButton>
            )}
            <CardButton
              onClick={() => void copy(absolute, "read")}
              label={copied === "read" ? "Copied" : "Copy link"}
            >
              {copied === "read" ? <Check class="h-3 w-3" /> : <Copy class="h-3 w-3" />}
            </CardButton>
            <a
              href={screenshot.url}
              download={screenshot.file}
              class="inline-flex h-7 items-center gap-1.5 rounded-md border border-white/10 bg-white/[0.05] px-2.5
                     text-[11.5px] font-medium text-ink-200 transition hover:bg-white/[0.09] hover:text-ink-100"
            >
              <Download class="h-3 w-3" />
              Download
            </a>
            {canSend && onSend && (
              <CardButton
                onClick={onSend}
                disabled={sending}
                label={sending ? "Sending…" : "Send to chat apps"}
              >
                <Bell class="h-3 w-3" />
              </CardButton>
            )}
          </div>
        </div>
      </div>

      {delivered && delivered.length > 0 && (
        <ul class="mt-1.5 space-y-0.5 border-t border-white/[0.07] pt-1.5">
          {delivered.map((row) => (
            <li key={row.sink} class="text-[10.5px] leading-4">
              <span class="font-mono text-ink-300">{row.sink}</span>{" "}
              <span class={row.delivered ? "text-accent-blue" : "text-ink-400"}>
                {row.delivered ? "sent" : row.error || "not delivered"}
              </span>
            </li>
          ))}
        </ul>
      )}

      {publicUrl && (
        <div class="mt-1.5 rounded border border-accent-blue/30 bg-accent-blue/[0.08] p-2">
          <div class="flex items-center gap-1.5 text-[11px] font-semibold text-ink-50">
            <Link2 class="h-3 w-3 flex-none" />
            Public link, valid 24 hours
          </div>
          <code class="mt-1 block break-all font-mono text-[10px] text-ink-100">{publicUrl}</code>
          <div class="mt-1 flex items-center gap-1.5">
            <CardButton
              onClick={() => void copy(publicUrl, "public")}
              label={copied === "public" ? "Copied" : "Copy public link"}
            >
              {copied === "public" ? <Check class="h-3 w-3" /> : <Copy class="h-3 w-3" />}
            </CardButton>
          </div>
          <div class="mt-1 text-[10px] text-ink-300">
            Minted because a configured chat app cannot carry pictures. Anyone with it can see this
            image until it expires.
          </div>
        </div>
      )}
    </div>
  );
}

function CardButton({
  onClick,
  label,
  disabled,
  children,
}: {
  onClick: () => void;
  label: string;
  disabled?: boolean;
  children: preact.ComponentChildren;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      class="inline-flex h-7 items-center gap-1.5 rounded-md border border-white/10 bg-white/[0.05] px-2.5
             text-[11.5px] font-medium text-ink-200 transition hover:bg-white/[0.09] hover:text-ink-100
             disabled:cursor-not-allowed disabled:opacity-50"
    >
      {children}
      {label}
    </button>
  );
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${Math.round(bytes / 1024)} KB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
}
