import { useEffect, useState } from "preact/hooks";
import { directModelsApi } from "../../../api/directModelsApi";
import type { DirectModelChoice } from "../../../models/directModels";

/**
 * The completion-API models the composer may offer.
 *
 * Loaded once per session. The list changes only when an administrator enables
 * a provider or edits the local model, which is rare enough that a refresh is
 * a fair price and far cheaper than polling on every chat open.
 */
export function useDirectModelChoices(enabled: boolean): DirectModelChoice[] {
  const [choices, setChoices] = useState<DirectModelChoice[]>([]);

  useEffect(() => {
    if (!enabled) return;
    let cancelled = false;
    void directModelsApi
      .list()
      .then((list) => {
        if (!cancelled) setChoices(list);
      })
      .catch(() => {
        // A list that cannot be read shows as no direct models, which leaves
        // the composer exactly as it was before this section existed.
      });
    return () => {
      cancelled = true;
    };
  }, [enabled]);

  return choices;
}
