import type { ComponentChildren, RefObject } from "preact";
import type { ChatMeta, ChatStatus, TranscriptIndexProgress } from "../../models/chat";
import type { AgentActivity } from "../../state/hooks/chat/agentActivity";
import type { ProjectMeta } from "../../models/project";
import type { ChatMessageBlock } from "../../models/chatMessage";
import type { ChatPolicies } from "../../state/hooks/chat/useChatPolicies";
import type { ChatFind } from "../../state/hooks/chat/useChatFind";
import { ChatComposer, type ChatComposerProps } from "./composer/ChatComposer";
import { AgentActivityStrip } from "./messages/AgentActivityStrip";
import { ChatFindBar } from "./find/ChatFindBar";
import { JumpToLatestButton } from "./messages/JumpToLatestButton";
import { MessageList } from "./messages/MessageList";
import { ThreadHeader } from "./header/ThreadHeader";
import type { ChatInteractionResponder } from "../../types/chatApi";

export function ChatThread({
  chat,
  project,
  activity,
  find,
  blocks,
  highlightAt,
  hasOlder,
  loadingOlder,
  indexingProgress,
  status,
  error,
  composer,
  policies,
  endpointBadge,
  directBadge,
  showJump,
  scrollRef,
  contentRef,
  bottomRef,
  onHamburger,
  onScroll,
  onJumpToBottom,
  onAnswerQuestion,
  onRespondInteraction,
  onLoadOlder,
  onRewind,
  onSaveSnippet,
  onOpenAgentBrowser,
  onOpenCompanionChat,
  actions,
  projectName,
}: {
  chat: ChatMeta;
  project: ProjectMeta | null;
  /** What the running turn is doing, for the strip and the header pill. */
  activity: AgentActivity;
  find: ChatFind;
  blocks: ChatMessageBlock[];
  /** A message instant to scroll to and flash, or null. */
  highlightAt: number | null;
  hasOlder: boolean;
  loadingOlder: boolean;
  indexingProgress: TranscriptIndexProgress | null;
  status: ChatStatus;
  error: string | null;
  composer: ChatComposerProps;
  policies: ChatPolicies;
  /**
   * Set when this chat runs against a third-party agent endpoint, so the
   * header can say whose model is answering. Null on every ordinary chat.
   */
  endpointBadge?: { short: string; title: string } | null;
  directBadge?: { short: string; title: string } | null;
  showJump: boolean;
  scrollRef: RefObject<HTMLDivElement>;
  contentRef: RefObject<HTMLDivElement>;
  bottomRef: RefObject<HTMLDivElement>;
  onHamburger: () => void;
  onScroll: () => void;
  onJumpToBottom: () => void;
  onAnswerQuestion: (text: string) => void;
  onRespondInteraction?: ChatInteractionResponder;
  onLoadOlder: () => Promise<void>;
  onRewind: (t: number, text: string) => void;
  /** Offers "Save as snippet" on prompts the user wrote. */
  onSaveSnippet?: (text: string) => void;
  onOpenAgentBrowser: () => void;
  /** Opens a team companion chat in the normal chat view. */
  onOpenCompanionChat: (chatId: string) => void;
  /** Workspace controls. Rendered in the header on desktop and in the toolbar
   *  strip below it on mobile — only ever one of the two is visible. */
  actions: ComponentChildren;
  projectName?: string;
}) {
  return (
    <div class="codex-thread flex-1 h-full flex min-h-0 overflow-hidden bg-canvas">
      <div class="flex min-w-0 flex-1 flex-col">
        <ThreadHeader
          chat={chat}
          project={project}
          streaming={composer.streaming}
          activity={activity}
          policies={policies}
          endpointBadge={endpointBadge}
          directBadge={directBadge}
          projectName={projectName}
          actions={actions}
          onHamburger={onHamburger}
          onOpenAgentBrowser={onOpenAgentBrowser}
          onOpenCompanionChat={onOpenCompanionChat}
        />
        <div class="workspace-action-toolbar relative z-30 flex flex-none justify-end border-b border-line px-2.5 py-1.5 md:hidden">
          {actions}
        </div>

        <div class="relative flex-1 min-h-0">
          <MessageList
            status={status}
            blocks={blocks}
            highlightAt={highlightAt}
            hasOlder={hasOlder}
            loadingOlder={loadingOlder}
            indexingProgress={indexingProgress}
            error={error}
            chatId={chat.id}
            cwd={chat.cwd}
            playbooks={composer.playbooks}
            scrollRef={scrollRef}
            contentRef={contentRef}
            bottomRef={bottomRef}
            onScroll={onScroll}
            onAnswerQuestion={onAnswerQuestion}
            onRespondInteraction={onRespondInteraction}
            onLoadOlder={onLoadOlder}
            onRewind={onRewind}
            onSaveSnippet={onSaveSnippet}
          />
          <ChatFindBar find={find} hasUnloadedMessages={hasOlder} />
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
