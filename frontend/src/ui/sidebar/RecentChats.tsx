import type { ChatMeta } from "../../models/chat";
import type { ProjectMeta } from "../../models/project";
import { timeAgo } from "../../shared/format";
import { ChevronDown, ChevronRight, Clock, Loader } from "../primitives/icons";

/**
 * "Jump back in": the newest chats across every project, above the tree.
 *
 * Once work is spread over several projects, finding the conversation you had
 * an hour ago means remembering which project it was in and expanding that
 * group. This strip skips both steps; it stands down while a search is running
 * because the search already answers the same question better.
 */
export function RecentChats({
  chats,
  projects,
  activeChatId,
  open,
  onToggle,
  onSelectChat,
}: {
  chats: ChatMeta[];
  projects: ProjectMeta[];
  activeChatId: string | null;
  open: boolean;
  onToggle: () => void;
  onSelectChat: (chatId: string) => void;
}) {
  if (chats.length === 0) return null;
  const projectNames = new Map(projects.map((project) => [project.id, project.name]));

  return (
    <section class="rounded-lg" aria-label="Recent chats">
      <button
        type="button"
        onClick={onToggle}
        class="flex h-8 w-full items-center gap-1.5 rounded-md px-2 text-ink-300 transition
               hover:bg-white/[0.04] hover:text-ink-100"
        aria-expanded={open}
        aria-controls="sidebar-recent-chats"
      >
        {open ? <ChevronDown class="h-3.5 w-3.5 flex-none" /> : <ChevronRight class="h-3.5 w-3.5 flex-none" />}
        <span class="text-[10.5px] font-semibold uppercase tracking-wider">Recent</span>
        <span class="ms-auto text-[11px] tabular-nums text-ink-400">{chats.length}</span>
      </button>

      {open && (
        <div id="sidebar-recent-chats" class="mt-0.5 space-y-0.5">
          {chats.map((chat) => {
            const active = chat.id === activeChatId;
            const unread =
              !active && !chat.running && (chat.lastMessageAt || 0) > (chat.lastReadAt || 0);
            return (
              <button
                key={chat.id}
                type="button"
                onClick={() => onSelectChat(chat.id)}
                class={`flex w-full items-center gap-2 rounded-md px-2 py-1.5 text-left transition
                        ${active ? "bg-accent-blue/[0.14] text-ink-50" : "text-ink-100 hover:bg-white/[0.05]"}`}
              >
                {chat.running ? (
                  <Loader class="h-3 w-3 flex-none animate-spin text-accent-blue" />
                ) : (
                  <span
                    class={`h-2 w-2 flex-none rounded-full ${unread ? "bg-accent-green" : "bg-white/15"}`}
                    title={unread ? "Unread" : undefined}
                  />
                )}
                <span class="min-w-0 flex-1">
                  <span
                    dir="auto"
                    class="bidi-auto block truncate text-[12.5px] leading-tight"
                    title={chat.title || "Untitled"}
                  >
                    {chat.title || "Untitled"}
                  </span>
                  <span class="mt-0.5 flex items-center gap-1 text-[10.5px] text-ink-400">
                    <span class="truncate">
                      {chat.projectId ? projectNames.get(chat.projectId) ?? "Project" : "No project"}
                    </span>
                    <Clock class="h-2.5 w-2.5 flex-none" aria-hidden="true" />
                    <span class="tabular-nums whitespace-nowrap">{timeAgo(chat.lastMessageAt)}</span>
                  </span>
                </span>
              </button>
            );
          })}
        </div>
      )}
    </section>
  );
}
