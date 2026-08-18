/**
 * A labelled usage bar. Shared by the fleet Resources settings and the
 * per-project Resources panel so "70% of the memory limit" looks the same
 * wherever it is measured.
 *
 * An undefined percent renders an empty track rather than a 0% bar: a stopped
 * container has no usage to report, and drawing zero would read as "idle".
 */
export function Meter({
  label,
  detail,
  percent,
}: {
  label: string;
  detail: string;
  percent?: number;
}) {
  return (
    <div>
      <div class="flex items-baseline justify-between gap-3 text-[12px]">
        <span class="text-ink-200">{label}</span>
        <span class="font-mono tabular-nums text-ink-300">{detail}</span>
      </div>
      <div
        class="mt-1.5 h-2 rounded-full bg-white/[0.06] overflow-hidden"
        role="progressbar"
        aria-label={label}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-valuenow={percent != null ? Math.round(percent) : undefined}
      >
        {percent != null && (
          <div
            class={`h-full rounded-full ${meterTone(percent)}`}
            style={{ width: `${percent}%` }}
          />
        )}
      </div>
    </div>
  );
}

function meterTone(percent: number): string {
  if (percent >= 90) return "bg-accent-red";
  if (percent >= 70) return "bg-accent-orange";
  return "bg-accent-blue";
}
