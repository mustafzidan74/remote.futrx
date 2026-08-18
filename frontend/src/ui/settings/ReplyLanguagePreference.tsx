import { useState } from "preact/hooks";
import {
  CUSTOM_LANGUAGE_VALUE,
  REPLY_LANGUAGE_OPTIONS,
  isCustomLanguage,
} from "../../state/settings/replyPreferencesState";
import { Globe } from "../primitives/icons";

/** The extra option only the personal picker has: defer to the platform. */
const FOLLOW_PLATFORM_VALUE = "";

/**
 * A user's personal reply-language override.
 *
 * It sits in Appearance because it is the one agent preference that is
 * genuinely per-person; tone and house rules are platform policy an individual
 * cannot opt out of. An empty value means "follow whatever the admin set",
 * which is deliberately distinct from "auto" — that one is a real choice
 * meaning "mirror my own language" and overrides the platform.
 */
export function ReplyLanguagePreference({
  language,
  disabled,
  onChange,
}: {
  language: string;
  disabled: boolean;
  onChange: (language: string) => void;
}) {
  const custom = isCustomLanguage(language);
  // Keeping the picker's own selection lets the custom field stay open while
  // it is still empty, which a value-derived selection could not do.
  const [showCustom, setShowCustom] = useState(custom);
  const selectValue = custom || showCustom ? CUSTOM_LANGUAGE_VALUE : language;

  return (
    <section class="rounded-lg border border-white/10 bg-[#101318] overflow-hidden">
      <header class="px-4 py-3 flex items-start gap-3 border-b border-white/[0.06]">
        <div class="mt-0.5 w-9 h-9 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none">
          <Globe class="w-4 h-4 text-ink-200" />
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-[14.5px] font-semibold text-ink-50">Agent reply language</div>
          <div class="text-[12.5px] text-ink-300 mt-0.5 leading-snug">
            Applies to the chats you run. Overrides the server-wide setting.
          </div>
        </div>
      </header>

      <div class="p-3 space-y-3">
        <select
          value={selectValue}
          disabled={disabled}
          onChange={(event) => {
            const next = (event.currentTarget as HTMLSelectElement).value;
            if (next === CUSTOM_LANGUAGE_VALUE) {
              setShowCustom(true);
              onChange("");
              return;
            }
            setShowCustom(false);
            onChange(next);
          }}
          class="w-full h-10 rounded-md bg-[#0b0d11] border border-white/10 px-3
                 text-[13.5px] text-ink-100 focus:outline-none focus:border-accent-blue/70
                 disabled:cursor-wait"
        >
          <option value={FOLLOW_PLATFORM_VALUE}>Follow the server setting</option>
          {REPLY_LANGUAGE_OPTIONS.map((option) => (
            <option key={option.value} value={option.value}>
              {option.label}
              {option.hint ? ` — ${option.hint}` : ""}
            </option>
          ))}
          <option value={CUSTOM_LANGUAGE_VALUE}>Custom…</option>
        </select>

        {(custom || showCustom) && (
          <input
            value={custom ? language : ""}
            placeholder="Levantine Arabic"
            disabled={disabled}
            onInput={(event) => onChange((event.currentTarget as HTMLInputElement).value)}
            class="w-full h-10 rounded-md bg-[#0b0d11] border border-white/10 px-3
                   text-[13.5px] text-ink-100 placeholder:text-ink-400
                   focus:outline-none focus:border-accent-blue/70"
          />
        )}

        <p class="text-[11.5px] text-ink-400 leading-relaxed">
          Code, identifiers, commands, and file paths always stay in English.
        </p>
      </div>
    </section>
  );
}
