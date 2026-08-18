import { useEffect, useRef, useState } from "preact/hooks";
import {
  AUTO_TEST_PROMPT,
  SMOKE_TEST_PROMPT,
  canSendUrlCheck,
  urlCheckPrompt,
} from "../../../state/chat/testPrompts";
import { ChevronDown, TestTube } from "../../primitives/icons";

/**
 * The composer's Test menu: three one-click Playwright checks a human can fire
 * without composing the prompt themselves.
 *
 * Every item sends with the `autotest` label, so a check the user asked for is
 * badged in the transcript exactly like one the auto-test policy started —
 * both are verification passes, and reading back which was which matters less
 * than seeing that a run was a check rather than more implementation.
 */
export function TestMenu({
  disabled,
  onSendTest,
}: {
  disabled: boolean;
  onSendTest: (prompt: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [urlFormOpen, setUrlFormOpen] = useState(false);
  const [url, setUrl] = useState("");
  const [expectation, setExpectation] = useState("");
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

  useEffect(() => {
    if (!open) setUrlFormOpen(false);
  }, [open]);

  function send(prompt: string) {
    setOpen(false);
    onSendTest(prompt);
  }

  function sendUrlCheck() {
    const input = { url, expectation };
    if (!canSendUrlCheck(input)) return;
    setUrl("");
    setExpectation("");
    send(urlCheckPrompt(input));
  }

  return (
    <div ref={rootRef} class="codex-test-menu-root relative flex-none">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        disabled={disabled}
        class={`codex-test-menu flex h-7 items-center gap-1.5 rounded-md px-2 transition
                disabled:cursor-not-allowed disabled:opacity-40
                ${open ? "bg-white/[0.1] text-ink-50" : "bg-white/[0.045] text-ink-200 hover:bg-white/[0.08]"}`}
        aria-haspopup="menu"
        aria-expanded={open}
        title="Run a Playwright check now"
      >
        <TestTube class="h-3 w-3 flex-none" aria-hidden="true" />
        <span class="text-[11.5px] font-semibold">Test</span>
        <ChevronDown class="h-3 w-3 flex-none" aria-hidden="true" />
      </button>

      {open && (
        <div
          class="theme-menu-surface absolute left-0 bottom-full z-40 mb-2 w-[min(22rem,calc(100vw-1.5rem))]
                 overflow-hidden rounded-lg border border-white/10 bg-[#14161d] shadow-2xl sm:left-auto sm:right-0"
          role="menu"
        >
          <div class="border-b border-white/10 bg-[#191a1f] px-3 py-2 text-[11px] leading-4 text-ink-400">
            Runs Playwright in this project through the <code>playwright-e2e</code> skill and reports
            PASS/FAIL.
          </div>

          <button
            type="button"
            role="menuitem"
            onClick={() => send(AUTO_TEST_PROMPT)}
            class="w-full px-3 py-2.5 text-left hover:bg-white/[0.07] focus:bg-white/[0.07] focus:outline-none"
          >
            <div class="text-[13px] font-medium text-ink-100">Test the last change</div>
            <div class="mt-0.5 text-[11.5px] leading-4 text-ink-400">
              Cover the journey the agent just touched, and fix the change if the spec fails.
            </div>
          </button>

          <button
            type="button"
            role="menuitem"
            onClick={() => setUrlFormOpen((value) => !value)}
            aria-expanded={urlFormOpen}
            class="w-full border-t border-white/[0.07] px-3 py-2.5 text-left hover:bg-white/[0.07]
                   focus:bg-white/[0.07] focus:outline-none"
          >
            <div class="text-[13px] font-medium text-ink-100">Test a URL or flow…</div>
            <div class="mt-0.5 text-[11.5px] leading-4 text-ink-400">
              Point at one page and say what should be true.
            </div>
          </button>

          {urlFormOpen && (
            <div class="border-t border-white/[0.07] bg-white/[0.02] px-3 py-2.5">
              <input
                type="text"
                value={url}
                placeholder="http://localhost:3000/checkout"
                onInput={(event) => setUrl((event.currentTarget as HTMLInputElement).value)}
                class="mb-1.5 h-8 w-full rounded-md border border-white/10 bg-white/[0.04] px-2 text-[12.5px]
                       text-ink-50 placeholder:text-ink-500 focus:border-accent-blue/40 focus:outline-none"
                aria-label="URL to check"
              />
              <input
                type="text"
                value={expectation}
                placeholder="What should be true? (optional)"
                onInput={(event) => setExpectation((event.currentTarget as HTMLInputElement).value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.preventDefault();
                    sendUrlCheck();
                  }
                }}
                class="h-8 w-full rounded-md border border-white/10 bg-white/[0.04] px-2 text-[12.5px]
                       text-ink-50 placeholder:text-ink-500 focus:border-accent-blue/40 focus:outline-none"
                aria-label="What to check"
              />
              <button
                type="button"
                disabled={!canSendUrlCheck({ url, expectation })}
                onClick={sendUrlCheck}
                class="mt-2 h-8 w-full rounded-md bg-accent-blue/20 text-[12px] font-semibold text-accent-blue
                       hover:bg-accent-blue/30 disabled:cursor-not-allowed disabled:opacity-40"
              >
                Run this check
              </button>
            </div>
          )}

          <button
            type="button"
            role="menuitem"
            onClick={() => send(SMOKE_TEST_PROMPT)}
            class="w-full border-t border-white/[0.07] px-3 py-2.5 text-left hover:bg-white/[0.07]
                   focus:bg-white/[0.07] focus:outline-none"
          >
            <div class="text-[13px] font-medium text-ink-100">Test the whole app</div>
            <div class="mt-0.5 text-[11.5px] leading-4 text-ink-400">
              A smoke suite over the main journeys, with a PASS/FAIL line for each.
            </div>
          </button>
        </div>
      )}
    </div>
  );
}
