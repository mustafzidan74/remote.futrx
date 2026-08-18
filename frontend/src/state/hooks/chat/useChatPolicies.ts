import { useCallback, useState } from "preact/hooks";
import type { ChatMeta, UpdateChatInput } from "../../../models/chat";
import {
  armAutopilotPatch,
  autoTestEnabled,
  autopilotView,
  validateAutopilotDraft,
  type AutopilotDraft,
  type AutopilotView,
} from "../../chat/chatPolicyState";

export interface ChatPolicies {
  autopilot: AutopilotView;
  autoTest: boolean;
  /** True while a patch is in flight, so the controls can lock. */
  busy: boolean;
  armAutopilot: (draft: AutopilotDraft) => void;
  saveAutopilotLimits: (draft: AutopilotDraft) => void;
  stopAutopilot: () => void;
  setAutoTest: (enabled: boolean) => void;
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
  applyMeta,
  sendPrompt,
}: {
  chat: ChatMeta;
  streaming: boolean;
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

  const sendTest = useCallback(
    (prompt: string) => {
      sendPrompt(prompt);
    },
    [sendPrompt],
  );

  return {
    autopilot: autopilotView(chat, streaming),
    autoTest: autoTestEnabled(chat),
    busy,
    armAutopilot,
    saveAutopilotLimits,
    stopAutopilot,
    setAutoTest,
    sendTest,
  };
}
