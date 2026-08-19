import type { ChatMessageBlock } from "../../../models/chatMessage";
import { AssistantMessage } from "./AssistantMessage";
import { ErrorMessage } from "./ErrorMessage";
import { UserMessage } from "./UserMessage";

export function MessageBlock({
  block,
  blockIndex,
  highlighted,
  streaming,
  chatId,
  cwd,
  onAnswerQuestion,
  onRewind,
  onSaveSnippet,
}: {
  block: ChatMessageBlock;
  /** Position in the thread; the anchor a search hit scrolls to. */
  blockIndex?: number;
  /** True while this block is the target of a search hit or deep link. */
  highlighted?: boolean;
  streaming: boolean;
  chatId?: string;
  cwd?: string;
  onAnswerQuestion?: (text: string) => void;
  onRewind?: (t: number, text: string) => void;
  /** Offers "Save as snippet" on prompts the user wrote. */
  onSaveSnippet?: (text: string) => void;
}) {
  return (
    <div
      data-block-index={blockIndex}
      data-message-at={block.t}
      class={highlighted ? "chat-message-flash rounded-lg" : undefined}
    >
      {renderBlock()}
    </div>
  );

  function renderBlock() {
    if (block.type === "user") {
      return (
        <UserMessage
          text={block.text}
          t={block.t}
          synthetic={block.synthetic}
          routing={block.routing}
          onRewind={onRewind}
          onSaveSnippet={onSaveSnippet}
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
}
