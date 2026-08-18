import type { ProjectHealth } from "../../models/health";
import type { ProjectMeta } from "../../models/project";
import { projectHealthState } from "../../state/workspace/projectHealthState";

const TONE_CLASS = {
  green: "bg-accent-green",
  amber: "bg-accent-yellow",
  red: "bg-accent-red",
  grey: "bg-ink-400",
} as const;

/**
 * The one dot beside a project's name. It shows the health monitor's verdict
 * when there is one and the container's lifecycle status otherwise, so the
 * colour always means "is this project all right" rather than two different
 * things depending on which subsystem last spoke.
 *
 * Clicking it opens the project's settings, which lands on the Info tab where
 * the same reasons are spelled out with numbers.
 */
export function ProjectStatusDot({
  project,
  health,
  onOpen,
}: {
  project: ProjectMeta;
  health?: ProjectHealth;
  onOpen?: () => void;
}) {
  const dot = projectHealthState.dot(project, health);
  const pulse = project.status === "provisioning" ? " animate-pulse" : "";
  const mark = (
    <span
      class={`block w-2 h-2 rounded-full ${TONE_CLASS[dot.tone]}${pulse}`}
      aria-hidden="true"
    />
  );

  if (!onOpen) {
    return (
      <span class="flex-none grid place-items-center w-3 h-3" title={dot.title}>
        {mark}
      </span>
    );
  }

  return (
    <button
      type="button"
      onClick={(event) => {
        event.stopPropagation();
        onOpen();
      }}
      class="flex-none grid place-items-center w-3 h-3 rounded-full
             hover:ring-2 hover:ring-white/20 focus:outline-none focus:ring-2 focus:ring-accent-blue"
      title={dot.title}
      aria-label={`${project.name}: ${dot.label}. Open project info.`}
    >
      {mark}
    </button>
  );
}
