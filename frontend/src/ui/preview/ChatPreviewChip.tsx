import { useEffect, useRef, useState } from "preact/hooks";
import { PUBLIC_HOSTNAME } from "../../config/runtime.ts";
import type { ProjectMeta } from "../../models/project";
import { useProjectPreviewLinks } from "../../state/hooks/projects/useProjectPreviewLinks.ts";
import { preferredPreviewPort } from "../../state/projects/projectPreviewLinksState.ts";
import { buildProjectPreviewUrl } from "../../shared/projectPreviewUrls.ts";
import { ChevronDown, Globe } from "../primitives/icons";
import { PreviewPopover } from "./PreviewPopover";
import { PreviewPortList } from "./PreviewPortList";

/**
 * Chat header chip pointing at the project's running app. Hidden entirely
 * while nothing shareable is listening, so a chat with no preview keeps the
 * header exactly as it was.
 */
export function ChatPreviewChip({
  project,
  streaming,
  onAgentBrowserOpened,
}: {
  project: ProjectMeta;
  streaming: boolean;
  /** Reveals the chat's Agent Browser pane after a successful navigation. */
  onAgentBrowserOpened?: (port: number) => void;
}) {
  const [open, setOpen] = useState(false);
  const anchorRef = useRef<HTMLDivElement>(null);
  const links = useProjectPreviewLinks({ project, enabled: true, polling: open });
  const { refresh } = links;
  const wasStreaming = useRef(streaming);

  // A finished turn is the moment a dev server most often appears, and it
  // costs one scan rather than a standing poll.
  useEffect(() => {
    const ended = wasStreaming.current && !streaming;
    wasStreaming.current = streaming;
    if (ended) void refresh();
  }, [streaming, refresh]);

  const port = preferredPreviewPort(links.rows);
  if (port === null) return null;
  const url = buildProjectPreviewUrl(project.slug, port, PUBLIC_HOSTNAME);
  if (!url) return null;

  return (
    <div
      ref={anchorRef}
      class="flex flex-none items-stretch overflow-hidden rounded-md border border-white/10 bg-white/5"
    >
      <a
        href={url}
        target="_blank"
        rel="noopener noreferrer"
        class="inline-flex h-8 items-center gap-1.5 px-2 text-[11.5px] font-medium text-ink-200
               transition hover:bg-white/[0.09] hover:text-ink-100"
        title={`Open ${url}`}
      >
        <Globe class="h-3.5 w-3.5 flex-none" aria-hidden="true" />
        <span class="hidden sm:inline">Preview</span>
        <span class="font-mono tabular-nums">:{port}</span>
      </a>
      <button
        type="button"
        onClick={() => setOpen((current) => !current)}
        class={`grid w-6 flex-none place-items-center border-l border-white/10 transition hover:bg-white/[0.09]
                ${open ? "text-accent-blue" : "text-ink-300 hover:text-ink-100"}`}
        aria-label="Preview ports and share links"
        aria-haspopup="dialog"
        aria-expanded={open}
        title="Other ports, copy and share"
      >
        <ChevronDown class="h-3 w-3" />
      </button>
      {open && (
        <PreviewPopover
          anchorRef={anchorRef}
          title={`${project.name} — preview`}
          onClose={() => setOpen(false)}
        >
          <PreviewPortList
            project={project}
            links={links}
            onAgentBrowserOpened={(port) => {
              setOpen(false);
              onAgentBrowserOpened?.(port);
            }}
          />
        </PreviewPopover>
      )}
    </div>
  );
}
