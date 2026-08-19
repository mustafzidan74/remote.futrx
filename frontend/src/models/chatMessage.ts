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
      /**
       * When the tool started and finished, copied straight off the two chat
       * events that describe it. They are what lets the turn timeline show a
       * duration per step without asking the backend for anything new.
       */
      startedAt?: number;
      endedAt?: number;
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
