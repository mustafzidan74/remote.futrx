import { useEffect, useRef, useState } from "preact/hooks";
import type { Playbook } from "../../../models/playbook";
import type { PlaybookLibrary } from "../../../state/hooks/chat/usePlaybooks";
import { playbookLabel } from "../../../state/chat/playbookState";
import { ChevronDown, Loader, Zap } from "../../primitives/icons";

/**
 * The composer's Playbooks menu: one click applies a saved prompt's skills,
 * mode, and provider, then puts the prompt in the composer. Shift-click sends
 * it straight away — the safe default is insert, so the user always sees what
 * is about to run.
 */
export function PlaybookPicker({ library, disabled }: { library: PlaybookLibrary; disabled: boolean }) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    function closeOnOutsideClick(event: MouseEvent) {
      const target = event.target as Node | null;
      if (target && !rootRef.current?.contains(target)) setOpen(false);
    }
    window.addEventListener("mousedown", closeOnOutsideClick);
    return () => window.removeEventListener("mousedown", closeOnOutsideClick);
  }, [open]);

  function choose(playbook: Playbook, shiftKey: boolean) {
    setOpen(false);
    void library.run(playbook, { send: shiftKey });
  }

  const count = library.playbooks.length;

  return (
    <div ref={rootRef} class="codex-playbook-control-root relative w-[122px] flex-none sm:w-[136px]">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        disabled={disabled}
        class={`codex-playbook-control flex h-7 w-full items-center justify-between gap-1.5 rounded-md px-2 text-left transition
                disabled:cursor-not-allowed disabled:opacity-40
                ${open ? "bg-accent-blue/[0.14] text-accent-blue" : "bg-accent-blue/[0.08] text-ink-100 hover:bg-accent-blue/[0.12]"}`}
        aria-haspopup="menu"
        aria-expanded={open}
        title="Playbooks — one-click prompt templates"
      >
        <span class="flex min-w-0 items-center gap-1.5">
          {library.running ? (
            <Loader class="h-3 w-3 flex-none animate-spin" />
          ) : (
            <Zap class="h-3 w-3 flex-none" />
          )}
          <span class="truncate text-[11.5px] font-semibold">Playbooks</span>
        </span>
        <span class="inline-flex flex-none items-center gap-1">
          <span class="rounded bg-white/10 px-1 py-0.5 text-[10px] leading-none text-ink-300">
            {library.loading ? "..." : count}
          </span>
          <ChevronDown class="h-3 w-3 flex-none" />
        </span>
      </button>

      {open && (
        <div
          class="theme-menu-surface absolute left-0 bottom-full z-40 mb-2 w-[calc(100vw-1.5rem)] overflow-hidden rounded-lg
                 border border-white/10 bg-[#14161d] shadow-2xl sm:left-auto sm:right-0 sm:w-[420px]"
          role="menu"
        >
          <div class="border-b border-white/10 bg-[#191a1f] px-3 py-2 text-[11px] leading-4 text-ink-400">
            Click to load a playbook into the composer. Shift-click to send it immediately.
          </div>
          <div class="max-h-[320px] overflow-y-auto py-1">
            {library.error ? (
              <div class="px-3 py-3 text-[12px] text-red-300">{library.error}</div>
            ) : library.loading ? (
              <div class="px-3 py-3 text-[12px] text-ink-400">Loading playbooks...</div>
            ) : count === 0 ? (
              <div class="px-3 py-3 text-[12px] leading-relaxed text-ink-400">
                No playbooks yet. An administrator can add them under Settings → Playbooks.
              </div>
            ) : (
              library.playbooks.map((playbook) => (
                <button
                  key={playbook.id}
                  type="button"
                  role="menuitem"
                  onClick={(event) => choose(playbook, event.shiftKey)}
                  disabled={library.running !== null}
                  class="w-full px-3 py-2.5 text-left focus:outline-none hover:bg-white/[0.07] focus:bg-white/[0.07]
                         disabled:cursor-not-allowed disabled:opacity-50"
                >
                  <div class="flex min-w-0 items-center gap-2">
                    <span class="truncate text-[13px] font-medium text-ink-100">
                      {playbookLabel(playbook)}
                    </span>
                    {playbook.mode && (
                      <span class="flex-none rounded bg-white/[0.08] px-1.5 py-0.5 text-[10px] uppercase text-ink-400">
                        {playbook.mode}
                      </span>
                    )}
                    {playbook.provider && (
                      <span class="flex-none rounded bg-accent-blue/[0.16] px-1.5 py-0.5 text-[10px] uppercase text-accent-blue">
                        {playbook.provider}
                      </span>
                    )}
                  </div>
                  {playbook.hint && (
                    <div class="mt-1 max-h-8 overflow-hidden text-[12px] leading-4 text-ink-400">
                      {playbook.hint}
                    </div>
                  )}
                </button>
              ))
            )}
          </div>
        </div>
      )}
    </div>
  );
}
