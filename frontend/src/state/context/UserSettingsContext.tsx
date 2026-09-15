import type { ComponentChildren } from "preact";
import { createContext } from "preact";
import { useCallback, useContext, useEffect, useMemo, useState } from "preact/hooks";
import { useAuthContext } from "./AuthContext";
import {
  type AppearanceTheme,
  type ChatSettings,
  type LanguageChoice,
  type UserSettings,
} from "../../models/settings";
import { localeStore } from "../../i18n/localeStore";
import { settingsApi } from "../../api/settingsApi";
import { DEFAULT_USER_SETTINGS } from "../../config/settings";
import { appearanceThemeState } from "./appearanceThemeState";

interface UserSettingsContextValue {
  settings: UserSettings;
  loading: boolean;
  saving: boolean;
  error: string | null;
  refresh: () => Promise<void>;
  setTheme: (theme: AppearanceTheme) => Promise<void>;
  setLanguage: (language: LanguageChoice) => Promise<void>;
  setChatSettings: (
    scope: "host" | "project",
    chat: Partial<ChatSettings>
  ) => Promise<void>;
  setReplyLanguage: (language: string) => Promise<void>;
}

const UserSettingsContext = createContext<UserSettingsContextValue | null>(null);

/**
 * The defaults, with appearance taken from what this browser last applied.
 *
 * The built-in default is "system", and using it before the server answers
 * repaints a light-theme user's app in dark for the length of the round-trip —
 * on every load, and again whenever the gate closes. Worse, applying it also
 * caches "system", so the next cold boot paints its very first frame wrong too.
 */
function settingsFromCachedAppearance(): UserSettings {
  return {
    ...DEFAULT_USER_SETTINGS,
    appearance: {
      ...DEFAULT_USER_SETTINGS.appearance,
      theme: appearanceThemeState.remembered(),
      language: localeStore.remembered(),
    },
  };
}

export function UserSettingsProvider({ children }: { children: ComponentChildren }) {
  ////////////////
  // Local State
  ////////////////
  const { gateOpen } = useAuthContext();
  const [settings, setSettings] = useState<UserSettings>(settingsFromCachedAppearance);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  ////////////////
  // Handlers
  ////////////////
  const refresh = useCallback(async () => {
    if (!gateOpen) {
      setSettings(settingsFromCachedAppearance());
      setLoading(false);
      setError(null);
      return;
    }

    setLoading(true);
    try {
      setSettings(await settingsApi.fetch());
      setError(null);
    } catch (e) {
      setError((e as Error).message);
    } finally {
      setLoading(false);
    }
  }, [gateOpen]);

  const setTheme = useCallback(async (theme: AppearanceTheme) => {
    const previous = settings;
    setSettings({ ...settings, appearance: { ...settings.appearance, theme } });
    setSaving(true);
    try {
      setSettings(await settingsApi.update({ appearance: { theme } }));
      setError(null);
    } catch (e) {
      setSettings(previous);
      setError((e as Error).message);
    } finally {
      setSaving(false);
    }
  }, [settings]);

  // Optimistic like the theme. Applying a different locale remounts the app
  // (see i18n/LocaleRoot), and the save is already on the wire by then.
  const setLanguage = useCallback(async (language: LanguageChoice) => {
    const previous = settings;
    setSettings({ ...settings, appearance: { ...settings.appearance, language } });
    setSaving(true);
    try {
      setSettings(await settingsApi.update({ appearance: { language } }));
      setError(null);
    } catch (e) {
      setSettings(previous);
      setError((e as Error).message);
    } finally {
      setSaving(false);
    }
  }, [settings]);

  // The personal override of the platform reply language. It is optimistic
  // like the theme: the field is a text input in its custom mode, so waiting
  // for the server on every keystroke would make it unusable.
  const setReplyLanguage = useCallback(async (language: string) => {
    const previous = settings;
    setSettings({ ...settings, agent: { ...settings.agent, replyLanguage: language } });
    setSaving(true);
    try {
      setSettings(await settingsApi.update({ agent: { replyLanguage: language } }));
      setError(null);
    } catch (e) {
      setSettings(previous);
      setError((e as Error).message);
    } finally {
      setSaving(false);
    }
  }, [settings]);

  const setChatSettings = useCallback(async (
    scope: "host" | "project",
    chat: Partial<ChatSettings>
  ) => {
    const previous = settings;
    const key = scope === "project" ? "projectChat" : "chat";
    setSettings({ ...settings, [key]: { ...settings[key], ...chat } });
    setSaving(true);
    try {
      setSettings(await settingsApi.update({ [key]: chat }));
      setError(null);
    } catch (e) {
      setSettings(previous);
      setError((e as Error).message);
    } finally {
      setSaving(false);
    }
  }, [settings]);

  ////////////////
  // Effects
  ////////////////
  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    appearanceThemeState.apply(settings.appearance.theme);
    return appearanceThemeState.observeSystemChanges(settings.appearance.theme);
  }, [settings.appearance.theme]);

  useEffect(() => {
    localeStore.setChoice(settings.appearance.language);
  }, [settings.appearance.language]);

  ////////////////
  // Context Value
  ////////////////
  const value = useMemo<UserSettingsContextValue>(() => ({
    settings,
    loading,
    saving,
    error,
    refresh,
    setTheme,
    setLanguage,
    setChatSettings,
    setReplyLanguage,
  }), [settings, loading, saving, error, refresh, setTheme, setLanguage, setChatSettings, setReplyLanguage]);

  return (
    <UserSettingsContext.Provider value={value}>
      {children}
    </UserSettingsContext.Provider>
  );
}

export function useUserSettingsContext(): UserSettingsContextValue {
  const value = useContext(UserSettingsContext);
  if (!value) throw new Error("useUserSettingsContext must be used inside UserSettingsProvider");
  return value;
}
