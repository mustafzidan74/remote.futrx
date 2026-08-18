import { useEffect, useMemo, useRef, useState } from "preact/hooks";
import type { ChatProvider } from "../../../models/chat";
import type { RegisteredSkill } from "../../../models/skill";
import { useAvailableSkills } from "../../../state/hooks/chat/useAvailableSkills";
import { globalSkillsState } from "../../../state/settings/globalSkillsState";
import { ChevronDown, Code, Search } from "../../primitives/icons";

export function SkillPicker({
  provider,
  projectId,
  selectedCount,
  onSelect,
}: {
  provider: ChatProvider;
  projectId?: string;
  selectedCount: number;
  onSelect: (skill: RegisteredSkill) => void;
}) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const { skills, loading, error } = useAvailableSkills(provider, projectId);
  const rootRef = useRef<HTMLDivElement>(null);
  const searchRef = useRef<HTMLInputElement>(null);

  useEffect(() => {
    setOpen(false);
    setQuery("");
  }, [provider, projectId]);

  useEffect(() => {
    if (!open) return;
    const id = window.setTimeout(() => searchRef.current?.focus(), 0);
    function closeOnOutsideClick(event: MouseEvent) {
      const target = event.target as Node | null;
      if (target && !rootRef.current?.contains(target)) setOpen(false);
    }
    window.addEventListener("mousedown", closeOnOutsideClick);
    return () => {
      window.clearTimeout(id);
      window.removeEventListener("mousedown", closeOnOutsideClick);
    };
  }, [open]);

  const filteredSkills = useMemo(() => {
    const term = query.trim().toLowerCase();
    if (!term) return skills;
    return skills.filter((skill) =>
      `${skill.name} ${skill.description || ""} ${skill.source || ""}`.toLowerCase().includes(term)
    );
  }, [query, skills]);

  function choose(skill: RegisteredSkill) {
    if (!globalSkillsState.isSelectable(skill)) return;
    onSelect(skill);
    setOpen(false);
    setQuery("");
  }

  const providerLabel = provider === "codex" ? "Codex" : "Claude";

  return (
    <div ref={rootRef} class="codex-skill-control-root relative flex-none">
      <button
        type="button"
        onClick={() => setOpen((value) => !value)}
        class={`codex-skill-control composer-pill ${open ? "composer-pill-active" : ""}`}
        aria-haspopup="listbox"
        aria-expanded={open}
        title={`${providerLabel} skills`}
      >
        <Code class="h-4 w-4 flex-none text-ink-400" aria-hidden="true" />
        <span class="truncate font-semibold text-ink-100">Skills</span>
        <span class="rounded bg-white/10 px-1 py-0.5 text-[10px] leading-none text-ink-300">
          {selectedCount > 0 ? selectedCount : loading ? "..." : skills.length}
        </span>
        <ChevronDown class="h-4 w-4 flex-none text-ink-400" aria-hidden="true" />
      </button>

      {open && (
        <div
          class="popover-surface theme-menu-surface absolute left-0 bottom-full z-40 mb-2
                 w-[calc(100vw-1.5rem)] sm:left-auto sm:right-0 sm:w-[460px]"
        >
          <div class="p-2 border-b border-white/10 bg-[#191a1f]">
            <div class="h-9 rounded-md bg-[#0b0d11] border border-white/10 px-2.5 flex items-center gap-2">
              <Search class="w-3.5 h-3.5 text-ink-400 flex-none" />
              <input
                ref={searchRef}
                value={query}
                onInput={(event) => setQuery((event.currentTarget as HTMLInputElement).value)}
                class="min-w-0 flex-1 bg-transparent text-[13px] text-ink-100 placeholder:text-ink-400 focus:outline-none"
                placeholder={`Search ${providerLabel} skills`}
              />
            </div>
          </div>

          <div class="max-h-[300px] overflow-y-auto py-1">
            {error ? (
              <div class="px-3 py-3 text-[12px] text-red-300">{error}</div>
            ) : loading ? (
              <div class="px-3 py-3 text-[12px] text-ink-400">Loading skills...</div>
            ) : filteredSkills.length === 0 ? (
              <div class="px-3 py-3 text-[12px] text-ink-400">
                {skills.length === 0 ? `No ${providerLabel} skills registered` : "No matching skills"}
              </div>
            ) : (
              filteredSkills.map((skill) => {
                const selectable = globalSkillsState.isSelectable(skill);
                const badge = globalSkillsState.badgeFor(skill);
                const isGlobal = skill.scope === "global";
                return (
                  <button
                    key={`${skill.source || "skill"}:${skill.name}`}
                    type="button"
                    onClick={() => choose(skill)}
                    disabled={!selectable}
                    title={
                      selectable
                        ? undefined
                        : `A project skill named "${skill.command || skill.name}" shadows this global skill`
                    }
                    class={`w-full text-left px-3 py-2.5 focus:outline-none ${
                      selectable
                        ? "hover:bg-white/[0.07] focus:bg-white/[0.07]"
                        : "cursor-not-allowed opacity-50"
                    }`}
                    role="option"
                  >
                    <div class="flex items-center gap-2 min-w-0">
                      <span class="truncate text-[13px] font-medium text-ink-100">{skill.name}</span>
                      {badge && (
                        <span
                          class={`flex-none rounded px-1.5 py-0.5 text-[10px] uppercase ${
                            isGlobal && selectable
                              ? "bg-accent-blue/[0.16] text-accent-blue"
                              : "bg-white/[0.08] text-ink-400"
                          }`}
                        >
                          {badge}
                        </span>
                      )}
                    </div>
                    {skill.description && (
                      <div class="mt-1 max-h-8 overflow-hidden text-[12px] leading-4 text-ink-400">
                        {skill.description}
                      </div>
                    )}
                  </button>
                );
              })
            )}
          </div>
        </div>
      )}
    </div>
  );
}
