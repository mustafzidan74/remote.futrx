import type { AppearanceTheme } from "../../models/settings";
import { Check, Loader, Monitor, Moon, Sun } from "../primitives/icons";

const options: Array<{
  theme: AppearanceTheme;
  label: string;
  Icon: typeof Monitor;
}> = [
  { theme: "system", label: "System", Icon: Monitor },
  { theme: "dark", label: "Dark", Icon: Moon },
  { theme: "light", label: "Light", Icon: Sun },
];

export function AppearanceSettings({
  theme,
  loading,
  saving,
  error,
  onThemeChange,
}: {
  theme: AppearanceTheme;
  loading: boolean;
  saving: boolean;
  error: string | null;
  onThemeChange: (theme: AppearanceTheme) => void;
}) {
  return (
    <section class="rounded-lg border border-white/10 bg-[#101318] overflow-hidden">
      <header class="px-4 py-3 flex items-start gap-3 border-b border-white/[0.06]">
        <div class="mt-0.5 w-9 h-9 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none">
          <Monitor class="w-4 h-4 text-ink-200" />
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-[14.5px] font-semibold text-ink-50">Appearance</div>
          <div class="text-[12.5px] text-ink-300 mt-0.5 leading-snug">Theme preference</div>
        </div>
        {(loading || saving) && <Loader class="w-4 h-4 mt-2 text-ink-300 animate-spin" />}
      </header>

      <div class="p-3 space-y-3">
        <div
          class="grid grid-cols-3 gap-1 rounded-lg bg-white/[0.05] border border-white/10 p-1"
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
                class={`h-10 rounded-md inline-flex items-center justify-center gap-2 text-sm transition
                        disabled:cursor-wait ${
                          selected
                            ? "bg-accent-blue text-ink-900 shadow-sm"
                            : "text-ink-200 hover:text-ink-50 hover:bg-white/[0.07]"
                        }`}
                aria-checked={selected}
                role="radio"
              >
                <Icon class="w-4 h-4" />
                <span class="truncate">{label}</span>
              </button>
            );
          })}
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
