import { useCallback, useEffect, useState } from "preact/hooks";
import { playbookApi } from "../../../api/playbookApi";
import type { Playbook } from "../../../models/playbook";
import { sortPlaybooks } from "../../chat/playbookState";
import {
  playbookLibraryProblem,
  playbookLibraryRequest,
} from "../../settings/playbookLibraryState";

export interface PlaybookLibraryEditor {
  draft: Playbook[];
  loading: boolean;
  saving: boolean;
  error: string | null;
  saved: boolean;
  dirty: boolean;
  /** Set when the draft cannot be submitted as it stands. */
  problem: string | null;
  setDraft: (next: Playbook[]) => void;
  reset: () => void;
  save: () => Promise<void>;
}

/**
 * Admin-only playbook editing. The whole library is one document, so the page
 * keeps a draft and submits it in one PUT; `dirty` is what makes Save and
 * Discard meaningful.
 */
export function usePlaybookLibrary(enabled: boolean): PlaybookLibraryEditor {
  const [stored, setStored] = useState<Playbook[]>([]);
  const [draft, setDraft] = useState<Playbook[]>([]);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [saved, setSaved] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const list = sortPlaybooks(await playbookApi.list());
      setStored(list);
      setDraft(list);
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

  const save = useCallback(async () => {
    const request = playbookLibraryRequest(draft);
    const problem = playbookLibraryProblem(request);
    if (problem) {
      setError(problem);
      return;
    }
    setSaving(true);
    setSaved(false);
    try {
      const list = sortPlaybooks(await playbookApi.saveAll(request));
      setStored(list);
      setDraft(list);
      setError(null);
      setSaved(true);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setSaving(false);
    }
  }, [draft]);

  const applyDraft = useCallback((next: Playbook[]) => {
    setDraft(next);
    setSaved(false);
  }, []);

  const reset = useCallback(() => {
    setDraft(stored);
    setError(null);
    setSaved(false);
  }, [stored]);

  return {
    draft,
    loading,
    saving,
    error,
    saved,
    dirty: JSON.stringify(draft) !== JSON.stringify(stored),
    problem: playbookLibraryProblem(draft),
    setDraft: applyDraft,
    reset,
    save,
  };
}
