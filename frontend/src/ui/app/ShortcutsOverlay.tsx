import { createPortal } from "preact/compat";
import { useEffect, useState } from "preact/hooks";
import { Keyboard, X } from "../primitives/icons";

/**
 * The `?` overlay.
 *
 * It lists only shortcuts that exist. A help sheet that advertises a key which
 * does nothing is worse than no help sheet, so every row here maps to a live
 * handler: Ctrl/Cmd+K in `CommandPalette`, Ctrl/Cmd+Enter in `PromptTextarea`,
 * Escape in `useChatKeyboardShortcuts`, and the roving tabindex in the
 * settings and project navigations.
 */
const SHORTCUTS: Array<{ keys: string[]; description: string }> = [
  { keys: ["Ctrl", "K"], description: "Open the command palette" },
  { keys: ["Ctrl", "Enter"], description: "Send the prompt from the composer" },
  { keys: ["Esc"], description: "Stop the running agent, or close the open menu" },
  { keys: ["?"], description: "Show this list" },
  { keys: ["↑", "↓"], description: "Move between settings sections, or palette results" },
  { keys: ["Home", "End"], description: "Jump to the first or last settings section" },
];

export function ShortcutsOverlay() {
  const [open, setOpen] = useState(false);

  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape" && open) {
        event.stopPropagation();
        setOpen(false);
        return;
      }
      if (event.key !== "?") return;
      // "?" is an ordinary character in a prompt, a search box or a filename.
      if (isTyping(event.target)) return;
      event.preventDefault();
      setOpen((current) => !current);
    }
    window.addEventListener("keydown", onKeyDown, true);
    return () => window.removeEventListener("keydown", onKeyDown, true);
  }, [open]);

  if (!open) return null;

  return createPortal(
    <div
      class="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4"
      onClick={() => setOpen(false)}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="shortcuts-overlay-title"
        class="dialog-panel popover-surface theme-menu-surface flex w-full max-w-md flex-col"
        onClick={(event) => event.stopPropagation()}
      >
        <header class="flex flex-none items-center gap-2 border-b border-white/[0.07] px-4 py-3">
          <Keyboard class="h-4 w-4 flex-none text-ink-300" aria-hidden="true" />
          <h2 id="shortcuts-overlay-title" class="flex-1 text-[14.5px] font-semibold text-ink-50">
            Keyboard shortcuts
          </h2>
          <button
            type="button"
            onClick={() => setOpen(false)}
            class="grid h-8 w-8 flex-none place-items-center rounded-md text-ink-300 hover:bg-white/[0.08] hover:text-ink-50"
            aria-label="Close"
          >
            <X class="h-4 w-4" />
          </button>
        </header>

        <dl class="min-h-0 flex-1 overflow-y-auto touch-scroll scrollbar-thin px-4 py-3">
          {SHORTCUTS.map((shortcut) => (
            <div
              key={shortcut.keys.join("+")}
              class="flex items-center gap-4 border-b border-white/[0.05] py-2.5 last:border-b-0"
            >
              <dt class="flex flex-none items-center gap-1">
                {shortcut.keys.map((key) => (
                  <kbd
                    key={key}
                    class="rounded border border-white/10 bg-white/[0.05] px-1.5 py-0.5 text-[11px] font-medium text-ink-100"
                  >
                    {key}
                  </kbd>
                ))}
              </dt>
              <dd class="min-w-0 flex-1 text-[12.5px] leading-snug text-ink-300">
                {shortcut.description}
              </dd>
            </div>
          ))}
        </dl>

        <footer class="flex-none border-t border-white/[0.07] px-4 py-2 text-[11px] text-ink-400">
          On macOS, Ctrl means &#8984; Command.
        </footer>
      </div>
    </div>,
    document.body,
  );
}

function isTyping(target: EventTarget | null): boolean {
  const element = target as HTMLElement | null;
  if (!element) return false;
  const tag = element.tagName;
  return tag === "INPUT" || tag === "TEXTAREA" || tag === "SELECT" || element.isContentEditable;
}
