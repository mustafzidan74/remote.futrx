import type { ChatEvent, ChatEventPage, ChatStatus } from "../../models/chat";
import type {
  AssistantMessageBlock,
  AssistantMessagePart,
  ChatMessageBlock,
} from "../../models/chatMessage";
import type { ChatUsageTotals } from "../../models/chatUsage";
import { emptyActivity, reduceActivity, type AgentActivity } from "./agentActivity.ts";

type AssistantToolPart = Extract<AssistantMessagePart, { kind: "tool" }>;

type Usage =
  | {
      input_tokens?: number;
      output_tokens?: number;
      cache_read_input_tokens?: number;
      cache_creation_input_tokens?: number;
    }
  | null;

const EMPTY_USAGE_TOTALS: ChatUsageTotals = {
  inputTokens: 0,
  outputTokens: 0,
  cacheReadTokens: 0,
  cacheWriteTokens: 0,
};

export interface ChatRenderState {
  events: ChatEvent[];
  blocks: ChatMessageBlock[];
  usageTotals: ChatUsageTotals;
  /** What the newest run is doing right now — see `agentActivity.ts`. */
  activity: AgentActivity;
  eventCount: number;
  hasOlder: boolean;
  nextBefore: number;
  lastSeq: number;
}

class ChatEventStateProjector {
  empty(): ChatRenderState {
    return {
      events: [],
      blocks: [],
      usageTotals: EMPTY_USAGE_TOTALS,
      activity: emptyActivity(),
      eventCount: 0,
      hasOlder: false,
      nextBefore: 0,
      lastSeq: 0,
    };
  }

  fromEvents(
    events: ChatEvent[],
    page: Pick<ChatEventPage, "hasMore" | "nextBefore" | "lastSeq">
  ): ChatRenderState {
    let usageTotals = EMPTY_USAGE_TOTALS;
    // The activity is folded in the same pass as the totals: it is a fold over
    // the very events the blocks are built from, so replaying history lands on
    // the same phase the live stream would have produced.
    let activity = emptyActivity();
    for (const event of events) {
      usageTotals = this.addUsage(usageTotals, event);
      activity = reduceActivity(activity, event);
    }
    return {
      events,
      blocks: events.reduce<ChatMessageBlock[]>(
        (blocks, event) => this.appendBlock(blocks, event),
        []
      ),
      usageTotals,
      activity,
      eventCount: events.length,
      hasOlder: page.hasMore,
      nextBefore: page.nextBefore ?? 0,
      lastSeq: Math.max(page.lastSeq, this.latestSequence(events)),
    };
  }

  append(state: ChatRenderState, events: ChatEvent[]): ChatRenderState {
    if (events.length === 0) return state;
    const merged = this.mergeEvents(state.events, events);
    return this.fromEvents(merged, {
      hasMore: state.hasOlder,
      nextBefore: state.nextBefore,
      lastSeq: Math.max(state.lastSeq, this.latestSequence(events)),
    });
  }

  prepend(state: ChatRenderState, page: ChatEventPage): ChatRenderState {
    return this.fromEvents(this.mergeEvents(page.events, state.events), page);
  }

  latestSequence(events: ChatEvent[]): number {
    return events.reduce((max, event) => Math.max(max, event.seq || 0), 0);
  }

  statusAfter(event: ChatEvent, current: ChatStatus): ChatStatus {
    if (event.type === "complete" || event.type === "error") {
      // The backend clears the run lock in a later sync event. Keep streaming
      // until sync running=false so queued prompts are not sent into a locked run.
      return current === "streaming" ? "streaming" : "ready";
    }
    return "streaming";
  }

  private mergeEvents(first: ChatEvent[], second: ChatEvent[]): ChatEvent[] {
    const merged = [...first];
    const seenSequences = new Set<number>();
    for (const event of merged) {
      if (event.seq) seenSequences.add(event.seq);
    }
    for (const event of second) {
      if (event.seq && seenSequences.has(event.seq)) continue;
      merged.push(event);
      if (event.seq) seenSequences.add(event.seq);
    }
    return merged.sort((left, right) => this.eventOrder(left) - this.eventOrder(right));
  }

  private eventOrder(event: ChatEvent): number {
    return event.seq || event.t;
  }

  private addUsage(totals: ChatUsageTotals, event: ChatEvent): ChatUsageTotals {
    if (event.type !== "complete" || !event.usage) return totals;

    try {
      const usage = (typeof event.usage === "string" ? JSON.parse(event.usage) : event.usage) as Usage;
      if (!usage) return totals;
      return {
        inputTokens: totals.inputTokens + (usage.input_tokens ?? 0),
        outputTokens: totals.outputTokens + (usage.output_tokens ?? 0),
        cacheReadTokens: totals.cacheReadTokens + (usage.cache_read_input_tokens ?? 0),
        cacheWriteTokens: totals.cacheWriteTokens + (usage.cache_creation_input_tokens ?? 0),
      };
    } catch {
      return totals;
    }
  }

  private appendBlock(blocks: ChatMessageBlock[], event: ChatEvent): ChatMessageBlock[] {
    switch (event.type) {
      case "user": {
        const next = this.endTrailingAssistant(blocks);
        // The label rides through to the bubble so an unattended round is
        // never mistaken for something the operator typed. It is spread in
        // only when present, keeping a human prompt's block shape untouched.
        return [
          ...next,
          {
            type: "user",
            text: event.text,
            t: event.t,
            ...(event.synthetic ? { synthetic: event.synthetic } : {}),
            // The routing block says which model actually answered the turn.
            // It rides through the same way, so a turn nobody routed keeps
            // exactly the block shape it always had.
            ...(event.routing ? { routing: event.routing } : {}),
          },
        ];
      }
      case "assistant_text": {
        const { blocks: next, assistant } = this.ensureTrailingAssistant(blocks, event.t);
        const lastIndex = assistant.parts.length - 1;
        const last = assistant.parts[lastIndex];
        if (last?.kind === "text") {
          assistant.parts[lastIndex] = { ...last, text: last.text + event.text };
        } else {
          assistant.parts.push({ kind: "text", text: event.text });
        }
        return next;
      }
      case "thinking": {
        const { blocks: next, assistant } = this.ensureTrailingAssistant(blocks, event.t);
        assistant.parts.push({ kind: "thinking", text: event.text });
        return next;
      }
      case "tool_use_start": {
        const { blocks: next, assistant } = this.ensureTrailingAssistant(blocks, event.t);
        assistant.parts.push({
          kind: "tool",
          id: event.id,
          name: event.name,
          input: event.input ?? {},
          status: "running",
          startedAt: event.t,
        });
        return next;
      }
      case "tool_use_end":
        return this.updateTrailingTool(blocks, event.id, {
          output: event.output,
          isError: event.isError,
          status: "done",
          endedAt: event.t,
        });
      case "complete":
        return this.endTrailingAssistant(blocks);
      case "error": {
        const next = this.endTrailingAssistant(blocks);
        return [...next, { type: "error", message: event.message, t: event.t }];
      }
      default:
        return blocks;
    }
  }

  private endTrailingAssistant(blocks: ChatMessageBlock[]): ChatMessageBlock[] {
    const lastIndex = blocks.length - 1;
    const last = blocks[lastIndex];
    if (!last || last.type !== "assistant" || last.isComplete) return blocks;
    const next = blocks.slice();
    next[lastIndex] = { ...last, isComplete: true };
    return next;
  }

  private ensureTrailingAssistant(
    blocks: ChatMessageBlock[],
    timestamp: number
  ): { blocks: ChatMessageBlock[]; assistant: AssistantMessageBlock } {
    const lastIndex = blocks.length - 1;
    const last = blocks[lastIndex];
    if (last?.type === "assistant" && !last.isComplete) {
      const next = blocks.slice();
      const assistant: AssistantMessageBlock = { ...last, parts: last.parts.slice() };
      next[lastIndex] = assistant;
      return { blocks: next, assistant };
    }

    const assistant: AssistantMessageBlock = {
      type: "assistant",
      parts: [],
      t: timestamp,
      isComplete: false,
    };
    return { blocks: [...blocks, assistant], assistant };
  }

  private updateTrailingTool(
    blocks: ChatMessageBlock[],
    id: string,
    patch: Partial<AssistantToolPart>
  ): ChatMessageBlock[] {
    const lastIndex = blocks.length - 1;
    const last = blocks[lastIndex];
    if (!last || last.type !== "assistant") return blocks;

    const partIndex = last.parts.findIndex((part) => part.kind === "tool" && part.id === id);
    if (partIndex < 0) return blocks;
    const part = last.parts[partIndex];
    if (part.kind !== "tool") return blocks;

    const next = blocks.slice();
    const parts = last.parts.slice();
    parts[partIndex] = { ...part, ...patch };
    next[lastIndex] = { ...last, parts };
    return next;
  }
}

export const chatEventStateProjector = new ChatEventStateProjector();
