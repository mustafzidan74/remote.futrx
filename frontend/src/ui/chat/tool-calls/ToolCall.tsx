import { AskUserQuestion } from "./ask-user-question/AskUserQuestion";
import type { AskInput, ToolCallProps } from "./ToolCallTypes";
import { BashCall } from "./renderers/BashCall";
import { EditCall } from "./renderers/EditCall";
import { GenericCall } from "./renderers/GenericCall";
import { ReadCall } from "./renderers/ReadCall";
import { SearchCall } from "./renderers/SearchCall";
import { WriteCall } from "./renderers/WriteCall";

export function ToolCall({ toolUseId, chatId, name, input, output, isError, status, defaultOpen, onAnswerQuestion }: ToolCallProps) {
  if (name === "AskUserQuestion" && toolUseId && chatId && onAnswerQuestion) {
    return (
      <AskUserQuestion
        toolUseId={toolUseId}
        chatId={chatId}
        input={(input as unknown as AskInput) ?? { questions: [] }}
        onSubmit={onAnswerQuestion}
      />
    );
  }

  switch (name) {
    case "Read":
      return <ReadCall input={input} output={output} status={status} isError={isError} defaultOpen={defaultOpen} />;
    case "Edit":
    case "MultiEdit":
      return <EditCall input={input} output={output} status={status} isError={isError} defaultOpen={defaultOpen} />;
    case "Write":
      return <WriteCall input={input} output={output} status={status} isError={isError} defaultOpen={defaultOpen} />;
    case "Bash":
      return <BashCall input={input} output={output} status={status} isError={isError} defaultOpen={defaultOpen} />;
    case "Glob":
    case "Grep":
      return <SearchCall name={name} input={input} output={output} status={status} isError={isError} defaultOpen={defaultOpen} />;
    default:
      return <GenericCall name={name} input={input} output={output} status={status} isError={isError} defaultOpen={defaultOpen} />;
  }
}
