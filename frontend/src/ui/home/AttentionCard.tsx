import type { DashboardAlert } from "../../models/dashboard";
import { describeAlert, summarizeAlerts } from "../../state/home/dashboardState";
import { CardEmpty, CardSkeleton, DashboardCard, ToneDot } from "./DashboardCard";
import { AlertCircle, Check } from "../primitives/icons";

/**
 * The Attention column: everything wrong with the box, worst first, each with
 * the one action that resolves it. Nothing here is a notification the user
 * has to go and interpret elsewhere — the fix is the row.
 */
export function AttentionCard({
  id,
  alerts,
  loading,
  onAct,
}: {
  id?: string;
  alerts: DashboardAlert[];
  loading: boolean;
  onAct: (alert: DashboardAlert) => void;
}) {
  const summary = summarizeAlerts(alerts);

  return (
    <DashboardCard
      id={id}
      title="Attention"
      subtitle={summary.headline}
      Icon={summary.total === 0 ? Check : AlertCircle}
    >
      {loading ? (
        <CardSkeleton rows={2} />
      ) : alerts.length === 0 ? (
        <CardEmpty>
          Nothing needs a decision. Projects are healthy, backups and snapshots are current, and
          the platform is answering.
        </CardEmpty>
      ) : (
        <ul class="divide-y divide-white/[0.06]">
          {alerts.map((alert) => {
            const view = describeAlert(alert);
            return (
              <li key={alert.id} class="flex items-start gap-3 px-4 py-3">
                <span class="mt-1">
                  <ToneDot tone={view.tone} title={view.severityLabel} />
                </span>
                <div class="min-w-0 flex-1">
                  <div class="text-[13px] font-medium leading-snug text-ink-50">{alert.title}</div>
                  {alert.detail && (
                    <p class="mt-0.5 text-[12px] leading-relaxed text-ink-300">{alert.detail}</p>
                  )}
                  {/* The severity is spelled out, never left to the dot's
                      colour alone. */}
                  <span class="sr-only">{view.severityLabel}</span>
                </div>
                {view.actionable && (
                  <button
                    type="button"
                    onClick={() => onAct(alert)}
                    class="mt-0.5 flex-none rounded-md bg-white/[0.08] px-2.5 py-1.5 text-[12px]
                           font-medium text-ink-100 transition hover:bg-white/[0.12]"
                  >
                    {view.actionLabel}
                  </button>
                )}
              </li>
            );
          })}
        </ul>
      )}
    </DashboardCard>
  );
}
