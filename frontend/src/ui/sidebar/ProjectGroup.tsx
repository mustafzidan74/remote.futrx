import type { ChatMeta } from "../../models/chat";
import type { ProjectHealth } from "../../models/health";
import type { ProjectMeta } from "../../models/project";
import { ChevronDown, ChevronRight, Loader, Plus, Settings } from "../primitives/icons";
import { ProjectPreviewButton } from "../preview/ProjectPreviewButton";
import { ChatRow } from "./ChatRow";
import { ProjectStatusDot } from "./ProjectStatusDot";

export function ProjectGroup({
  project,
  health,
  chats,
  visibleChats,
  activeChatId,
  collapsed,
  onToggle,
  onNewChat,
  onOpenContainer,
  onSelectChat,
  onDeleteChat,
  onToggleChatUnread,
  onForkChat,
  draggable,
  dragging,
  dragOver,
  onDragStart,
  onDragOver,
  onDrop,
  onDragEnd,
}: {
  project: ProjectMeta;
  health?: ProjectHealth;
  chats: ChatMeta[];
  visibleChats: ChatMeta[];
  activeChatId: string | null;
  collapsed: boolean;
  onToggle: () => void;
  onNewChat: () => void;
  onOpenContainer: () => void;
  onSelectChat: (chatId: string) => void;
  onDeleteChat: (chat: ChatMeta, event: Event) => void;
  onToggleChatUnread: (chat: ChatMeta, event: Event) => void;
  onForkChat: (chat: ChatMeta, event: Event) => void;
  draggable?: boolean;
  dragging?: boolean;
  dragOver?: boolean;
  onDragStart?: (event: DragEvent) => void;
  onDragOver?: (event: DragEvent) => void;
  onDrop?: (event: DragEvent) => void;
  onDragEnd?: (event: DragEvent) => void;
}) {
  const provisioning = project.status === "provisioning";
  const hasUnread = chats.some((chat) => (chat.lastMessageAt || 0) > (chat.lastReadAt || 0));

  return (
    <div
      class={`min-h-0 rounded-lg transition ${dragging ? "opacity-55" : ""} ${dragOver ? "ring-1 ring-accent-blue/60 bg-accent-blue/[0.08]" : ""}`}
      draggable={draggable}
      onDragStart={onDragStart as any}
      onDragOver={onDragOver as any}
      onDrop={onDrop as any}
      onDragEnd={onDragEnd as any}
    >
      {/*
        The resting row is only "status dot, name, slug" — the settings, preview
        and new-chat controls float over the right edge and appear on hover or
        keyboard focus, and stay permanently visible where there is no hover
        (see `.sidebar-project-row-actions` in index.css).
      */}
      <div class="group relative flex items-stretch gap-0.5 rounded-md hover:bg-white/[0.04]">
        <button
          type="button"
          onClick={onToggle}
          class="w-7 flex-none grid place-items-center text-ink-300 hover:text-ink-100"
          aria-label={collapsed ? `Collapse ${project.name}` : `Expand ${project.name}`}
          aria-expanded={!collapsed}
          title={collapsed ? "Expand" : "Collapse"}
        >
          {collapsed ? <ChevronRight class="w-4 h-4" /> : <ChevronDown class="w-4 h-4" />}
        </button>
        <div class="sidebar-project-row-label flex-1 min-w-0 py-2 pe-1 flex items-center gap-2">
          <ProjectStatusDot project={project} health={health} onOpen={onOpenContainer} />
          {hasUnread && (
            <span class="flex-none w-2 h-2 rounded-full bg-accent-green animate-pulse shadow-[0_0_0_3px_rgba(43,213,118,0.12)]" title="Unread chats" />
          )}
          <div class="flex-1 min-w-0">
            <div
              dir="auto"
              title={project.name}
              class="bidi-auto text-[13.5px] leading-tight truncate font-medium text-ink-100"
            >
              {project.name}
            </div>
            <div class="text-[11px] text-ink-300 truncate font-mono" title={project.slug}>
              {project.slug}
              {chats.length > 0 && (
                <span class="ms-1.5 tabular-nums">
                  · {chats.length} chat{chats.length === 1 ? "" : "s"}
                </span>
              )}
            </div>
          </div>
        </div>
        <div class="sidebar-project-row-actions hover-actions">
          <button
            type="button"
            onClick={(event) => {
              event.stopPropagation();
              onOpenContainer();
            }}
            class="h-10 w-10 rounded-md grid place-items-center text-ink-300 hover:text-ink-50 hover:bg-white/[0.08]"
            aria-label={`Open container info for ${project.name}`}
            title="Project settings"
          >
            <Settings class="w-4 h-4" />
          </button>
          <ProjectPreviewButton project={project} />
          <button
            type="button"
            onClick={(event) => {
              event.stopPropagation();
              onNewChat();
            }}
            disabled={provisioning}
            class="h-10 w-10 grid place-items-center text-ink-300 hover:text-ink-50 hover:bg-white/[0.08]
                   rounded-md disabled:opacity-40 disabled:cursor-not-allowed"
            aria-label={`New chat in ${project.name}`}
            title={provisioning ? "Project is still provisioning" : "New chat in this project"}
          >
            {provisioning ? <Loader class="w-4 h-4 animate-spin" /> : <Plus class="w-4 h-4" />}
          </button>
        </div>
      </div>

      {project.errorMsg && (
        <div dir="ltr" class="bidi-ltr ms-7 mt-1 me-1 text-[11px] text-accent-red bg-accent-red/10 border border-accent-red/30 rounded-md px-2 py-1.5 break-words font-mono">
          {project.errorMsg}
        </div>
      )}

      {!collapsed && (
        <div class="sidebar-project-chat-list ml-5 pl-2 pr-1 mt-1 space-y-0.5 border-l border-white/[0.08] overflow-y-auto touch-scroll scrollbar-thin">
          {visibleChats.length === 0 ? (
            <button
              type="button"
              onClick={onNewChat}
              disabled={provisioning}
              class="ml-2 mt-1 mb-2 inline-flex items-center gap-1.5 h-7 px-2 rounded
                     text-[12px] text-ink-300 hover:text-ink-100 hover:bg-white/[0.06]
                     disabled:opacity-40 disabled:cursor-not-allowed"
            >
              <Plus class="w-3.5 h-3.5" /> New chat
            </button>
          ) : (
            visibleChats.map((chat) => (
              <ChatRow
                key={chat.id}
                chat={chat}
                active={chat.id === activeChatId}
                onSelect={() => onSelectChat(chat.id)}
                onDelete={(event) => onDeleteChat(chat, event)}
                onToggleUnread={(event) => onToggleChatUnread(chat, event)}
                onFork={(event) => onForkChat(chat, event)}
              />
            ))
          )}
        </div>
      )}
    </div>
  );
}
