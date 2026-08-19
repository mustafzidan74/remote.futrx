import type { ComponentChildren, RefObject } from "preact";
import type { ChatMeta, ChatStatus } from "../../models/chat";
import type { AgentActivity } from "../../state/chat/agentActivity";
import type { ProjectMeta } from "../../models/project";
import type { ChatMessageBlock } from "../../models/chatMessage";
import type { ChatPolicies } from "../../state/hooks/chat/useChatPolicies";
import { ChatComposer, type ChatComposerProps } from "./composer/ChatComposer";
import { AgentActivityStrip } from "./messages/AgentActivityStrip";
import { JumpToLatestButton } from "./messages/JumpToLatestButton";
import { MessageList } from "./messages/MessageList";
import { ThreadHeader } from "./header/ThreadHeader";

export function ChatThread({
  chat,
  project,
  activity,
  blocks,
  highlightAt,
  hasOlder,
  loadingOlder,
  status,
  error,
  composer,
  policies,
  showJump,
  scrollRef,
  contentRef,
  bottomRef,
  onHamburger,
  onScroll,
  onJumpToBottom,
  onAnswerQuestion,
  onLoadOlder,
  onRewind,
  onSaveSnippet,
  onOpenAgentBrowser,
  onOpenCompanionChat,
  mobileToolbar,
}: {
  chat: ChatMeta;
  project: ProjectMeta | null;
  /** What the running turn is doing, for the strip and the header pill. */
  activity: AgentActivity;
  blocks: ChatMessageBlock[];
  /** A message instant to scroll to and flash, or null. */
  highlightAt: number | null;
  hasOlder: boolean;
  loadingOlder: boolean;
  status: ChatStatus;
  error: string | null;
  composer: ChatComposerProps;
  policies: ChatPolicies;
  showJump: boolean;
  scrollRef: RefObject<HTMLDivElement>;
  contentRef: RefObject<HTMLDivElement>;
  bottomRef: RefObject<HTMLDivElement>;
  onHamburger: () => void;
  onScroll: () => void;
  onJumpToBottom: () => void;
  onAnswerQuestion: (text: string) => void;
  onLoadOlder: () => Promise<void>;
  onRewind: (t: number, text: string) => void;
  /** Offers "Save as snippet" on prompts the user wrote. */
  onSaveSnippet?: (text: string) => void;
  onOpenAgentBrowser: () => void;
  /** Opens a team companion chat in the normal chat view. */
  onOpenCompanionChat: (chatId: string) => void;
  mobileToolbar: ComponentChildren;
}) {
  return (
    <div class="codex-thread flex-1 h-full flex min-h-0 overflow-hidden bg-[#0b0d11]">
      <div class="flex min-w-0 flex-1 flex-col">
        <ThreadHeader
          chat={chat}
          project={project}
          streaming={composer.streaming}
          activity={activity}
          policies={policies}
          onHamburger={onHamburger}
          onOpenAgentBrowser={onOpenAgentBrowser}
          onOpenCompanionChat={onOpenCompanionChat}
        />
        {mobileToolbar}

        <div class="relative flex-1 min-h-0">
          <MessageList
            status={status}
            blocks={blocks}
            highlightAt={highlightAt}
            hasOlder={hasOlder}
            loadingOlder={loadingOlder}
            error={error}
            chatId={chat.id}
            cwd={chat.cwd}
            playbooks={composer.playbooks}
            scrollRef={scrollRef}
            contentRef={contentRef}
            bottomRef={bottomRef}
            onScroll={onScroll}
            onAnswerQuestion={onAnswerQuestion}
            onLoadOlder={onLoadOlder}
            onRewind={onRewind}
            onSaveSnippet={onSaveSnippet}
          />
          {showJump && <JumpToLatestButton onClick={onJumpToBottom} />}
        </div>

        {/*
          Pinned between the transcript and the composer rather than inside the
          scroller: a live narration that scrolls out of view answers nothing.
        */}
        <AgentActivityStrip
          activity={activity}
          streaming={composer.streaming}
          onCancel={composer.onCancel}
        />

        <ChatComposer {...composer} />
      </div>
    </div>
  );
}
