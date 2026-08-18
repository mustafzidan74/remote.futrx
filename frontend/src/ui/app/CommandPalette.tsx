import { createPortal } from "preact/compat";
import { useEffect, useMemo, useReducer, useRef } from "preact/hooks";
import {
  commandPaletteState,
  filterCommands,
  sectionsOf,
  type CommandItem,
} from "../../state/app/commandPaletteState";
import { Command, MessageSquare, Search, Settings, Folder, Zap } from "../primitives/icons";

/**
 * Ctrl/Cmd+K: one keystroke to anything.
 *
 * The workspace hides most of itself behind navigation — a chat lives inside a
 * project group inside the sidebar, a settings page inside a tab list inside a
 * settings view. The palette flattens all of it into one ranked list, so the
 * operator types what they want instead of remembering where it lives.
 */
export function CommandPalette({ items }: { items: CommandItem[] }) {
  const [state, dispatch] = useReducer(
    commandPaletteState.reduce,
    commandPaletteState.createInitial(),
  );
  const inputRef = useRef<HTMLInputElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const results = useMemo(() => filterCommands(items, state.query), [items, state.query]);
  const highlight = results.length === 0 ? 0 : Math.min(state.highlight, results.length - 1);

  // Ctrl/Cmd+K works from anywhere, including inside the composer: it is the
  // one shortcut that has to interrupt whatever the operator is typing into.
  useEffect(() => {
    function onKeyDown(event: KeyboardEvent) {
      if (event.key.toLowerCase() !== "k" || !(event.ctrlKey || event.metaKey)) return;
      event.preventDefault();
      dispatch({ type: "toggle" });
    }
    window.addEventListener("keydown", onKeyDown);
    return () => window.removeEventListener("keydown", onKeyDown);
  }, []);

  useEffect(() => {
    if (!state.open) return;
    inputRef.current?.focus();
  }, [state.open]);

  // Keeps the keyboard cursor on screen while arrowing through a long list.
  useEffect(() => {
    if (!state.open) return;
    listRef.current
      ?.querySelector<HTMLElement>(`[data-command-index="${highlight}"]`)
      ?.scrollIntoView({ block: "nearest" });
  }, [state.open, highlight]);

  if (!state.open) return null;

  function close() {
    dispatch({ type: "close" });
  }

  function pick(item: CommandItem | undefined) {
    if (!item) return;
    close();
    item.run();
  }

  function onKeyDown(event: KeyboardEvent) {
    if (event.key === "ArrowDown") {
      event.preventDefault();
      dispatch({ type: "move", delta: 1, count: results.length });
    } else if (event.key === "ArrowUp") {
      event.preventDefault();
      dispatch({ type: "move", delta: -1, count: results.length });
    } else if (event.key === "Enter") {
      event.preventDefault();
      pick(results[highlight]);
    } else if (event.key === "Escape") {
      // Swallowed, so closing the palette never also cancels a running agent.
      event.preventDefault();
      event.stopPropagation();
      close();
    }
  }

  let index = -1;

  return createPortal(
    <div
      class="fixed inset-0 z-50 flex justify-center bg-black/60 px-3 pb-3 pt-[8vh]"
      onClick={close}
    >
      <div
        role="dialog"
        aria-modal="true"
        aria-label="Command palette"
        class="dialog-panel popover-surface theme-menu-surface flex w-full max-w-xl flex-col"
        onClick={(event) => event.stopPropagation()}
        onKeyDown={onKeyDown}
      >
        <label class="flex flex-none items-center gap-2 border-b border-white/[0.07] px-3 py-2.5">
          <Search class="h-4 w-4 flex-none text-ink-400" aria-hidden="true" />
          <input
            ref={inputRef}
            value={state.query}
            onInput={(event) =>
              dispatch({
                type: "set-query",
                query: (event.currentTarget as HTMLInputElement).value,
              })
            }
            placeholder="Search chats, projects, settings and actions"
            class="min-w-0 flex-1 bg-transparent text-[15px] text-ink-50 placeholder:text-ink-400 focus:outline-none"
            role="combobox"
            aria-expanded="true"
            aria-controls="command-palette-results"
            aria-autocomplete="list"
            autocomplete="off"
            spellcheck={false}
          />
          <kbd class="flex-none rounded border border-white/10 px-1.5 py-0.5 text-[10px] text-ink-400">
            Esc
          </kbd>
        </label>

        <div
          ref={listRef}
          id="command-palette-results"
          role="listbox"
          aria-label="Commands"
          class="min-h-0 flex-1 overflow-y-auto touch-scroll scrollbar-thin p-1.5"
        >
          {results.length === 0 ? (
            <div class="px-3 py-8 text-center text-[13px] text-ink-400">
              Nothing matches &ldquo;{state.query.trim()}&rdquo;.
            </div>
          ) : (
            sectionsOf(results).map((section) => (
              <div key={section.group} class="mb-1 last:mb-0">
                <div class="popover-label px-2 pb-1 pt-1.5">{section.group}</div>
                {section.items.map((item) => {
                  index += 1;
                  const rowIndex = index;
                  const active = rowIndex === highlight;
                  return (
                    <button
                      key={item.id}
                      type="button"
                      role="option"
                      aria-selected={active}
                      data-command-index={rowIndex}
                      onMouseEnter={() => dispatch({ type: "highlight", index: rowIndex })}
                      onClick={() => pick(item)}
                      class={`flex w-full items-center gap-2.5 rounded-md px-2 py-2 text-left transition
                              ${active ? "bg-accent-blue/[0.16] text-ink-50" : "text-ink-100 hover:bg-white/[0.06]"}`}
                    >
                      <CommandIcon kind={item.kind} active={active} />
                      <span class="min-w-0 flex-1">
                        <span dir="auto" class="bidi-auto block truncate text-[13.5px] font-medium">
                          {item.title}
                        </span>
                        {item.subtitle && (
                          <span class="block truncate text-[11.5px] text-ink-400">
                            {item.subtitle}
                          </span>
                        )}
                      </span>
                    </button>
                  );
                })}
              </div>
            ))
          )}
        </div>

        <footer class="flex flex-none items-center gap-3 border-t border-white/[0.07] px-3 py-2 text-[11px] text-ink-400">
          <span>&uarr;&darr; move</span>
          <span>&crarr; open</span>
          <span class="ms-auto inline-flex items-center gap-1">
            <Command class="h-3 w-3" aria-hidden="true" /> K
          </span>
        </footer>
      </div>
    </div>,
    document.body,
  );
}

function CommandIcon({ kind, active }: { kind: CommandItem["kind"]; active: boolean }) {
  const Icon =
    kind === "chat" ? MessageSquare : kind === "project" ? Folder : kind === "settings" ? Settings : Zap;
  return (
    <Icon
      class={`h-4 w-4 flex-none ${active ? "text-accent-blue" : "text-ink-400"}`}
      aria-hidden="true"
    />
  );
}
