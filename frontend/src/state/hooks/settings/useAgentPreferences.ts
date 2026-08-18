import { useCallback, useEffect, useState } from "preact/hooks";
import { agentPreferencesApi } from "../../../api/agentPreferencesApi";
import {
  DEFAULT_AGENT_PREFERENCES,
  type AgentPreferences,
} from "../../../models/agentPreferences";
import {
  preferencesDirty,
  preferencesProblem,
  preferencesRequest,
} from "../../settings/replyPreferencesState";

export interface AgentPreferencesEditor {
  draft: AgentPreferences;
  loading: boolean;
  saving: boolean;
  saved: boolean;
  dirty: boolean;
  error: string | null;
  /** Set when the draft cannot be submitted as it stands. */
  problem: string | null;
  setDraft: (patch: Partial<AgentPreferences>) => void;
  reset: () => void;
  save: () => Promise<void>;
}

/**
 * Admin-only editing of the platform reply preferences. The whole document is
 * one PUT, so the panel keeps a draft and submits it; `dirty` is what makes
 * Save and Discard meaningful.
 */
export function useAgentPreferences(enabled: boolean): AgentPreferencesEditor {
  const [stored, setStored] = useState<AgentPreferences>(DEFAULT_AGENT_PREFERENCES);
  const [draft, setDraftState] = useState<AgentPreferences>(DEFAULT_AGENT_PREFERENCES);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const prefs = await agentPreferencesApi.fetch();
      setStored(prefs);
      setDraftState(prefs);
      setError(null);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!enabled) return;
    void load();
  }, [enabled, load]);

  const setDraft = useCallback((patch: Partial<AgentPreferences>) => {
    setDraftState((current) => ({ ...current, ...patch }));
    setSaved(false);
  }, []);

  const reset = useCallback(() => {
    setDraftState(stored);
    setError(null);
    setSaved(false);
  }, [stored]);

  const save = useCallback(async () => {
    const problem = preferencesProblem(draft);
    if (problem) {
      setError(problem);
      return;
    }
    setSaving(true);
    setSaved(false);
    try {
      const prefs = await agentPreferencesApi.save(preferencesRequest(draft));
      setStored(prefs);
      setDraftState(prefs);
      setError(null);
      setSaved(true);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setSaving(false);
    }
  }, [draft]);

  return {
    draft,
    loading,
    saving,
    saved,
    dirty: preferencesDirty(draft, stored),
    error,
    problem: preferencesProblem(draft),
    setDraft,
    reset,
    save,
  };
}
