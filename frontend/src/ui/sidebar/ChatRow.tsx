import type { ChatMeta } from "../../models/chat";
import { modelShortLabel } from "../../config/chat";
import { PHASE_ABBREVIATION } from "../../state/chat/agentActivity";
import { useLiveChatPhase } from "../../state/hooks/chat/useAgentActivity";
import { timeAgo } from "../../shared/format";
import { Clock, Eye, EyeOff, GitFork, MessageSquare, X } from "../primitives/icons";

export function ChatRow({
  chat,
  active,
  onSelect,
  onDelete,
  onToggleUnread,
  onFork,
}: {
  chat: ChatMeta;
  active: boolean;
  onSelect: () => void;
  onDelete: (event: Event) => void;
  onToggleUnread: (event: Event) => void;
  onFork: (event: Event) => void;
}) {
  const rawUnread = (chat.lastMessageAt || 0) > (chat.lastReadAt || 0);
  const unread = !active && !chat.running && rawUnread;
  // Only the open chat holds a socket, so only it can say which phase a run is
  // in. Every other running chat keeps the honest, unqualified "running".
  const live = useLiveChatPhase();
  const phase = live?.chatId === chat.id ? PHASE_ABBREVIATION[live.phase] : "";

  return (
    <div
      class={`group flex items-stretch gap-0.5 rounded transition-colors
              ${active
                ? "bg-accent-blue/[0.14] border border-accent-blue/[0.32]"
                : "border border-transparent hover:bg-white/[0.04]"}`}
    >
      <button
        type="button"
        onClick={onSelect}
        class="flex-1 min-w-0 text-left px-2.5 py-2"
      >
        <div class="flex items-start gap-2">
          {chat.running ? (
            <span
              class="mt-0.5 w-3.5 h-3.5 flex-none grid place-items-center"
              title={phase ? `Running — ${phase}` : "Running"}
            >
              <span class="w-2.5 h-2.5 rounded-full bg-accent-blue animate-pulse shadow-[0_0_0_3px_rgba(93,157,255,0.14)]" />
            </span>
          ) : unread ? (
            <span class="mt-0.5 w-3.5 h-3.5 flex-none grid place-items-center" title="Unread">
              <span class="w-2.5 h-2.5 rounded-full bg-accent-green shadow-[0_0_0_3px_rgba(43,213,118,0.12)]" />
            </span>
          ) : (
            <MessageSquare
              class={`mt-0.5 w-3.5 h-3.5 flex-none ${active ? "text-accent-blue" : "text-ink-400"}`}
            />
          )}
          <div class="flex-1 min-w-0">
            <div
              dir="auto"
              title={chat.title || "Untitled"}
              class={`bidi-auto text-[13px] leading-snug truncate ${active ? "text-ink-50 font-medium" : "text-ink-100"}`}
            >
              {chat.title || "Untitled"}
            </div>
            <div class="mt-0.5 flex items-center gap-1.5 text-[11px] text-ink-300">
              <span class={`px-1 py-0.5 rounded bg-white/[0.06] text-[10px] leading-none whitespace-nowrap flex-none ${active ? "text-accent-blue" : ""}`}>
                {modelShortLabel(chat.model)}
              </span>
              {chat.running && phase ? (
                <span class="truncate text-accent-blue">{phase}</span>
              ) : (
                <>
                  <Clock class="w-3 h-3 flex-none text-ink-400" aria-hidden="true" />
                  <span class="truncate tabular-nums">{timeAgo(chat.lastMessageAt)}</span>
                </>
              )}
            </div>
          </div>
        </div>
      </button>
      <button
        type="button"
        onClick={onToggleUnread}
        class="hover-actions w-8 grid place-items-center text-ink-300 hover:text-ink-50 hover:bg-white/[0.08]"
        aria-label={rawUnread ? `Mark ${chat.title || "chat"} read` : `Mark ${chat.title || "chat"} unread`}
        title={rawUnread ? "Mark read" : "Mark unread"}
      >
        {rawUnread ? <Eye class="w-3.5 h-3.5" /> : <EyeOff class="w-3.5 h-3.5" />}
      </button>
      <button
        type="button"
        onClick={onFork}
        class="hover-actions w-8 grid place-items-center text-ink-300 hover:text-accent-blue hover:bg-white/[0.08]"
        aria-label={`Fork ${chat.title || "chat"}`}
        title="Fork from last message"
      >
        <GitFork class="w-3.5 h-3.5" />
      </button>
      <button
        type="button"
        onClick={onDelete}
        class="hover-actions w-8 grid place-items-center rounded-r text-ink-300 hover:text-accent-red hover:bg-accent-red/10"
        aria-label={`Delete ${chat.title || "chat"}`}
        title="Delete chat"
      >
        <X class="w-3.5 h-3.5" />
      </button>
    </div>
  );
}
