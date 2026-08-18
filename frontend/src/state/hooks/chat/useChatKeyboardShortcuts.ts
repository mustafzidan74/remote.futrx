import { useEffect } from "preact/hooks";
import type { ChatStatus } from "../../../models/chat";
import { isDictating } from "../../chat/voiceInputState";

export function useChatKeyboardShortcuts({
  status,
  onCancel,
}: {
  status: ChatStatus;
  onCancel: () => void;
}) {
  useEffect(() => {
    function onKey(event: KeyboardEvent) {
      // Escape stops a live microphone before it cancels a run: dictating a
      // queued prompt while the agent works is ordinary, and killing the run
      // because someone wanted the microphone off would be a nasty surprise.
      if (event.key === "Escape" && status === "streaming" && !isDictating()) onCancel();
    }
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [status, onCancel]);
}
