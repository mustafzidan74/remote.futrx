import { useEffect, useRef, useState } from "preact/hooks";
import { providerDisplayLabel } from "../../../config/chat";
import type { TeamHopView, TeamRoleView, TeamView } from "../../../state/chat/teamState";
import { AlertCircle, Check, ExternalLink, Users, X } from "../../primitives/icons";

/**
 * The header's team pill and the panel it opens.
 *
 * The pill answers "what is the team doing right now" in three words; the
 * panel answers "and what did it say", which is the part that cannot fit in a
 * pill. Every hop links to the chat it ran in, because the summary is a
 * verdict and the reasoning behind it lives in the companion's own transcript
 * — a review you cannot read is a review you have to take on faith.
 */
export function TeamPanel({
  view,
  busy,
  onOpenChat,
  onStop,
}: {
  view: TeamView;
  busy: boolean;
  onOpenChat: (chatId: string) => void;
  onStop: () => void;
}) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    function closeOnOutsideClick(event: MouseEvent) {
      const target = event.target as Node | null;
      if (target && !rootRef.current?.contains(target)) setOpen(false);
    }
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key !== "Escape") return;
      event.stopPropagation();
      setOpen(false);
    }
    window.addEventListener("mousedown", closeOnOutsideClick);
    window.addEventListener("keydown", closeOnEscape, true);
    return () => {
      window.removeEventListener("mousedown", closeOnOutsideClick);
      window.removeEventListener("keydown", closeOnEscape, true);
    };
  }, [open]);

  if (!view.enabled) return null;

  const settled = view.phase === "done" || view.phase === "error";

  return (
    <div ref={rootRef} class="codex-team-panel-root relative flex-none">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        class={`codex-team-pill flex h-8 flex-none items-center gap-1.5 rounded-full border px-2.5 py-1
                text-[11.5px] font-semibold whitespace-nowrap
                ${
                  view.phase === "error"
                    ? "border-accent-red/30 bg-accent-red/[0.12] text-accent-red"
                    : "border-accent-purple/30 bg-accent-purple/[0.12] text-accent-purple"
                }`}
        aria-haspopup="dialog"
        aria-expanded={open}
        title={`Team mode — ${view.status}`}
      >
        <Users
          class={`h-3.5 w-3.5 flex-none ${view.running ? "animate-pulse" : ""}`}
          aria-hidden="true"
        />
        <span>{view.pillLabel}</span>
      </button>

      {open && (
        <div
          class="popover-surface theme-menu-surface absolute end-0 top-full z-40 mt-2
                 w-[min(24rem,calc(100vw-1.5rem))]"
          role="dialog"
          aria-label="Team"
        >
          <div class="flex items-start justify-between gap-2 border-b border-white/10 bg-[#191a1f] px-3 py-2">
            <div class="min-w-0">
              <div class="text-[12px] font-semibold text-ink-100">Team</div>
              <p class="mt-0.5 text-[11px] leading-4 text-ink-400">{view.status}</p>
            </div>
            <button
              type="button"
              onClick={() => setOpen(false)}
              class="grid h-6 w-6 flex-none place-items-center rounded-md text-ink-300 hover:bg-white/[0.08]"
              aria-label="Close the team panel"
            >
              <X class="h-3.5 w-3.5" />
            </button>
          </div>

          <div class="border-b border-white/10 px-3 py-2 space-y-1">
            <SeatRow seat={view.implementer} onOpenChat={onOpenChat} />
            {view.reviewer.enabled && (
              <SeatRow seat={view.reviewer} onOpenChat={onOpenChat} />
            )}
            {view.tester.enabled && <SeatRow seat={view.tester} onOpenChat={onOpenChat} />}
          </div>

          <div class="max-h-64 overflow-y-auto px-3 py-2">
            {view.hops.length === 0 ? (
              <p class="text-[11.5px] leading-4 text-ink-400">
                Nothing yet. The next turn you send starts a review.
              </p>
            ) : (
              <ol class="space-y-2">
                {view.hops.map((hop) => (
                  <HopRow key={hop.key} hop={hop} onOpenChat={onOpenChat} />
                ))}
              </ol>
            )}
          </div>

          {!settled && (
            <div class="border-t border-white/10 px-3 py-2">
              <button
                type="button"
                onClick={() => {
                  setOpen(false);
                  onStop();
                }}
                disabled={busy}
                class="h-8 w-full rounded-md border border-white/10 bg-white/[0.05] text-[12px] font-semibold
                       text-ink-100 hover:bg-white/[0.09] disabled:cursor-not-allowed disabled:opacity-40"
                title="Stop team mode — a hop already in flight keeps going"
              >
                Stop team mode
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

function SeatRow({
  seat,
  onOpenChat,
}: {
  seat: TeamRoleView;
  onOpenChat: (chatId: string) => void;
}) {
  return (
    <div class="flex items-center justify-between gap-2">
      <span class="min-w-0 truncate text-[12px] text-ink-200">
        {seat.label}
        <span class="text-ink-400"> · {providerDisplayLabel(seat.provider)}</span>
        {seat.model && <span class="text-ink-400"> · {seat.model}</span>}
      </span>
      {seat.chatId ? (
        <button
          type="button"
          onClick={() => onOpenChat(seat.chatId)}
          class="inline-flex flex-none items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] text-accent-blue
                 hover:bg-accent-blue/[0.14]"
        >
          Open chat
          <ExternalLink class="h-3 w-3" aria-hidden="true" />
        </button>
      ) : (
        <span class="flex-none text-[11px] text-ink-400">
          {seat.resolved ? "auto" : " "}
        </span>
      )}
    </div>
  );
}

function HopRow({
  hop,
  onOpenChat,
}: {
  hop: TeamHopView;
  onOpenChat: (chatId: string) => void;
}) {
  const good = hop.verdict === "ship" || hop.verdict === "pass";
  const bad = hop.verdict === "fix" || hop.verdict === "fail" || hop.verdict === "unknown";

  return (
    <li class="rounded-md border border-white/10 bg-white/[0.03] px-2.5 py-2">
      <div class="flex items-center justify-between gap-2">
        <span class="min-w-0 truncate text-[11px] text-ink-400">{hop.detail}</span>
        {hop.verdictLabel && (
          <span
            class={`inline-flex flex-none items-center gap-1 rounded-full px-1.5 py-0.5 text-[10px] font-semibold
                    ${
                      good
                        ? "bg-accent-green/[0.14] text-accent-green"
                        : bad
                          ? "bg-accent-yellow/[0.14] text-accent-yellow"
                          : "bg-white/[0.06] text-ink-300"
                    }`}
          >
            {good ? (
              <Check class="h-2.5 w-2.5" aria-hidden="true" />
            ) : bad ? (
              <AlertCircle class="h-2.5 w-2.5" aria-hidden="true" />
            ) : null}
            {hop.verdictLabel}
          </span>
        )}
      </div>
      {hop.findings && (
        <p
          dir="auto"
          class="bidi-auto mt-1 line-clamp-4 whitespace-pre-wrap break-words text-[11.5px] leading-4 text-ink-200"
        >
          {hop.findings}
        </p>
      )}
      {hop.chatId && (
        <button
          type="button"
          onClick={() => onOpenChat(hop.chatId)}
          class="mt-1 inline-flex items-center gap-1 text-[11px] text-accent-blue hover:underline"
        >
          Open the {hop.label.toLowerCase()}&rsquo;s chat
          <ExternalLink class="h-3 w-3" aria-hidden="true" />
        </button>
      )}
    </li>
  );
}
