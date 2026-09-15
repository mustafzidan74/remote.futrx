import type { AppearanceTheme, LanguageChoice } from "../../models/settings";
import { activeLocale } from "../../i18n/translate";
import { Check, Globe, Loader, Monitor, Moon, Sun } from "../primitives/icons";

const options: Array<{
  theme: AppearanceTheme;
  label: string;
  Icon: typeof Monitor;
}> = [
  { theme: "system", label: "System", Icon: Monitor },
  { theme: "dark", label: "Dark", Icon: Moon },
  { theme: "light", label: "Light", Icon: Sun },
];

/**
 * Language names are written in their own language and marked so the
 * translation shim leaves them alone: someone who cannot read the current
 * interface still has to be able to find their way back to one they can.
 */
const languageOptions: Array<{
  language: LanguageChoice;
  label: string;
  lang?: string;
}> = [
  { language: "auto", label: "Auto" },
  { language: "en", label: "English", lang: "en" },
  { language: "ar", label: "العربية", lang: "ar" },
];

export function AppearanceSettings({
  theme,
  language,
  loading,
  saving,
  error,
  onThemeChange,
  onLanguageChange,
}: {
  theme: AppearanceTheme;
  language: LanguageChoice;
  loading: boolean;
  saving: boolean;
  error: string | null;
  onThemeChange: (theme: AppearanceTheme) => void;
  onLanguageChange: (language: LanguageChoice) => void;
}) {
  return (
    <section class="overflow-hidden rounded-card border border-line bg-surface">
      <header class="px-4 py-3 flex items-start gap-3 border-b border-line">
        <div class="mt-0.5 grid h-8 w-8 flex-none place-items-center rounded-control bg-tint text-ink-300">
          <Monitor class="h-4 w-4" />
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-[14.5px] font-semibold text-ink-50">Appearance</div>
          <div class="text-[12.5px] text-ink-300 mt-0.5 leading-snug">Theme and interface language</div>
        </div>
        {(loading || saving) && <Loader class="w-4 h-4 mt-2 text-ink-300 animate-spin" />}
      </header>

      <div class="p-3 space-y-3">
        <div
          class="segmented grid-cols-3"
          role="radiogroup"
          aria-label="Theme"
        >
          {options.map(({ theme: optionTheme, label, Icon }) => {
            const selected = optionTheme === theme;
            return (
              <button
                key={optionTheme}
                type="button"
                disabled={loading || saving}
                onClick={() => onThemeChange(optionTheme)}
                class="segmented-option disabled:cursor-wait"
                aria-checked={selected}
                role="radio"
              >
                <Icon class="w-4 h-4" />
                <span class="truncate">{label}</span>
              </button>
            );
          })}
        </div>

        <div class="space-y-1.5">
          <div class="flex items-center gap-1.5 text-[12px] font-medium text-ink-300">
            <Globe class="h-3.5 w-3.5" />
            <span>Interface language</span>
            {/* A pointer for Arabic readers who land on another language; redundant in Arabic itself. */}
            {activeLocale() !== "ar" && (
              <span translate={false} class="text-ink-400">
                · لغة الواجهة
              </span>
            )}
          </div>
          <div
            class="segmented grid-cols-3"
            role="radiogroup"
            aria-label="Interface language"
          >
            {languageOptions.map(({ language: optionLanguage, label, lang }) => {
              const selected = optionLanguage === language;
              return (
                <button
                  key={optionLanguage}
                  type="button"
                  disabled={loading || saving}
                  onClick={() => onLanguageChange(optionLanguage)}
                  class="segmented-option disabled:cursor-wait"
                  aria-checked={selected}
                  role="radio"
                >
                  {lang ? (
                    <span class="truncate" lang={lang} translate={false}>{label}</span>
                  ) : (
                    <span class="truncate">{label}</span>
                  )}
                </button>
              );
            })}
          </div>
        </div>

        <div class="min-h-5 text-[12px]">
          {error ? (
            <span class="text-accent-red">{error}</span>
          ) : saving ? (
            <span class="text-ink-300">Saving</span>
          ) : (
            <span class="inline-flex items-center gap-1 text-accent-green">
              <Check class="w-3.5 h-3.5" /> Saved
            </span>
          )}
        </div>
      </div>
    </section>
  );
}
