export interface AskInput {
  questions?: Array<{
    question: string;
    header?: string;
    multiSelect?: boolean;
    options: Array<{ label: string; description?: string }>;
  }>;
}

export interface ToolCallProps {
  toolUseId?: string;
  chatId?: string;
  name: string;
  input: Record<string, unknown> | undefined;
  output?: string;
  isError?: boolean;
  status: "running" | "done";
  /** Opens the call's detail immediately — the turn timeline expands into it. */
  defaultOpen?: boolean;
  onAnswerQuestion?: (text: string) => void;
}
