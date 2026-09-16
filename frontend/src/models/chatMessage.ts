import type { ChatEventRouting, ChatProvider, SyntheticKind } from "./chat";

export type AssistantMessagePart =
  | { kind: "text"; text: string }
  | {
      kind: "tool";
      id: string;
      name: string;
      input: Record<string, unknown>;
      output?: string;
      outputRef?: string;
      outputBytes?: number;
      outputTruncated?: boolean;
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
  | { kind: "thinking"; text: string }
  | {
      kind: "interaction";
      id: string;
      method: string;
      input: Record<string, unknown>;
      interactionKind: string;
      supportsCancellation: boolean;
      status: string;
    }
  | {
      kind: "collaboration";
      id: string;
      name?: string;
      data: Record<string, unknown>;
      status: string;
    }
  | { kind: "turn-status"; status: string; data?: Record<string, unknown>; provider?: ChatProvider }
  // Something the platform did during the turn, shown beside the reply rather
  // than inside it: today, moving to another model when the chosen one had no
  // capacity.
  | { kind: "model-fallback"; from: string; to: string };

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
