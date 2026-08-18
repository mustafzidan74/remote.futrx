import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { agentBrowserApi } from "../../../api/agents/agentBrowserApi";
import { agentBrowserTargetUrl } from "../../projects/projectPreviewLinksState";

/** How long to wait for a cold Agent Browser to finish provisioning. */
const readyTimeoutMs = 90_000;
const pollIntervalMs = 1_500;

export interface AgentBrowserOpener {
  /** Port currently being opened, or null. */
  busyPort: number | null;
  /** Port whose navigation succeeded most recently, or null. */
  openedPort: number | null;
  error: string | null;
  /** The port the error belongs to, so a row shows only its own failure. */
  errorPort: number | null;
  open: (port: number) => Promise<AgentBrowserResult>;
  /**
   * The same start/wait/navigate sequence for an address rather than a port.
   * `/browser <url>` uses it; nothing about the flow differs except that the
   * caller already knows where it wants to land.
   */
  openUrl: (url: string) => Promise<AgentBrowserResult>;
}

/**
 * The outcome, returned as well as stored. A row in the port list reads the
 * state; a caller with no row of its own — the `/browser` command — needs the
 * reason back, and reading it off state would give the value from before the
 * request.
 */
export interface AgentBrowserResult {
  ok: boolean;
  error?: string;
}

/**
 * "Open in Agent Browser" for one project.
 *
 * Three steps, in this order: make sure the shared browser is up (the start
 * endpoint is idempotent and provisions in the background), reveal the pane so
 * the user watches it come up, then drive it to the port. The wait sits
 * between the reveal and the navigation because a cold container needs a
 * minute to install Chromium, and the pane already renders that state.
 *
 * The browser is sent to container loopback rather than the public preview
 * host: it runs inside the project's container, so loopback reaches the dev
 * server directly and the platform's sign-in page never appears.
 */
export function useAgentBrowserOpener({
  projectId,
  onOpened,
}: {
  projectId: string | null;
  onOpened?: (port: number) => void;
}): AgentBrowserOpener {
  const [busyPort, setBusyPort] = useState<number | null>(null);
  const [openedPort, setOpenedPort] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [errorPort, setErrorPort] = useState<number | null>(null);
  const aliveRef = useRef(true);

  useEffect(() => {
    aliveRef.current = true;
    return () => {
      aliveRef.current = false;
    };
  }, []);

  // `port` is what the port list reports progress against; a URL-driven open
  // has no row to report into, so it passes null and reports through `error`.
  const drive = useCallback(
    async (url: string, port: number | null): Promise<AgentBrowserResult> => {
      if (!projectId) {
        const message = "This chat is not attached to a project container.";
        setError(message);
        setErrorPort(port);
        return { ok: false, error: message };
      }
      setBusyPort(port);
      setError(null);
      setErrorPort(null);
      setOpenedPort(null);
      try {
        await agentBrowserApi.startAgentBrowser(projectId);
        onOpened?.(port ?? 0);
        await waitForBrowserCore(projectId, () => aliveRef.current);
        await agentBrowserApi.navigateAgentBrowser(projectId, url);
        if (!aliveRef.current) return { ok: true };
        setOpenedPort(port);
        return { ok: true };
      } catch (cause) {
        const message = (cause as Error).message || "Could not open the Agent Browser.";
        if (!aliveRef.current) return { ok: false, error: message };
        setError(message);
        setErrorPort(port);
        return { ok: false, error: message };
      } finally {
        if (aliveRef.current) setBusyPort(null);
      }
    },
    [projectId, onOpened],
  );

  const open = useCallback(
    (port: number) => drive(agentBrowserTargetUrl(port), port),
    [drive],
  );
  const openUrl = useCallback((url: string) => drive(url, null), [drive]);

  return { busyPort, openedPort, error, errorPort, open, openUrl };
}

/**
 * Polls until the browser core answers. `core: "ready"` is the state that
 * matters: the noVNC view can still be coming up while CDP already accepts
 * navigation.
 */
async function waitForBrowserCore(projectId: string, alive: () => boolean): Promise<void> {
  const deadline = Date.now() + readyTimeoutMs;
  for (;;) {
    if (!alive()) return;
    const info = await agentBrowserApi.fetchAgentBrowserStatus(projectId);
    if (info.core === "ready") return;
    if (info.status === "error") {
      throw new Error(info.error || "The Agent Browser failed to start.");
    }
    if (Date.now() >= deadline) {
      throw new Error("The Agent Browser did not finish starting in time.");
    }
    await delay(pollIntervalMs);
  }
}

function delay(ms: number): Promise<void> {
  return new Promise((resolve) => {
    window.setTimeout(resolve, ms);
  });
}
