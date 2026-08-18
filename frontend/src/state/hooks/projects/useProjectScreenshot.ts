import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import { projectApi } from "../../../api/projectApi";
import type { ProjectScreenshot, ScreenshotDelivery } from "../../../models/screenshot";

export interface ProjectScreenshotCard {
  screenshot: ProjectScreenshot;
  delivered?: ScreenshotDelivery[];
  publicUrl?: string;
  /** False when no notification sink is configured on this server. */
  canSend: boolean;
}

export interface ProjectScreenshotState {
  /** The port being captured, so only its own row shows a spinner. */
  busyPort: number | null;
  /** The port a failure belongs to, for the same reason. */
  errorPort: number | null;
  error: string | null;
  sending: boolean;
  card: ProjectScreenshotCard | null;
  capture: (port: number) => Promise<void>;
  send: () => void;
  dismiss: () => void;
}

/**
 * "Share screenshot" for one project's preview popover.
 *
 * Capturing and sending are two requests rather than one because they are two
 * decisions: the picture on screen is the one that gets sent, and re-capturing
 * to deliver would quietly send a later moment than the one being looked at.
 */
export function useProjectScreenshot(projectId: string): ProjectScreenshotState {
  const [busyPort, setBusyPort] = useState<number | null>(null);
  const [errorPort, setErrorPort] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  const [card, setCard] = useState<ProjectScreenshotCard | null>(null);
  const alive = useRef(true);

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  // Another project's capture must not linger in a reopened popover.
  useEffect(() => {
    setCard(null);
    setError(null);
    setErrorPort(null);
  }, [projectId]);

  const capture = useCallback(
    async (port: number) => {
      setBusyPort(port);
      setError(null);
      setErrorPort(null);
      setCard(null);
      try {
        const result = await projectApi.captureScreenshot(projectId, { port });
        if (!alive.current) return;
        setCard({ screenshot: result.screenshot, canSend: result.notifications });
      } catch (cause) {
        if (!alive.current) return;
        setError((cause as Error).message || "Could not capture this port.");
        setErrorPort(port);
      } finally {
        if (alive.current) setBusyPort(null);
      }
    },
    [projectId],
  );

  const send = useCallback(() => {
    if (!card) return;
    setSending(true);
    setError(null);
    projectApi
      .sendScreenshot(projectId, card.screenshot.id)
      .then((result) => {
        if (!alive.current) return;
        setCard({
          screenshot: result.screenshot,
          canSend: true,
          delivered: result.delivered,
          publicUrl: result.publicUrl,
        });
      })
      .catch((cause: Error) => {
        if (!alive.current) return;
        setError(cause.message || "Could not send the screenshot.");
        setErrorPort(card.screenshot.port);
      })
      .finally(() => {
        if (alive.current) setSending(false);
      });
  }, [card, projectId]);

  return {
    busyPort,
    errorPort,
    error,
    sending,
    card,
    capture,
    send,
    dismiss: () => setCard(null),
  };
}
