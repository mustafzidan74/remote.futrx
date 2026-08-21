import { useCallback, useEffect, useState } from "preact/hooks";
import { antigravityAuthApi } from "../../../api/agents/auth/antigravityAuthApi";

/**
 * Antigravity's status is polled rather than subscribed.
 *
 * The other agents push status over a websocket because the platform runs
 * their login and knows the moment it completes. Antigravity's sign-in happens
 * in a terminal the platform does not watch, and the credential only reaches
 * the host after the next successful run — so there is no event to push. A
 * fetch when the settings page opens, and on demand, is the honest shape.
 */
export interface AntigravityAuthState {
  authenticated: boolean;
  loading: boolean;
  hint?: string;
  refresh: () => Promise<void>;
}

export function useAntigravityAuth(enabled: boolean): AntigravityAuthState {
  const [authenticated, setAuthenticated] = useState(false);
  const [hint, setHint] = useState<string | undefined>(undefined);
  const [loading, setLoading] = useState(false);

  const refresh = useCallback(async () => {
    if (!enabled) return;
    setLoading(true);
    try {
      const status = await antigravityAuthApi.status();
      setAuthenticated(!!status?.authenticated);
      setHint(status?.hint);
    } catch {
      // A status that cannot be read is reported as "not signed in" rather
      // than as an error: there is no action the operator could take about a
      // failed fetch, and the sign-in steps are useful either way.
    } finally {
      setLoading(false);
    }
  }, [enabled]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  return { authenticated, loading, hint, refresh };
}
