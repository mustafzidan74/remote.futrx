import type { ChatEventRouting, SyntheticKind } from "./chat";

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
  | {
      type: "user";
      text: string;
      t: number;
      synthetic?: SyntheticKind;
      /** Which model answered this turn and why, when routing chose it. */
      routing?: ChatEventRouting;
    }
  | AssistantMessageBlock
  | { type: "error"; message: string; t: number };
