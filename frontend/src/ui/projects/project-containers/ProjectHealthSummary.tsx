import type { ProjectHealth } from "../../../models/health";
import { projectHealthState } from "../../../state/workspace/projectHealthState";
import { formatSize } from "../../../state/settings/resourcePolicyState";
import { Meter } from "../../primitives/Meter";

const TONE: Record<string, string> = {
  ok: "text-accent-green bg-accent-green/[0.12]",
  warn: "text-accent-yellow bg-accent-yellow/[0.12]",
  crit: "text-accent-red bg-accent-red/[0.12]",
  unknown: "text-ink-300 bg-white/[0.06]",
};

const LABEL: Record<string, string> = {
  ok: "healthy",
  warn: "degraded",
  crit: "critical",
  unknown: "health unknown",
};

/**
 * The health pill in the project header: the same verdict the sidebar dot
 * shows, worded rather than coloured, so the two surfaces never disagree.
 */
export function ProjectHealthBadge({ health }: { health?: ProjectHealth }) {
  if (!health) return null;
  const status = health.status ?? "unknown";
  return (
    <span
      class={`inline-flex items-center h-5 px-1.5 rounded text-[11px] font-medium ${
        TONE[status] ?? TONE.unknown
      }`}
      title={(health.reasons ?? []).join("\n")}
    >
      {LABEL[status] ?? LABEL.unknown}
    </span>
  );
}

/**
 * The monitor's numbers under the project header: the memory bar it decides
 * on, plus every reason behind a non-healthy verdict. It reuses the same Meter
 * as the Resources panel so "94% of the memory limit" looks identical wherever
 * it is measured.
 */
export function ProjectHealthMeters({ health }: { health?: ProjectHealth }) {
  if (!health || health.status === "unknown") return null;

  const detail =
    health.memoryUsedBytes && health.memoryLimitBytes
      ? `${formatSize(health.memoryUsedBytes)} of ${formatSize(health.memoryLimitBytes)}`
      : "not reported";
  const extras = [
    health.cpuPct != null ? `CPU ${health.cpuPct}%` : "",
    health.diskUsedPct != null ? `disk ${health.diskUsedPct}%` : "",
    health.previewOk === true ? "preview answering" : "",
    health.previewOk === false ? "preview not answering" : "",
  ].filter(Boolean);

  return (
    <div class="mt-2 space-y-1.5">
      <Meter
        label="Memory"
        detail={detail}
        percent={projectHealthState.memoryPercent(health)}
      />
      {(health.reasons ?? []).length > 0 && (
        <ul class="text-[11.5px] text-ink-300 leading-relaxed">
          {(health.reasons ?? []).map((reason) => (
            <li key={reason}>· {reason}</li>
          ))}
        </ul>
      )}
      {extras.length > 0 && (
        <div class="text-[11px] text-ink-400">{extras.join(" · ")}</div>
      )}
    </div>
  );
}
