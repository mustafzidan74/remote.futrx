import { useCallback, useEffect, useState } from "preact/hooks";
import { modelRoutingApi } from "../../../api/modelRoutingApi";
import type {
  RoutingDecision,
  RoutingPolicy,
  RoutingProviderModels,
  RoutingTestInput,
  RoutingView,
} from "../../../models/modelRouting";
import { routingPolicyProblem } from "../../settings/modelRoutingState";

export interface ModelRoutingEditor {
  /** The saved document, or null until the first load lands. */
  view: RoutingView | null;
  /** The document being edited. Never null once the view has loaded. */
  draft: RoutingPolicy | null;
  catalog: RoutingProviderModels[];
  providers: string[];
  loading: boolean;
  saving: boolean;
  error: string | null;
  /** Why the draft cannot be saved yet, or null. */
  problem: string | null;
  dirty: boolean;
  setDraft: (policy: RoutingPolicy) => void;
  revert: () => void;
  save: () => Promise<void>;
  refresh: () => Promise<void>;
  /** The "Test this policy" box. */
  tested: RoutingDecision | null;
  testing: boolean;
  testError: string | null;
  test: (input: RoutingTestInput) => Promise<void>;
}

/**
 * The admin-only routing policy editor.
 *
 * A draft is held locally rather than saved on every keystroke: rule order is
 * load-bearing, and an operator dragging rules around should not have the fleet
 * change model three times on the way. `dirty` is what tells them a save is
 * still owed.
 */
export function useModelRouting(enabled: boolean): ModelRoutingEditor {
  const [view, setView] = useState<RoutingView | null>(null);
  const [draft, setDraft] = useState<RoutingPolicy | null>(null);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [tested, setTested] = useState<RoutingDecision | null>(null);
  const [testing, setTesting] = useState(false);
  const [testError, setTestError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const next = await modelRoutingApi.fetch();
      setView(next);
      setDraft(next.policy);
      setError(null);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (!enabled) return;
    void refresh();
  }, [enabled, refresh]);

  const save = useCallback(async () => {
    if (!draft) return;
    setSaving(true);
    try {
      const next = await modelRoutingApi.save(draft);
      setView(next);
      setDraft(next.policy);
      setError(null);
    } catch (cause) {
      setError((cause as Error).message);
      throw cause;
    } finally {
      setSaving(false);
    }
  }, [draft]);

  const revert = useCallback(() => {
    if (view) setDraft(view.policy);
  }, [view]);

  const test = useCallback(async (input: RoutingTestInput) => {
    setTesting(true);
    try {
      setTested(await modelRoutingApi.test(input));
      setTestError(null);
    } catch (cause) {
      setTested(null);
      setTestError((cause as Error).message);
    } finally {
      setTesting(false);
    }
  }, []);

  return {
    view,
    draft,
    catalog: view?.catalog ?? [],
    providers: view?.providers ?? [],
    loading,
    saving,
    error,
    problem: draft ? routingPolicyProblem(draft) : null,
    // The saved document is the comparison, so reverting really does clear it.
    dirty: Boolean(draft && view && JSON.stringify(draft) !== JSON.stringify(view.policy)),
    setDraft,
    revert,
    save,
    refresh,
    tested,
    testing,
    testError,
    test,
  };
}
