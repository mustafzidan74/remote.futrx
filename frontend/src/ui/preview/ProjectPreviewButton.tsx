import { useRef, useState } from "preact/hooks";
import type { ProjectMeta } from "../../models/project";
import { useProjectPreviewLinks } from "../../state/hooks/projects/useProjectPreviewLinks.ts";
import { Globe } from "../primitives/icons";
import { PreviewPopover } from "./PreviewPopover";
import { PreviewPortList } from "./PreviewPortList";

/**
 * Sidebar quick action: one click from any project row to its running app's
 * preview URL, without going through the project settings page.
 */
export function ProjectPreviewButton({ project }: { project: ProjectMeta }) {
  const [open, setOpen] = useState(false);
  const buttonRef = useRef<HTMLButtonElement>(null);
  // Scanning ports costs an `lxc exec` per call, so nothing is fetched until
  // the popover is actually open.
  const links = useProjectPreviewLinks({ project, enabled: open, polling: open });

  return (
    <>
      <button
        ref={buttonRef}
        type="button"
        onClick={(event) => {
          event.stopPropagation();
          setOpen((current) => !current);
        }}
        class={`grid w-8 place-items-center rounded transition hover:bg-white/[0.08]
                ${open ? "text-accent-blue" : "text-ink-300 hover:text-ink-50"}`}
        aria-label={`Preview links for ${project.name}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        title="Preview links"
      >
        <Globe class="h-4 w-4" />
      </button>
      {open && (
        <PreviewPopover
          anchorRef={buttonRef}
          title={`${project.name} — preview`}
          onClose={() => setOpen(false)}
        >
          <PreviewPortList project={project} links={links} />
        </PreviewPopover>
      )}
    </>
  );
}
