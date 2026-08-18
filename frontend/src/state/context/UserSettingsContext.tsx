import type { ComponentChildren } from "preact";
import { createContext } from "preact";
import { useCallback, useContext, useEffect, useMemo, useState } from "preact/hooks";
import { useAuthContext } from "./AuthContext";
import {
  type AppearanceTheme,
  type ChatSettings,
  type UserSettings,
} from "../../models/settings";
import { settingsApi } from "../../api/settingsApi";
import { DEFAULT_USER_SETTINGS } from "../../config/settings";
import { appearanceThemeState } from "../settings/appearanceThemeState";

interface UserSettingsContextValue {
  settings: UserSettings;
  loading: boolean;
  saving: boolean;
  error: string | null;
  refresh: () => Promise<void>;
  setTheme: (theme: AppearanceTheme) => Promise<void>;
  setChatSettings: (chat: Partial<ChatSettings>) => Promise<void>;
  setReplyLanguage: (language: string) => Promise<void>;
}

const UserSettingsContext = createContext<UserSettingsContextValue | null>(null);

export function UserSettingsProvider({ children }: { children: ComponentChildren }) {
  const { gateOpen } = useAuthContext();
  const [settings, setSettings] = useState<UserSettings>(DEFAULT_USER_SETTINGS);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!gateOpen) {
      setSettings(DEFAULT_USER_SETTINGS);
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

  useEffect(() => {
    void refresh();
  }, [refresh]);

  useEffect(() => {
    appearanceThemeState.apply(settings.appearance.theme);
    return appearanceThemeState.observeSystemChanges(settings.appearance.theme);
  }, [settings.appearance.theme]);

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

  const setChatSettings = useCallback(async (chat: Partial<ChatSettings>) => {
    const previous = settings;
    setSettings({ ...settings, chat: { ...settings.chat, ...chat } });
    setSaving(true);
    try {
      setSettings(await settingsApi.update({ chat }));
      setError(null);
    } catch (e) {
      setSettings(previous);
      setError((e as Error).message);
    } finally {
      setSaving(false);
    }
  }, [settings]);

  const value = useMemo<UserSettingsContextValue>(() => ({
    settings,
    loading,
    saving,
    error,
    refresh,
    setTheme,
    setChatSettings,
    setReplyLanguage,
  }), [settings, loading, saving, error, refresh, setTheme, setChatSettings, setReplyLanguage]);

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
