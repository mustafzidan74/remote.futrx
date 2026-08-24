import type { ComponentChildren } from "preact";
import type { ChatMeta } from "../../../models/chat";
import type { AgentActivity } from "../../../state/chat/agentActivity";
import type { ProjectMeta } from "../../../models/project";
import { useState } from "preact/hooks";
import { chatApi } from "../../../api/chatApi";
import { useAuxModelJob } from "../../../state/hooks/settings/useAuxModelJobs";
import { Globe, Loader, Menu, MessageSquare, RotateCcw } from "../../primitives/icons";
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
  endpointBadge,
  directBadge,
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
  /**
   * Set when this chat runs against a third-party agent endpoint. Null — the
   * default — means the vendor's own, and the header says nothing.
   */
  endpointBadge?: { short: string; title: string } | null;
  /**
   * Set when this chat is answered by a completion-API model rather than an
   * agent. It reads as information, not as a warning: nothing is wrong, the
   * chat simply cannot touch files, and an operator who asked for an edit
   * needs to see why they got an explanation instead.
   */
  directBadge?: { short: string; title: string } | null;
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
          class="bidi-auto min-w-0 truncate text-[14px] font-semibold text-ink-50"
        >
          {title}
        </h1>
        <RegenerateTitleButton chatId={chat.id} />
        <span class="flex-1" />
      </div>

      <div class="flex w-full flex-none items-center justify-end gap-1.5 sm:ms-auto sm:w-auto">
        {endpointBadge && <ThirdPartyBadge badge={endpointBadge} />}
        {directBadge && <AnswerOnlyBadge badge={directBadge} />}
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

/**
 * The red badge on a chat running against a third-party endpoint.
 *
 * It exists so nobody mistakes whose model produced a piece of client code.
 * That is a commercial fact, not a technical one: a landing page written by
 * GLM must not be handed over as Claude's work, and the person who opens the
 * chat two weeks later has no other way to know. It is therefore loud, it
 * carries the word "not Anthropic" in full, and it sits first in the header
 * group rather than behind an overflow.
 */
function ThirdPartyBadge({ badge }: { badge: { short: string; title: string } }) {
  return (
    <span
      class="flex h-8 flex-none items-center gap-1.5 rounded-full border border-accent-red/40
             bg-accent-red/[0.14] px-2.5 text-[11.5px] font-semibold text-accent-red"
      title={badge.title}
      role="status"
    >
      <Globe class="h-3.5 w-3.5 flex-none" aria-hidden="true" />
      <span class="hidden max-w-[11rem] truncate sm:inline">{badge.short}</span>
      <span class="sm:hidden">3rd-party</span>
      <span class="sr-only">{badge.title}</span>
    </span>
  );
}

/**
 * The answer-only badge. Green rather than red on purpose: a chat pointed at a
 * free model is working as chosen, and the badge is telling the operator what
 * it can do, not warning them about it.
 */
function AnswerOnlyBadge({ badge }: { badge: { short: string; title: string } }) {
  return (
    <span
      class="flex h-8 flex-none items-center gap-1.5 rounded-full border border-accent-green/40
             bg-accent-green/[0.12] px-2.5 text-[11.5px] font-semibold text-accent-green"
      title={badge.title}
      role="status"
    >
      <MessageSquare class="h-3.5 w-3.5 flex-none" aria-hidden="true" />
      <span class="hidden max-w-[12rem] truncate sm:inline">{badge.short}</span>
      <span class="sm:hidden">no tools</span>
      <span class="sr-only">{badge.title}</span>
    </span>
  );
}

/**
 * "Rename this chat" — one press, and the auxiliary model writes a 3–6 word
 * title in the language the first prompt was written in.
 *
 * The button renders only where it can work: a server with no auxiliary model,
 * or one whose chat-title job is switched off, simply does not have it. The
 * refreshed title arrives through the workspace socket like every other chat
 * change, so nothing here has to hold state beyond "am I waiting".
 */
function RegenerateTitleButton({ chatId }: { chatId: string }) {
  const available = useAuxModelJob("chatTitle");
  const [busy, setBusy] = useState(false);
  const [failed, setFailed] = useState<string | null>(null);

  if (!available) return null;

  async function rename() {
    setBusy(true);
    setFailed(null);
    try {
      await chatApi.regenerateTitle(chatId);
    } catch (cause) {
      setFailed((cause as Error).message);
    } finally {
      setBusy(false);
    }
  }

  return (
    <button
      type="button"
      onClick={() => void rename()}
      disabled={busy}
      aria-label="Rename this chat with the auxiliary model"
      title={failed ? `Rename failed: ${failed}` : "Rename with the auxiliary model"}
      class={`grid h-7 w-7 flex-none place-items-center rounded-md text-ink-400 transition
              hover:bg-white/[0.08] hover:text-ink-100 disabled:opacity-50
              ${failed ? "text-accent-red" : ""}`}
    >
      {busy ? <Loader class="h-3.5 w-3.5 animate-spin" /> : <RotateCcw class="h-3.5 w-3.5" />}
    </button>
  );
}
