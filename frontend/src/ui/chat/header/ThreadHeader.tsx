import type { ComponentChildren } from "preact";
import type { ChatMeta } from "../../../models/chat";
import type { AgentActivity } from "../../../state/chat/agentActivity";
import type { ProjectMeta } from "../../../models/project";
import { Menu, MessageSquare } from "../../primitives/icons";
import { ChatPreviewChip } from "../../preview/ChatPreviewChip";
import type { ChatPolicies } from "../../../state/hooks/chat/useChatPolicies";
import { TeamPanel } from "../team/TeamPanel";
import { ChatStatusPill } from "./ChatStatusPill";

/**
 * The chat header is deliberately a two-row layout on narrow viewports and a
 * single row from `sm` up: the title block claims a whole row of its own until
 * there is room beside it, so adding another control (preview, autopilot, …)
 * can never squeeze the title into a two-character stub.
 *
 * The title says what the chat is; `ChatStatusPill` says what it is doing.
 * Those are the only two claims the header makes, and each is made once — with
 * one exception: a chat running team mode also carries the team pill, because
 * "what is this chat doing" and "where is the review up to" are genuinely two
 * different questions once three agents are working on one change.
 *
 * Extra header controls belong in `actions`, which renders in the same
 * right-aligned flex group as the status pill and the preview chip.
 */
export function ThreadHeader({
  chat,
  project,
  streaming,
  activity,
  policies,
  actions,
  onHamburger,
  onOpenAgentBrowser,
  onOpenCompanionChat,
}: {
  chat: ChatMeta;
  project: ProjectMeta | null;
  streaming: boolean;
  /** What the running turn is doing; the pill says the same thing the strip does. */
  activity?: AgentActivity;
  policies?: ChatPolicies;
  /** Optional extra header controls, right-aligned next to the preview chip. */
  actions?: ComponentChildren;
  onHamburger: () => void;
  onOpenAgentBrowser?: () => void;
  /** Opens a team companion chat in the normal chat view. */
  onOpenCompanionChat?: (chatId: string) => void;
}) {
  const title = chat.title || "Untitled chat";

  return (
    <header class="codex-header top-chrome z-20 flex flex-none flex-wrap items-center gap-x-2 gap-y-1.5 border-b border-white/10 bg-[#101318] px-3 py-2 md:bg-[#101318]/95 md:backdrop-blur">
      <div class="codex-thread-heading flex min-h-9 min-w-0 flex-1 basis-full items-center gap-2 sm:basis-0">
        <button
          type="button"
          onClick={onHamburger}
          class="md:hidden h-9 w-9 rounded-md text-ink-100 hover:bg-white/[0.08] grid place-items-center flex-none"
          aria-label="Open chats"
          title="Chats"
        >
          <Menu class="w-5 h-5" />
        </button>

        <div class="hidden sm:grid h-8 w-8 rounded-md bg-white/[0.05] border border-white/10 text-ink-300 place-items-center flex-none">
          <MessageSquare class="w-4 h-4" />
        </div>

        <h1
          dir="auto"
          title={title}
          class="bidi-auto min-w-0 flex-1 truncate text-[14px] font-semibold text-ink-50"
        >
          {title}
        </h1>
      </div>

      <div class="flex w-full flex-none items-center justify-end gap-1.5 sm:ms-auto sm:w-auto">
        <ChatStatusPill
          provider={chat.provider}
          streaming={streaming}
          activity={activity}
          autopilot={policies?.autopilot}
          busy={policies?.busy}
          onStopAutopilot={policies?.stopAutopilot}
        />
        {policies?.team && onOpenCompanionChat && (
          <TeamPanel
            view={policies.team}
            busy={policies.busy}
            onOpenChat={onOpenCompanionChat}
            onStop={policies.stopTeam}
          />
        )}
        {actions}
        {project && (
          <ChatPreviewChip
            project={project}
            streaming={streaming}
            onAgentBrowserOpened={onOpenAgentBrowser}
          />
        )}
      </div>
    </header>
  );
}
