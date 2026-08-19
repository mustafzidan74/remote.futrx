import type { RoutingUsageSummary } from "../../../models/modelRouting";
import { cheapShare, savingsNote } from "../../../state/settings/modelRoutingState";
import { formatUsd } from "../../../state/usage/usageChartModel";
import { Zap } from "../../primitives/icons";

/**
 * What automatic model routing did this period, and what it plausibly cost or
 * saved.
 *
 * The money here is a counterfactual: nobody ran these tokens through the
 * default model, so the baseline is priced from the editable table in
 * `prices.json`. The card says so in three places rather than one, because a
 * number this shaped is exactly the kind that gets quoted out of context.
 */
export function AutoRoutingCard({ summary }: { summary: RoutingUsageSummary }) {
  const share = cheapShare(summary);
  const saved = summary.estimatedSavedUsd;
  const savedTone = saved >= 0 ? "text-accent-green" : "text-accent-orange";

  return (
    <section class="rounded-lg border border-white/10 bg-[#101318] px-4 py-3">
      <div class="flex flex-wrap items-center gap-2">
        <Zap class="h-4 w-4 flex-none text-accent-blue" aria-hidden="true" />
        <div class="text-[14.5px] font-semibold text-ink-50">Auto routing</div>
        {!summary.enabled && (
          <span class="rounded-full bg-white/[0.07] px-2 py-0.5 text-[11px] text-ink-300">
            currently off
          </span>
        )}
      </div>

      {summary.routedRuns === 0 ? (
        <p class="mt-2 text-[12.5px] leading-relaxed text-ink-300">
          No runs were routed automatically in this period. Turn routing on in{" "}
          <span class="text-ink-100">Settings → Model routing</span>, then switch a chat's model
          pill to Auto.
        </p>
      ) : (
        <>
          <div class="mt-3 grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <Stat label="Routed runs" value={String(summary.routedRuns)} />
            <Stat
              label="Cheap"
              value={String(summary.cheapRuns)}
              detail={summary.cheapModel}
            />
            <Stat
              label="Expensive"
              value={String(summary.expensiveRuns)}
              detail={summary.expensiveModel}
            />
            <Stat
              label="Estimated saved"
              value={formatUsd(saved)}
              detail={`vs ${summary.defaultModel || "the default model"}`}
              tone={savedTone}
            />
          </div>

          {share !== null && (
            <div class="mt-3">
              <div
                class="h-1.5 w-full overflow-hidden rounded-full bg-white/[0.08]"
                role="img"
                aria-label={`${Math.round(share * 100)} percent of routed runs went to the cheap model`}
              >
                <div
                  class="h-full rounded-full bg-accent-blue"
                  style={{ width: `${Math.round(share * 100)}%` }}
                />
              </div>
              <div class="mt-1 text-[11.5px] text-ink-300">
                {Math.round(share * 100)}% of routed runs went cheap ·{" "}
                {formatUsd(summary.routedCostUsd)} actual vs{" "}
                {formatUsd(summary.baselineCostUsd)} baseline
              </div>
            </div>
          )}

          <p class="mt-2 text-[11.5px] leading-relaxed text-ink-300">{savingsNote(summary)}</p>

          {summary.topRules.length > 0 && (
            <div class="mt-3">
              <div class="text-[11.5px] uppercase tracking-wide text-ink-300">Top rules</div>
              <ul class="mt-1.5 space-y-1">
                {summary.topRules.map((rule) => (
                  <li
                    key={rule.ruleId}
                    class="flex items-center justify-between gap-3 rounded-md bg-white/[0.03] px-2.5 py-1.5"
                  >
                    <span class="min-w-0 truncate text-[12.5px] text-ink-100" title={rule.ruleId}>
                      {rule.label}
                    </span>
                    <span class="flex-none text-[12px] tabular-nums text-ink-300">
                      {rule.runs} {rule.runs === 1 ? "run" : "runs"} · {formatUsd(rule.costUsd)}
                    </span>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
    </section>
  );
}

function Stat({
  label,
  value,
  detail,
  tone = "text-ink-50",
}: {
  label: string;
  value: string;
  detail?: string;
  tone?: string;
}) {
  return (
    <div class="rounded-md border border-white/10 bg-white/[0.03] px-3 py-2">
      <div class="text-[11.5px] uppercase tracking-wide text-ink-300">{label}</div>
      <div class={`mt-0.5 text-[19px] font-semibold tabular-nums ${tone}`}>{value}</div>
      {detail && <div class="mt-0.5 truncate text-[11.5px] text-ink-300">{detail}</div>}
    </div>
  );
}
