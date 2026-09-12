import type { RefObject } from "preact";
import { useEffect, useMemo, useState } from "preact/hooks";
import type { ChatStatus, TranscriptIndexProgress } from "../../../models/chat";
import type { ChatMessageBlock } from "../../../models/chatMessage";
import type { PlaybookLibrary } from "../../../state/hooks/chat/usePlaybooks";
import { ErrorBanner } from "../../primitives/Feedback";
import { MessageBlock } from "./MessageBlock";
import { MessageSkeleton } from "./MessageSkeleton";
import { ThreadEmptyState } from "./ThreadEmptyState";
import type { ChatInteractionResponder } from "../../../types/chatApi";

const INITIAL_VISIBLE_BLOCKS = 80;
const LOAD_MORE_BLOCKS = 80;

export function MessageList({
  status,
  blocks,
  highlightAt,
  hasOlder,
  loadingOlder,
  indexingProgress,
  error,
  chatId,
  cwd,
  playbooks,
  scrollRef,
  contentRef,
  bottomRef,
  onScroll,
  onAnswerQuestion,
  onRespondInteraction,
  onLoadOlder,
  onRewind,
  onSaveSnippet,
}: {
  status: ChatStatus;
  blocks: ChatMessageBlock[];
  /** A message instant to scroll to and flash, or null. */
  highlightAt: number | null;
  hasOlder: boolean;
  loadingOlder: boolean;
  indexingProgress: TranscriptIndexProgress | null;
  error: string | null;
  chatId: string;
  cwd?: string;
  playbooks?: PlaybookLibrary;
  scrollRef: RefObject<HTMLDivElement>;
  contentRef: RefObject<HTMLDivElement>;
  bottomRef: RefObject<HTMLDivElement>;
  onScroll: () => void;
  onAnswerQuestion: (text: string) => void;
  onRespondInteraction?: ChatInteractionResponder;
  onLoadOlder: () => Promise<void>;
  onRewind: (t: number, text: string) => void;
  /** Offers "Save as snippet" on prompts the user wrote. */
  onSaveSnippet?: (text: string) => void;
}) {
  const [visibleBlockCount, setVisibleBlockCount] = useState(INITIAL_VISIBLE_BLOCKS);
  const firstVisibleIndex = Math.max(0, blocks.length - visibleBlockCount);
  const hiddenCount = firstVisibleIndex;
  const visibleBlocks = useMemo(
    () => blocks.slice(firstVisibleIndex),
    [blocks, firstVisibleIndex]
  );

  useEffect(() => {
    setVisibleBlockCount(INITIAL_VISIBLE_BLOCKS);
  }, [chatId]);

  // A search hit points at one event's timestamp, but the thread renders
  // coalesced blocks whose `t` is the first event in them — so the target is
  // the nearest block rather than an exact match. Revealing it may need older
  // blocks unhidden first, which is why this widens the window before looking.
  const highlightBlockIndex = useMemo(
    () => nearestBlockIndex(blocks, highlightAt),
    [blocks, highlightAt],
  );

  useEffect(() => {
    if (highlightBlockIndex < 0) return;
    if (highlightBlockIndex < firstVisibleIndex) {
      setVisibleBlockCount(blocks.length - highlightBlockIndex + LOAD_MORE_BLOCKS);
      return;
    }
    const frame = requestAnimationFrame(() => {
      const element = contentRef.current?.querySelector<HTMLElement>(
        `[data-block-index="${highlightBlockIndex}"]`,
      );
      element?.scrollIntoView({ block: "center", behavior: "smooth" });
    });
    return () => cancelAnimationFrame(frame);
  }, [highlightBlockIndex, firstVisibleIndex, blocks.length, contentRef]);

  async function showOlder() {
    if (hiddenCount > 0) {
      setVisibleBlockCount((count) => count + LOAD_MORE_BLOCKS);
      return;
    }
    const element = scrollRef.current;
    const beforeHeight = element?.scrollHeight ?? 0;
    const beforeTop = element?.scrollTop ?? 0;
    await onLoadOlder();
    setVisibleBlockCount((count) => count + LOAD_MORE_BLOCKS);
    requestAnimationFrame(() => {
      const next = scrollRef.current;
      if (!next) return;
      next.scrollTop = beforeTop + next.scrollHeight - beforeHeight;
    });
  }

  return (
    <div
      ref={scrollRef}
      onScroll={onScroll}
      class="codex-message-scroll h-full overflow-y-auto overflow-x-hidden touch-scroll scrollbar-thin px-3 pb-6 pt-4 sm:px-5 md:px-8 md:pt-7"
    >
      {/* A measured column: long assistant prose stays readable on wide panes. */}
      <div ref={contentRef} class="mx-auto w-full min-w-0 max-w-[54rem] space-y-5 md:space-y-6">
        {status === "loading" && <MessageSkeleton />}

        {indexingProgress && (
          <div class="rounded-card border border-line bg-surface p-4 text-[13px] text-ink-300">
            Preparing conversation… {indexPercent(indexingProgress)}%
          </div>
        )}

        {status !== "loading" && blocks.length === 0 && !indexingProgress && <ThreadEmptyState cwd={cwd} playbooks={playbooks} />}

        {(hiddenCount > 0 || hasOlder) && (
          <div class="flex justify-center">
            <button
              type="button"
              onClick={showOlder}
              disabled={loadingOlder}
              class="h-8 rounded-control px-3 text-[12px] text-ink-400 transition-colors hover:bg-tint-strong hover:text-ink-100"
            >
              {hiddenCount > 0
                ? `Show ${Math.min(hiddenCount, LOAD_MORE_BLOCKS)} older message${Math.min(hiddenCount, LOAD_MORE_BLOCKS) === 1 ? "" : "s"}`
                : loadingOlder
                  ? "Loading older messages"
                  : "Load older messages"}
            </button>
          </div>
        )}

        {visibleBlocks.map((block, index) => {
          const blockIndex = firstVisibleIndex + index;
          return (
            <MessageBlock
              key={`${block.type}-${block.t}-${blockIndex}`}
              block={block}
              blockIndex={blockIndex}
              highlighted={blockIndex === highlightBlockIndex}
              streaming={status === "streaming" && blockIndex === blocks.length - 1}
              chatId={chatId}
              cwd={cwd}
              onAnswerQuestion={onAnswerQuestion}
              onRespondInteraction={onRespondInteraction}
              onRewind={onRewind}
              onSaveSnippet={onSaveSnippet}
            />
          );
        })}

        {error && <ErrorBanner message={error} />}

        <div ref={bottomRef} class="h-px" aria-hidden="true" />
      </div>
    </div>
  );
}

/**
 * The block closest to a timestamp, or -1 when there is nothing to reveal.
 * "Closest" rather than "equal" because a search hit carries the timestamp of
 * one event while a rendered block spans several.
 */
function nearestBlockIndex(blocks: ChatMessageBlock[], at: number | null): number {
  if (!at || blocks.length === 0) return -1;
  let best = -1;
  let bestDistance = Number.POSITIVE_INFINITY;
  for (let index = 0; index < blocks.length; index++) {
    const distance = Math.abs(blocks[index].t - at);
    if (distance < bestDistance) {
      best = index;
      bestDistance = distance;
    }
  }
  return best;
}

function indexPercent(progress: TranscriptIndexProgress): number {
  if (progress.totalBytes <= 0) return 0;
  return Math.min(99, Math.floor((progress.indexedBytes / progress.totalBytes) * 100));
}
