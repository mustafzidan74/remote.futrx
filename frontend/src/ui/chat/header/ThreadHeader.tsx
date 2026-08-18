import type { ComponentChildren } from "preact";
import type { ChatMeta } from "../../../models/chat";
import type { ProjectMeta } from "../../../models/project";
import { providerDisplayLabel } from "../../../config/chat";
import { Menu, MessageSquare } from "../../primitives/icons";
import { ChatPreviewChip } from "../../preview/ChatPreviewChip";

/**
 * The chat header is deliberately a two-row layout on narrow viewports and a
 * single row from `sm` up: the title block claims a whole row of its own until
 * there is room beside it, so adding another control (preview, autopilot, …)
 * can never squeeze the title into a two-character stub.
 *
 * Extra header controls belong in `actions`, which renders in the same
 * right-aligned flex group as the preview chip and inherits its gap.
 */
export function ThreadHeader({
  chat,
  project,
  streaming,
  actions,
  onHamburger,
  onOpenAgentBrowser,
}: {
  chat: ChatMeta;
  project: ProjectMeta | null;
  streaming: boolean;
  /** Optional extra header controls, right-aligned next to the preview chip. */
  actions?: ComponentChildren;
  onHamburger: () => void;
  onOpenAgentBrowser?: () => void;
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
          <MessageSquare class="w-3.5 h-3.5" />
        </div>

        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-2 min-w-0">
            <h1 dir="auto" title={title} class="bidi-auto truncate text-[14px] font-semibold text-ink-50">
              {title}
            </h1>
            <span
              class={`h-1.5 w-1.5 rounded-full flex-none ${streaming ? "bg-accent-green animate-pulse" : "bg-ink-400"}`}
              title={streaming ? "Streaming" : "Ready"}
            />
          </div>
          <div class="text-[11px] leading-4 text-ink-300 truncate">
            {providerDisplayLabel(chat.provider)} · {streaming ? "Working" : "Ready"}
          </div>
        </div>
      </div>

      {(actions || project) && (
        <div class="flex w-full flex-none items-center justify-end gap-1.5 empty:hidden sm:ms-auto sm:w-auto">
          {actions}
          {project && (
            <ChatPreviewChip
              project={project}
              streaming={streaming}
              onAgentBrowserOpened={onOpenAgentBrowser}
            />
          )}
        </div>
      )}
    </header>
  );
}
