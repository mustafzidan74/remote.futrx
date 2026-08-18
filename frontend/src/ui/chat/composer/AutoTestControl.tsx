import { useEffect, useRef, useState } from "preact/hooks";
import { Loader, TestTube } from "../../primitives/icons";

/**
 * The composer's Auto-test switch. One checkbox, but it gets a popover rather
 * than a bare toggle because "every turn now costs an extra agent run" is not
 * something a user should discover by watching their token spend.
 */
export function AutoTestControl({
  enabled,
  busy,
  onChange,
}: {
  enabled: boolean;
  busy: boolean;
  onChange: (enabled: boolean) => void;
}) {
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

  return (
    <div ref={rootRef} class="codex-autotest-control-root relative flex-none">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        disabled={busy}
        class={`codex-autotest-control composer-pill ${
          enabled ? "bg-accent-green/[0.18] text-accent-green hover:bg-accent-green/[0.24]" : ""
        }`}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={`Auto-test — ${enabled ? "on" : "off"}`}
        title="Auto-test — verify every change with Playwright"
      >
        {busy ? (
          <Loader class="h-4 w-4 flex-none animate-spin" />
        ) : (
          <TestTube class="h-4 w-4 flex-none" aria-hidden="true" />
        )}
        <span class="font-semibold">Auto-test</span>
      </button>

      {open && (
        <div
          class="popover-surface theme-menu-surface absolute left-0 bottom-full z-40 mb-2
                 w-[min(20rem,calc(100vw-1.5rem))] sm:left-auto sm:right-0"
          role="dialog"
          aria-label="Auto-test settings"
        >
          <div class="border-b border-white/10 bg-[#191a1f] px-3 py-2">
            <div class="text-[12px] font-semibold text-ink-100">Auto-test</div>
            <p class="mt-1 text-[11px] leading-4 text-ink-400">
              After every agent turn, run a Playwright check of the affected journey and report
              PASS/FAIL. This starts a second agent run each time, so it costs tokens.
            </p>
          </div>
          <label class="flex cursor-pointer items-start gap-2 px-3 py-2.5">
            <input
              type="checkbox"
              checked={enabled}
              disabled={busy}
              onChange={(event) => {
                setOpen(false);
                onChange((event.currentTarget as HTMLInputElement).checked);
              }}
              class="mt-0.5 h-3.5 w-3.5 flex-none accent-emerald-400"
            />
            <span class="text-[12.5px] leading-4 text-ink-100">
              Verify every change with Playwright
            </span>
          </label>
        </div>
      )}
    </div>
  );
}
