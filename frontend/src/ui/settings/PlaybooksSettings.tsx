import { useState } from "preact/hooks";
import { CHAT_MODE_OPTIONS, CHAT_PROVIDER_OPTIONS } from "../../config/chatCatalog";
import type { ChatMode, ChatProvider } from "../../models/chat";
import type { Playbook } from "../../models/playbook";
import type { RegisteredSkill } from "../../models/skill";
import type { PlaybookLibraryEditor } from "../../state/hooks/settings/usePlaybookLibrary";
import { useAvailableSkills } from "../../state/hooks/chat/useAvailableSkills";
import {
  hasPlaybookSkill,
  movePlaybook,
  newPlaybook,
  removePlaybook,
  togglePlaybookSkill,
  unknownPlaybookSkills,
  updatePlaybook,
} from "../../state/settings/playbookLibraryState";
import { AlertCircle, ArrowDown, ArrowUp, Check, Loader, Plus, Trash, Zap } from "../primitives/icons";

/**
 * Admin editor for the composer's playbook library.
 *
 * The whole library is one document server-side, so this edits a draft and
 * saves it in one request. The skill picker lists the same catalog the
 * composer shows; a playbook may still reference a skill this server has not
 * published, which is flagged rather than blocked — the reference stays valid
 * the moment the skill is installed.
 */
export function PlaybooksSettings({ editor }: { editor: PlaybookLibraryEditor }) {
  // Skills are provider-scoped in the catalog. Claude's list is the superset
  // an operator publishes globally, so it is what the picker offers.
  const { skills, loading: skillsLoading } = useAvailableSkills("claude");

  function addPlaybook() {
    editor.setDraft([...editor.draft, newPlaybook(editor.draft)]);
  }

  return (
    <div class="space-y-4">
      <div class="rounded-lg border border-white/10 bg-[#101318] p-4">
        <div class="flex items-start gap-2">
          <Zap class="mt-0.5 h-4 w-4 flex-none text-accent-blue" aria-hidden="true" />
          <div class="min-w-0 text-[12.5px] leading-relaxed text-ink-300">
            Playbooks appear behind the ⚡ button in every chat composer. Clicking one applies its
            skills, mode, and provider, then loads its prompt into the composer.
            <div class="mt-1.5">
              Prompts may use{" "}
              <code class="rounded bg-white/[0.07] px-1 font-mono text-[11px] text-ink-100">
                {"{{project}}"}
              </code>
              ,{" "}
              <code class="rounded bg-white/[0.07] px-1 font-mono text-[11px] text-ink-100">
                {"{{slug}}"}
              </code>
              , and{" "}
              <code class="rounded bg-white/[0.07] px-1 font-mono text-[11px] text-ink-100">
                {"{{previewUrl}}"}
              </code>
              . Any other placeholder — such as{" "}
              <code class="rounded bg-white/[0.07] px-1 font-mono text-[11px] text-ink-100">
                {"{{askUrl}}"}
              </code>{" "}
              — is left in the composer for the user to fill in, and such a prompt is never sent
              automatically.
            </div>
          </div>
        </div>
      </div>

      {editor.error && (
        <div class="flex items-start gap-2 rounded-lg border border-accent-red/30 bg-accent-red/[0.08] px-3 py-2.5 text-[12.5px] text-accent-red">
          <AlertCircle class="mt-0.5 h-4 w-4 flex-none" />
          <span class="min-w-0 break-words">{editor.error}</span>
        </div>
      )}

      {editor.loading ? (
        <div class="rounded-lg border border-white/10 bg-[#101318] px-4 py-6 text-[13px] text-ink-300">
          Loading playbooks…
        </div>
      ) : (
        <div class="space-y-3">
          {editor.draft.length === 0 && (
            <div class="rounded-lg border border-white/10 bg-[#101318] px-4 py-6 text-[13px] text-ink-300">
              The library is empty. Add a playbook to give every chat a one-click prompt.
            </div>
          )}
          {editor.draft.map((playbook, index) => (
            <PlaybookCard
              key={playbook.id}
              playbook={playbook}
              index={index}
              total={editor.draft.length}
              skills={skills}
              skillsLoading={skillsLoading}
              onChange={(patch) => editor.setDraft(updatePlaybook(editor.draft, playbook.id, patch))}
              onMove={(direction) => editor.setDraft(movePlaybook(editor.draft, playbook.id, direction))}
              onRemove={() => editor.setDraft(removePlaybook(editor.draft, playbook.id))}
              onToggleSkill={(skill) =>
                editor.setDraft(togglePlaybookSkill(editor.draft, playbook.id, skill))
              }
            />
          ))}
        </div>
      )}

      <div class="flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={addPlaybook}
          class="inline-flex h-8 items-center gap-1.5 rounded-md border border-white/10 bg-white/[0.05] px-3
                 text-[12.5px] font-medium text-ink-100 transition hover:bg-white/[0.09]"
        >
          <Plus class="h-3.5 w-3.5" />
          Add playbook
        </button>
        <button
          type="button"
          onClick={() => void editor.save()}
          disabled={editor.saving || !editor.dirty || editor.problem !== null}
          class="inline-flex h-8 items-center gap-1.5 rounded-md bg-accent-blue/80 px-3 text-[12.5px]
                 font-medium text-white transition hover:bg-accent-blue disabled:opacity-40"
        >
          {editor.saving ? <Loader class="h-3.5 w-3.5 animate-spin" /> : <Check class="h-3.5 w-3.5" />}
          {editor.saving ? "Saving…" : "Save library"}
        </button>
        <button
          type="button"
          onClick={editor.reset}
          disabled={editor.saving || !editor.dirty}
          class="inline-flex h-8 items-center rounded-md border border-white/10 bg-white/[0.05] px-3
                 text-[12.5px] font-medium text-ink-200 transition hover:bg-white/[0.09] disabled:opacity-40"
        >
          Discard changes
        </button>
        {editor.problem && (
          <span class="text-[12px] text-accent-red">{editor.problem}</span>
        )}
        {!editor.problem && editor.saved && !editor.dirty && (
          <span class="text-[12px] text-accent-green">Saved.</span>
        )}
      </div>
    </div>
  );
}

function PlaybookCard({
  playbook,
  index,
  total,
  skills,
  skillsLoading,
  onChange,
  onMove,
  onRemove,
  onToggleSkill,
}: {
  playbook: Playbook;
  index: number;
  total: number;
  skills: RegisteredSkill[];
  skillsLoading: boolean;
  onChange: (patch: Partial<Playbook>) => void;
  onMove: (direction: -1 | 1) => void;
  onRemove: () => void;
  onToggleSkill: (skill: RegisteredSkill) => void;
}) {
  const [skillsOpen, setSkillsOpen] = useState(false);
  const missing = unknownPlaybookSkills(playbook, skills);
  const selectedCount = playbook.skills?.length ?? 0;

  return (
    <div class="rounded-lg border border-white/10 bg-[#101318] p-3">
      <div class="flex items-center gap-2">
        <input
          value={playbook.icon ?? ""}
          onInput={(event) => onChange({ icon: (event.currentTarget as HTMLInputElement).value })}
          maxLength={8}
          aria-label="Emoji"
          placeholder="⚡"
          class="h-8 w-12 flex-none rounded-md border border-white/10 bg-[#0b0d11] px-2 text-center text-[15px] text-ink-50
                 focus:border-accent-blue/50 focus:outline-none"
        />
        <input
          value={playbook.title}
          onInput={(event) => onChange({ title: (event.currentTarget as HTMLInputElement).value })}
          aria-label="Title"
          placeholder="Title"
          class="h-8 min-w-0 flex-1 rounded-md border border-white/10 bg-[#0b0d11] px-2.5 text-[13px] text-ink-50
                 focus:border-accent-blue/50 focus:outline-none"
        />
        <button
          type="button"
          onClick={() => onMove(-1)}
          disabled={index === 0}
          aria-label="Move up"
          title="Move up"
          class="grid h-8 w-8 flex-none place-items-center rounded-md text-ink-300 hover:bg-white/[0.08] hover:text-ink-50 disabled:opacity-30"
        >
          <ArrowUp class="h-3.5 w-3.5" />
        </button>
        <button
          type="button"
          onClick={() => onMove(1)}
          disabled={index === total - 1}
          aria-label="Move down"
          title="Move down"
          class="grid h-8 w-8 flex-none place-items-center rounded-md text-ink-300 hover:bg-white/[0.08] hover:text-ink-50 disabled:opacity-30"
        >
          <ArrowDown class="h-3.5 w-3.5" />
        </button>
        <button
          type="button"
          onClick={onRemove}
          aria-label={`Delete ${playbook.title || "playbook"}`}
          title="Delete"
          class="grid h-8 w-8 flex-none place-items-center rounded-md text-ink-300 hover:bg-accent-red/[0.14] hover:text-accent-red"
        >
          <Trash class="h-3.5 w-3.5" />
        </button>
      </div>

      <input
        value={playbook.hint ?? ""}
        onInput={(event) => onChange({ hint: (event.currentTarget as HTMLInputElement).value })}
        aria-label="One-line hint"
        placeholder="One-line hint shown under the title in the composer menu"
        class="mt-2 h-8 w-full rounded-md border border-white/10 bg-[#0b0d11] px-2.5 text-[12.5px] text-ink-100
               placeholder:text-ink-500 focus:border-accent-blue/50 focus:outline-none"
      />

      <textarea
        value={playbook.prompt}
        onInput={(event) => onChange({ prompt: (event.currentTarget as HTMLTextAreaElement).value })}
        rows={6}
        aria-label="Prompt"
        placeholder="The prompt this playbook loads into the composer"
        class="mt-2 w-full resize-y rounded-md border border-white/10 bg-[#0b0d11] px-2.5 py-2 font-mono text-[12px]
               leading-5 text-ink-100 placeholder:text-ink-500 focus:border-accent-blue/50 focus:outline-none"
      />

      <div class="mt-2 flex flex-wrap items-center gap-2">
        <label class="flex items-center gap-1.5 text-[11.5px] text-ink-300">
          Mode
          <select
            value={playbook.mode ?? ""}
            onChange={(event) =>
              onChange({ mode: (event.currentTarget as HTMLSelectElement).value as ChatMode | "" })
            }
            class="h-7 rounded-md border border-white/10 bg-[#0b0d11] px-1.5 text-[11.5px] text-ink-100 focus:outline-none"
          >
            <option value="">Keep chat's mode</option>
            {CHAT_MODE_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </label>

        <label class="flex items-center gap-1.5 text-[11.5px] text-ink-300">
          Provider
          <select
            value={playbook.provider ?? ""}
            onChange={(event) =>
              onChange({
                provider: (event.currentTarget as HTMLSelectElement).value as ChatProvider | "",
              })
            }
            class="h-7 rounded-md border border-white/10 bg-[#0b0d11] px-1.5 text-[11.5px] text-ink-100 focus:outline-none"
          >
            <option value="">Keep chat's provider</option>
            {CHAT_PROVIDER_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </select>
        </label>

        <button
          type="button"
          onClick={() => setSkillsOpen((open) => !open)}
          class="inline-flex h-7 items-center gap-1.5 rounded-md border border-white/10 bg-white/[0.05] px-2.5
                 text-[11.5px] font-medium text-ink-200 transition hover:bg-white/[0.09]"
          aria-expanded={skillsOpen}
        >
          Skills
          <span class="rounded bg-white/10 px-1 py-0.5 text-[10px] leading-none text-ink-300">
            {selectedCount}
          </span>
        </button>
      </div>

      {selectedCount > 0 && (
        <div class="mt-2 flex flex-wrap gap-1.5">
          {(playbook.skills ?? []).map((skill) => (
            <span
              key={`${skill.source ?? ""}:${skill.command ?? skill.name}`}
              class="rounded border border-white/10 bg-white/[0.05] px-1.5 py-0.5 font-mono text-[10.5px] text-ink-200"
            >
              {skill.command || skill.name}
            </span>
          ))}
        </div>
      )}

      {missing.length > 0 && (
        <div class="mt-2 flex items-start gap-1.5 text-[11px] text-amber-300">
          <AlertCircle class="mt-0.5 h-3 w-3 flex-none" />
          <span class="min-w-0 break-words">
            Not published on this server yet: {missing.join(", ")}. The playbook still works once
            the skill is installed.
          </span>
        </div>
      )}

      {skillsOpen && (
        <div class="mt-2 max-h-56 overflow-y-auto rounded-md border border-white/10 bg-[#0b0d11] p-1">
          {skillsLoading ? (
            <div class="px-2 py-2 text-[12px] text-ink-400">Loading skills…</div>
          ) : skills.length === 0 ? (
            <div class="px-2 py-2 text-[12px] text-ink-400">No skills registered yet.</div>
          ) : (
            skills.map((skill) => {
              const selected = hasPlaybookSkill(playbook, skill);
              return (
                <button
                  key={`${skill.source ?? "skill"}:${skill.command || skill.name}`}
                  type="button"
                  onClick={() => onToggleSkill(skill)}
                  class={`flex w-full items-center gap-2 rounded px-2 py-1.5 text-left text-[12px] transition
                          ${selected ? "bg-accent-blue/[0.14] text-accent-blue" : "text-ink-200 hover:bg-white/[0.07]"}`}
                >
                  <span
                    class={`grid h-3.5 w-3.5 flex-none place-items-center rounded-sm border
                            ${selected ? "border-accent-blue bg-accent-blue/30" : "border-white/20"}`}
                    aria-hidden="true"
                  >
                    {selected && <Check class="h-2.5 w-2.5" />}
                  </span>
                  <span class="min-w-0 flex-1 truncate font-mono">{skill.command || skill.name}</span>
                  {skill.source && (
                    <span class="flex-none rounded bg-white/[0.08] px-1 py-0.5 text-[10px] uppercase text-ink-400">
                      {skill.source}
                    </span>
                  )}
                </button>
              );
            })
          )}
        </div>
      )}
    </div>
  );
}
