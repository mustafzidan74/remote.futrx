import { useCallback, useEffect, useState } from "preact/hooks";
import {
  agentActivityPreferences,
  agentPhaseStore,
  type ActivityPhase,
  type LiveChatPhase,
} from "../../chat/agentActivity";

/**
 * A once-a-second clock, running only while something is watching it.
 *
 * The elapsed timer and the stuck hint both need "now", and neither belongs in
 * the reducer. Keeping the interval inside the components that display a time
 * means a ticking second re-renders the strip and the pill rather than the
 * whole thread.
 */
export function useActivityClock(active: boolean): number {
  const [now, setNow] = useState(() => Date.now());

  useEffect(() => {
    if (!active) return;
    setNow(Date.now());
    const timer = window.setInterval(() => setNow(Date.now()), 1000);
    return () => window.clearInterval(timer);
  }, [active]);

  return now;
}

/** Publishes the open chat's phase for the sidebar row, and clears it on exit. */
export function usePublishChatPhase(chatId: string, phase: ActivityPhase): void {
  useEffect(() => {
    agentPhaseStore.publish(chatId, phase);
  }, [chatId, phase]);

  useEffect(() => () => agentPhaseStore.clear(chatId), [chatId]);
}

/** The open chat's phase, for views outside the thread. */
export function useLiveChatPhase(): LiveChatPhase | null {
  const [live, setLive] = useState<LiveChatPhase | null>(() => agentPhaseStore.current);
  useEffect(() => agentPhaseStore.subscribe(setLive), []);
  return live;
}

/** The remembered "Show thinking" choice, and a way to change it. */
export function useShowThinking(): [boolean, () => void] {
  const [show, setShow] = useState(() => agentActivityPreferences.showThinking());
  const toggle = useCallback(() => {
    setShow((current) => {
      const next = !current;
      agentActivityPreferences.setShowThinking(next);
      return next;
    });
  }, []);
  return [show, toggle];
}
