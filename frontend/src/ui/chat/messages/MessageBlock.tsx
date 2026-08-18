import type { ChatMessageBlock } from "../../../models/chatMessage";
import { AssistantMessage } from "./AssistantMessage";
import { ErrorMessage } from "./ErrorMessage";
import { UserMessage } from "./UserMessage";

export function MessageBlock({
  block,
  streaming,
  chatId,
  cwd,
  onAnswerQuestion,
  onRewind,
}: {
  block: ChatMessageBlock;
  streaming: boolean;
  chatId?: string;
  cwd?: string;
  onAnswerQuestion?: (text: string) => void;
  onRewind?: (t: number, text: string) => void;
}) {
  if (block.type === "user") {
    return (
      <UserMessage
        text={block.text}
        t={block.t}
        synthetic={block.synthetic}
        onRewind={onRewind}
      />
    );
  }

  if (block.type === "error") {
    return <ErrorMessage message={block.message} />;
  }

  return (
    <AssistantMessage
      block={block}
      streaming={streaming}
      chatId={chatId}
      cwd={cwd}
      onAnswerQuestion={onAnswerQuestion}
    />
  );
}
