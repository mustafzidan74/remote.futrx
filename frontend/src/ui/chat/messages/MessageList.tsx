import type { RefObject } from "preact";
import { useEffect, useMemo, useState } from "preact/hooks";
import type { ChatStatus } from "../../../models/chat";
import type { ChatMessageBlock } from "../../../models/chatMessage";
import type { PlaybookLibrary } from "../../../state/hooks/chat/usePlaybooks";
import { Loader } from "../../primitives/icons";
import { ErrorBanner } from "../../primitives/Feedback";
import { MessageBlock } from "./MessageBlock";
import { ThreadEmptyState } from "./ThreadEmptyState";

const INITIAL_VISIBLE_BLOCKS = 80;
const LOAD_MORE_BLOCKS = 80;

export function MessageList({
  status,
  blocks,
  hasOlder,
  loadingOlder,
  error,
  chatId,
  cwd,
  playbooks,
  scrollRef,
  contentRef,
  bottomRef,
  onScroll,
  onAnswerQuestion,
  onLoadOlder,
  onRewind,
}: {
  status: ChatStatus;
  blocks: ChatMessageBlock[];
  hasOlder: boolean;
  loadingOlder: boolean;
  error: string | null;
  chatId: string;
  cwd?: string;
  playbooks?: PlaybookLibrary;
  scrollRef: RefObject<HTMLDivElement>;
  contentRef: RefObject<HTMLDivElement>;
  bottomRef: RefObject<HTMLDivElement>;
  onScroll: () => void;
  onAnswerQuestion: (text: string) => void;
  onLoadOlder: () => Promise<void>;
  onRewind: (t: number, text: string) => void;
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
      class="codex-message-scroll h-full overflow-y-auto touch-scroll scrollbar-thin px-3 sm:px-4 md:px-6 pt-3 md:pt-6 pb-5 md:pb-6"
    >
      <div ref={contentRef} class="mx-auto w-full max-w-[1100px] space-y-4 md:space-y-5">
        {status === "loading" && (
          <div class="flex items-center gap-2 text-ink-300 text-sm rounded-lg border border-white/10 bg-white/[0.03] px-3 py-2">
            <Loader class="w-4 h-4 animate-spin" /> Loading conversation
          </div>
        )}

        {status !== "loading" && blocks.length === 0 && <ThreadEmptyState cwd={cwd} playbooks={playbooks} />}

        {(hiddenCount > 0 || hasOlder) && (
          <div class="flex justify-center">
            <button
              type="button"
              onClick={showOlder}
              disabled={loadingOlder}
              class="h-8 px-3 rounded-md text-[12px] text-ink-300 hover:text-ink-100 hover:bg-white/[0.07] border border-white/10"
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
              streaming={status === "streaming" && blockIndex === blocks.length - 1}
              chatId={chatId}
              cwd={cwd}
              onAnswerQuestion={onAnswerQuestion}
              onRewind={onRewind}
            />
          );
        })}

        {error && <ErrorBanner message={error} />}

        <div ref={bottomRef} class="h-px" aria-hidden="true" />
      </div>
    </div>
  );
}
