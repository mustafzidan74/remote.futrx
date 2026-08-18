import { useState } from "preact/hooks";
import type { AgentPreferencesEditor } from "../../state/hooks/settings/useAgentPreferences";
import {
  APPLY_TO_OPTIONS,
  CUSTOM_LANGUAGE_VALUE,
  REPLY_LANGUAGE_OPTIONS,
  REPLY_TONE_OPTIONS,
  isCustomLanguage,
  languageSelectValue,
  preferencesSummary,
} from "../../state/settings/replyPreferencesState";
import { MAX_EXTRA_INSTRUCTIONS_LENGTH } from "../../models/agentPreferences";
import { Check, Globe, Loader } from "../primitives/icons";

/**
 * The admin panel for platform-wide agent reply preferences.
 *
 * The preview line is the point of the page: an admin should be able to read
 * exactly what every agent will be told without opening a container.
 */
export function ReplyPreferencesSettings({ editor }: { editor: AgentPreferencesEditor }) {
  const { draft } = editor;
  const storedIsCustom = isCustomLanguage(draft.replyLanguage);
  // "Custom" with an empty label is a state the stored value cannot express —
  // it normalizes to "auto" — so the picker keeps its own selection. Without
  // this, choosing Custom… snaps the select straight back to "Match the user"
  // before the operator can type anything.
  const [customSelected, setCustomSelected] = useState(false);
  const showCustom = storedIsCustom || customSelected;
  const selectValue = showCustom
    ? CUSTOM_LANGUAGE_VALUE
    : languageSelectValue(draft.replyLanguage);

  return (
    <section class="rounded-lg border border-white/10 bg-[#101318] overflow-hidden">
      <header class="px-4 py-3 flex items-start gap-3 border-b border-white/[0.06]">
        <div class="mt-0.5 w-9 h-9 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none">
          <Globe class="w-4 h-4 text-ink-200" />
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-[14.5px] font-semibold text-ink-50">Reply preferences</div>
          <div class="text-[12.5px] text-ink-300 mt-0.5 leading-snug">
            Injected into every project's <code class="text-ink-200">/workspace/AGENTS.md</code> and
            into every prompt, so it applies to all four agent CLIs.
          </div>
        </div>
        {(editor.loading || editor.saving) && (
          <Loader class="w-4 h-4 mt-2 text-ink-300 animate-spin" />
        )}
      </header>

      <div class="p-4 space-y-5">
        <label class="block">
          <span class="block text-[12.5px] font-medium text-ink-100">Reply language</span>
          <select
            value={selectValue}
            disabled={editor.loading}
            onChange={(event) => {
              const next = (event.currentTarget as HTMLSelectElement).value;
              // "custom" is not a language, so it seeds an empty label and is
              // remembered locally rather than stored.
              setCustomSelected(next === CUSTOM_LANGUAGE_VALUE);
              editor.setDraft({
                replyLanguage: next === CUSTOM_LANGUAGE_VALUE ? "" : next,
              });
            }}
            class="mt-1.5 w-full max-w-md h-10 rounded-md bg-[#0b0d11] border border-white/10 px-3
                   text-[13.5px] text-ink-100 focus:outline-none focus:border-accent-blue/70"
          >
            {REPLY_LANGUAGE_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
                {option.hint ? ` — ${option.hint}` : ""}
              </option>
            ))}
            <option value={CUSTOM_LANGUAGE_VALUE}>Custom…</option>
          </select>
        </label>

        {showCustom && (
          <label class="block">
            <span class="block text-[12.5px] font-medium text-ink-100">Custom language</span>
            <input
              value={storedIsCustom ? draft.replyLanguage : ""}
              placeholder="Levantine Arabic"
              onInput={(event) =>
                editor.setDraft({
                  replyLanguage: (event.currentTarget as HTMLInputElement).value,
                })
              }
              class="mt-1.5 w-full max-w-md h-10 rounded-md bg-[#0b0d11] border border-white/10 px-3
                     text-[13.5px] text-ink-100 placeholder:text-ink-400
                     focus:outline-none focus:border-accent-blue/70"
            />
            <span class="mt-1 block text-[11.5px] text-ink-400">
              Written into the instruction verbatim, so name it the way you would to a person.
            </span>
          </label>
        )}

        <fieldset>
          <legend class="text-[12.5px] font-medium text-ink-100">Tone</legend>
          <div class="mt-1.5 grid gap-1 sm:grid-cols-3 max-w-2xl">
            {REPLY_TONE_OPTIONS.map((option) => {
              const selected = draft.tone === option.value;
              return (
                <button
                  key={option.value}
                  type="button"
                  onClick={() => editor.setDraft({ tone: option.value })}
                  aria-pressed={selected}
                  class={`text-left rounded-md border px-3 py-2 transition ${
                    selected
                      ? "border-accent-blue/60 bg-accent-blue/10"
                      : "border-white/10 bg-white/[0.03] hover:bg-white/[0.06]"
                  }`}
                >
                  <span class="block text-[13px] text-ink-50">{option.label}</span>
                  <span class="block text-[11.5px] text-ink-400 leading-snug">{option.hint}</span>
                </button>
              );
            })}
          </div>
        </fieldset>

        <label class="block">
          <span class="flex items-baseline justify-between gap-2">
            <span class="text-[12.5px] font-medium text-ink-100">Extra instructions</span>
            <span class="text-[11.5px] text-ink-400">
              {draft.extraInstructions.length} / {MAX_EXTRA_INSTRUCTIONS_LENGTH}
            </span>
          </span>
          <textarea
            value={draft.extraInstructions}
            rows={5}
            placeholder="House rules every agent should follow, e.g. never force-push, always run the test suite before reporting done."
            onInput={(event) =>
              editor.setDraft({
                extraInstructions: (event.currentTarget as HTMLTextAreaElement).value,
              })
            }
            class="mt-1.5 w-full rounded-md bg-[#0b0d11] border border-white/10 px-3 py-2
                   text-[13.5px] text-ink-100 placeholder:text-ink-400 leading-relaxed
                   focus:outline-none focus:border-accent-blue/70"
          />
          <span class="mt-1 block text-[11.5px] text-ink-400">
            The full text goes into the workspace file; a shortened copy rides on every prompt, so
            keep it brief.
          </span>
        </label>

        <fieldset>
          <legend class="text-[12.5px] font-medium text-ink-100">Apply to</legend>
          <div class="mt-1.5 grid gap-1 sm:grid-cols-2 max-w-2xl">
            {APPLY_TO_OPTIONS.map((option) => {
              const selected = draft.applyTo === option.value;
              return (
                <button
                  key={option.value}
                  type="button"
                  onClick={() => editor.setDraft({ applyTo: option.value })}
                  aria-pressed={selected}
                  class={`text-left rounded-md border px-3 py-2 transition ${
                    selected
                      ? "border-accent-blue/60 bg-accent-blue/10"
                      : "border-white/10 bg-white/[0.03] hover:bg-white/[0.06]"
                  }`}
                >
                  <span class="block text-[13px] text-ink-50">{option.label}</span>
                  <span class="block text-[11.5px] text-ink-400 leading-snug">{option.hint}</span>
                </button>
              );
            })}
          </div>
        </fieldset>

        <div class="rounded-md border border-white/10 bg-white/[0.03] px-3 py-2">
          <div class="text-[11px] uppercase tracking-wider text-ink-400 font-semibold">
            What agents are told
          </div>
          <p class="mt-1 text-[12.5px] text-ink-200 leading-relaxed">
            {preferencesSummary(draft)}
          </p>
          {draft.extraInstructions.trim() && (
            <p class="mt-2 whitespace-pre-wrap text-[12.5px] text-ink-300 leading-relaxed">
              {draft.extraInstructions.trim()}
            </p>
          )}
        </div>

        <div class="flex flex-wrap items-center gap-2">
          <button
            type="button"
            disabled={!editor.dirty || editor.saving || !!editor.problem}
            onClick={() => void editor.save()}
            class="h-9 px-3 rounded-md bg-accent-blue text-ink-900 text-[13px] font-medium
                   hover:bg-accent-blue/85 disabled:opacity-40 disabled:cursor-not-allowed transition"
          >
            {editor.saving ? "Saving…" : "Save preferences"}
          </button>
          <button
            type="button"
            disabled={!editor.dirty || editor.saving}
            onClick={() => {
              setCustomSelected(false);
              editor.reset();
            }}
            class="h-9 px-3 rounded-md border border-white/10 text-ink-200 text-[13px]
                   hover:bg-white/[0.06] disabled:opacity-40 disabled:cursor-not-allowed transition"
          >
            Discard
          </button>
          <span class="text-[12px]">
            {editor.error ? (
              <span class="text-accent-red">{editor.error}</span>
            ) : editor.problem ? (
              <span class="text-accent-red">{editor.problem}</span>
            ) : editor.saved ? (
              <span class="inline-flex items-center gap-1 text-accent-green">
                <Check class="w-3.5 h-3.5" /> Saved — applied on the next agent run
              </span>
            ) : null}
          </span>
        </div>

        {draft.updatedBy && (
          <div class="text-[11.5px] text-ink-400">
            Last changed by {draft.updatedBy}
            {draft.updatedAt ? ` on ${new Date(draft.updatedAt).toLocaleString()}` : ""}.
          </div>
        )}
      </div>
    </section>
  );
}
