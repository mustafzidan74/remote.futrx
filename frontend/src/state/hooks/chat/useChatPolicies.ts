import { useCallback, useMemo, useState } from "preact/hooks";
import type {
  ChatMeta,
  ChatProvider,
  TeamPatch,
  TeamRoleName,
  UpdateChatInput,
} from "../../../models/chat";
import {
  armAutopilotPatch,
  autoTestEnabled,
  autopilotView,
  validateAutopilotDraft,
  type AutopilotDraft,
  type AutopilotView,
} from "../../chat/chatPolicyState";
import { armTeamPatch, boundedLoops, teamView, type TeamView } from "../../chat/teamState";

export interface ChatPolicies {
  autopilot: AutopilotView;
  autoTest: boolean;
  team: TeamView;
  /** Providers with a host-side login — the only ones a seat may be given. */
  connectedProviders: ChatProvider[];
  /** True while a patch is in flight, so the controls can lock. */
  busy: boolean;
  armAutopilot: (draft: AutopilotDraft) => void;
  saveAutopilotLimits: (draft: AutopilotDraft) => void;
  stopAutopilot: () => void;
  setAutoTest: (enabled: boolean) => void;
  /** Arms team mode with the cast the panel is showing. */
  startTeam: () => void;
  stopTeam: () => void;
  setTeamRole: (role: TeamRoleName, patch: { provider?: ChatProvider; model?: string; enabled?: boolean }) => void;
  setTeamLoops: (loops: number) => void;
  setTeamAutoFix: (autoFix: boolean) => void;
  /** Sends a Playwright check now, labelled so the transcript badges it. */
  sendTest: (prompt: string) => void;
}

/**
 * The composer's post-run policy controls.
 *
 * Everything the controls display comes from the server's copy of the chat:
 * the round counter is spent by the post-run driver, not by the browser, and
 * the workspace socket already pushes a chat upsert on every metadata write.
 * So this hook only sends patches and re-reads — it never guesses at a count.
 */
export function useChatPolicies({
  chat,
  streaming,
  connectedProviders,
  applyMeta,
  sendPrompt,
}: {
  chat: ChatMeta;
  streaming: boolean;
  /** Providers the agent auth registry reports as logged in on the host. */
  connectedProviders: ChatProvider[];
  applyMeta: (patch: UpdateChatInput) => Promise<void>;
  sendPrompt: (text: string) => boolean;
}): ChatPolicies {
  const [busy, setBusy] = useState(false);

  const patch = useCallback(
    (input: UpdateChatInput) => {
      setBusy(true);
      void applyMeta(input).finally(() => setBusy(false));
    },
    [applyMeta],
  );

  const armAutopilot = useCallback(
    (draft: AutopilotDraft) => {
      const autopilot = armAutopilotPatch(draft);
      if (!autopilot) return;
      patch({ autopilot });
    },
    [patch],
  );

  // Adjusting limits without touching the switch: the server only resets the
  // counters on an enable transition, so a mid-flight loop keeps its progress.
  const saveAutopilotLimits = useCallback(
    (draft: AutopilotDraft) => {
      const validation = validateAutopilotDraft(draft);
      if (!validation.valid || !validation.patch) return;
      patch({ autopilot: validation.patch });
    },
    [patch],
  );

  const stopAutopilot = useCallback(() => {
    patch({ autopilot: { enabled: false } });
  }, [patch]);

  const setAutoTest = useCallback(
    (enabled: boolean) => {
      patch({ autoTest: { enabled } });
    },
    [patch],
  );

  // Arming sends the whole cast rather than a bare switch, so the server
  // records the providers the user could see in the panel instead of
  // re-deriving them later against a possibly different set of logins.
  const startTeam = useCallback(() => {
    patch({ team: armTeamPatch(chat, connectedProviders) });
  }, [patch, chat, connectedProviders]);

  const stopTeam = useCallback(() => {
    patch({ team: { enabled: false } });
  }, [patch]);

  const setTeamRole = useCallback(
    (role: TeamRoleName, role_patch: { provider?: ChatProvider; model?: string; enabled?: boolean }) => {
      const team: TeamPatch = { roles: { [role]: role_patch } };
      patch({ team });
    },
    [patch],
  );

  const setTeamLoops = useCallback(
    (loops: number) => {
      patch({ team: { maxLoops: boundedLoops(loops) } });
    },
    [patch],
  );

  const setTeamAutoFix = useCallback(
    (autoFix: boolean) => {
      patch({ team: { autoFix } });
    },
    [patch],
  );

  const sendTest = useCallback(
    (prompt: string) => {
      sendPrompt(prompt);
    },
    [sendPrompt],
  );

  const team = useMemo(() => teamView(chat, connectedProviders), [chat, connectedProviders]);

  return {
    autopilot: autopilotView(chat, streaming),
    autoTest: autoTestEnabled(chat),
    team,
    connectedProviders,
    busy,
    armAutopilot,
    saveAutopilotLimits,
    stopAutopilot,
    setAutoTest,
    startTeam,
    stopTeam,
    setTeamRole,
    setTeamLoops,
    setTeamAutoFix,
    sendTest,
  };
}
