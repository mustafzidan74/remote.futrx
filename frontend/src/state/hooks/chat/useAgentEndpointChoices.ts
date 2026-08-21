import { useEffect, useState } from "preact/hooks";
import { agentEndpointsApi } from "../../../api/agentEndpointsApi";
import type { AgentEndpointChoice } from "../../../models/agentEndpoints";

/**
 * The composer's list of third-party agent endpoints a chat may be pointed at.
 *
 * It is fetched once per session rather than per chat: the register changes
 * only when an administrator edits it, and every chat in the workspace shows
 * the same list. A deployment with no register, or one where every profile is
 * switched off, simply yields an empty list and the composer shows no
 * third-party section at all.
 */
export function useAgentEndpointChoices(enabled: boolean): AgentEndpointChoice[] {
  const [choices, setChoices] = useState<AgentEndpointChoice[]>([]);

  useEffect(() => {
    if (!enabled) return;
    let live = true;
    void agentEndpointsApi
      .choices()
      .then((payload) => {
        if (live) setChoices(payload.endpoints ?? []);
      })
      .catch(() => {
        // The section is an optional capability, not a precondition for
        // sending a prompt. A deployment without it loses nothing it had.
        if (live) setChoices([]);
      });
    return () => {
      live = false;
    };
  }, [enabled]);

  return choices;
}
