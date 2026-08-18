import { useEffect, useRef } from "preact/hooks";
import {
  SLASH_GROUP_LABEL,
  type SlashCommand,
  type SlashGroup,
} from "../../../state/chat/slashCommandState";
import { Code, TestTube, Zap } from "../../primitives/icons";

/**
 * The `/` menu, anchored above the textarea.
 *
 * It is a listbox the textarea drives rather than a focusable menu of its own:
 * the caret never leaves the draft, so typing keeps filtering while the arrows
 * move the selection. Group headers appear only where the group changes, which
 * keeps the built-in verbs visually separate from a long skill library without
 * splitting the list into scroll regions.
 */
export function SlashCommandMenu({
  items,
  activeIndex,
  query,
  onSelect,
  onHover,
}: {
  items: SlashCommand[];
  activeIndex: number;
  query: string;
  onSelect: (entry: SlashCommand, options: { send: boolean }) => void;
  onHover: (index: number) => void;
}) {
  const listRef = useRef<HTMLDivElement>(null);

  // Keyboard navigation has to keep the selection visible; the list is
  // scrollable and a skill library can be long.
  useEffect(() => {
    const active = listRef.current?.querySelector<HTMLElement>('[data-slash-active="true"]');
    active?.scrollIntoView({ block: "nearest" });
  }, [activeIndex, items.length]);

  if (items.length === 0) return null;

  let lastGroup: SlashGroup | null = null;

  return (
    <div
      class="theme-menu-surface absolute bottom-full left-0 right-0 z-40 mb-2 overflow-hidden rounded-lg
             border border-white/10 bg-[#14161d] shadow-2xl"
      role="listbox"
      aria-label="Slash commands"
    >
      <div ref={listRef} class="max-h-[280px] overflow-y-auto touch-scroll scrollbar-thin py-1">
        {items.map((entry, index) => {
          const showHeader = entry.group !== lastGroup;
          lastGroup = entry.group;
          const active = index === activeIndex;
          return (
            <div key={entry.id}>
              {showHeader && (
                <div class="px-3 pb-1 pt-2 text-[10px] font-semibold uppercase tracking-[0.12em] text-ink-400">
                  {SLASH_GROUP_LABEL[entry.group]}
                </div>
              )}
              <button
                type="button"
                role="option"
                aria-selected={active}
                data-slash-active={active ? "true" : "false"}
                // Selecting must not steal focus from the textarea first: a
                // blur would move the caret and close the menu mid-click.
                onMouseDown={(event) => event.preventDefault()}
                onMouseEnter={() => onHover(index)}
                onClick={(event) => onSelect(entry, { send: event.shiftKey })}
                class={`flex w-full items-baseline gap-2 px-3 py-1.5 text-left focus:outline-none ${
                  active ? "bg-white/[0.09]" : "hover:bg-white/[0.05]"
                }`}
              >
                <GroupIcon group={entry.group} />
                <span class="flex-none font-mono text-[12.5px] font-semibold text-ink-50">
                  /{entry.command}
                </span>
                {entry.argHint && (
                  <span class="flex-none font-mono text-[11px] text-ink-400">{entry.argHint}</span>
                )}
                {entry.hint && (
                  <span class="min-w-0 flex-1 truncate text-[11.5px] text-ink-400" title={entry.hint}>
                    {entry.hint}
                  </span>
                )}
              </button>
            </div>
          );
        })}
      </div>
      <div class="border-t border-white/[0.07] bg-[#191a1f] px-3 py-1.5 text-[10.5px] leading-4 text-ink-400">
        {query ? `Matching "${query}" · ` : ""}
        <span class="font-mono">↑↓</span> to move, <span class="font-mono">Enter</span> to pick,
        <span class="font-mono"> Shift+Enter</span> runs a playbook straight away,
        <span class="font-mono"> Esc</span> to dismiss.
      </div>
    </div>
  );
}

function GroupIcon({ group }: { group: SlashGroup }) {
  const shared = "h-3 w-3 flex-none self-center text-ink-400";
  if (group === "playbook") return <Zap class={shared} aria-hidden="true" />;
  if (group === "skill") return <Code class={shared} aria-hidden="true" />;
  return <TestTube class={shared} aria-hidden="true" />;
}
