import type { SyntheticKind } from "./chat";

export type AssistantMessagePart =
  | { kind: "text"; text: string }
  | {
      kind: "tool";
      id: string;
      name: string;
      input: Record<string, unknown>;
      output?: string;
      isError?: boolean;
      status: "running" | "done";
    }
  | { kind: "thinking"; text: string };

export type AssistantMessageBlock = {
  type: "assistant";
  parts: AssistantMessagePart[];
  t: number;
  isComplete: boolean;
};

export type ChatMessageBlock =
  | { type: "user"; text: string; t: number; synthetic?: SyntheticKind }
  | AssistantMessageBlock
  | { type: "error"; message: string; t: number };
