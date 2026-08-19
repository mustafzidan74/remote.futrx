import type { AssistantMessageBlock } from "../../../models/chatMessage";
import { AssistantPartList } from "./AssistantPartList";

/**
 * The turn's content. What the turn is *doing* right now is the activity
 * strip's job, pinned above the composer — a second "thinking" chip inside the
 * transcript said less and scrolled out of view while it said it.
 */
export function AssistantMessage({
  block,
  streaming,
  chatId,
  cwd,
  onAnswerQuestion,
}: {
  block: AssistantMessageBlock;
  streaming: boolean;
  chatId?: string;
  cwd?: string;
  onAnswerQuestion?: (text: string) => void;
}) {
  return (
    <div class="codex-assistant-block space-y-2 max-w-full">
      <AssistantPartList
        parts={block.parts}
        streaming={streaming}
        chatId={chatId}
        cwd={cwd}
        onAnswerQuestion={onAnswerQuestion}
      />
    </div>
  );
}
