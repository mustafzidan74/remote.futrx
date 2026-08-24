import { useEffect, useState } from "preact/hooks";
import { agentQuotaApi } from "../../api/agentQuotaApi";
import {
  agentQuotaLabel,
  measuredAgo,
  quotaTone,
  resetsIn,
  windowLabel,
  type AgentQuota,
  type QuotaTone,
  type QuotaWindow,
} from "../../models/agentQuota";
import { CardEmpty, CardSkeleton, DashboardCard } from "./DashboardCard";
import { Key } from "../primitives/icons";

const TONE_TEXT: Record<QuotaTone, string> = {
  ok: "text-accent-blue",
  warn: "text-accent-orange",
  spent: "text-accent-red",
  unknown: "text-ink-400",
};

const TONE_WORD: Record<QuotaTone, string> = {
  ok: "fine",
  warn: "getting low",
  spent: "out",
  unknown: "not reported",
};

/**
 * Plan limits: how much of the Claude and Codex subscriptions is left.
 *
 * Separate from the Free quota card because the two are different money. That
 * one meters API keys the platform bills itself; this one reads a rolling
 * subscription window the vendor owns and the platform can only overhear.
 *
 * The CLIs mention their windows during a run and offer no way to ask, so
 * every row is a snapshot with an age on it. The alternative — polling — does
 * not exist, and printing a stale figure as though it were current is how an
 * operator ends up planning a day's work against yesterday's number.
 */
export function PlanQuotaCard() {
  const [agents, setAgents] = useState<AgentQuota[] | null>(null);
  const [loading, setLoading] = useState(true);
  const now = Date.now();

  useEffect(() => {
    let cancelled = false;
    agentQuotaApi
      .list()
      .then((value) => !cancelled && setAgents(value))
      .catch(() => !cancelled && setAgents([]))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, []);

  const rows = (agents ?? []).filter((a) => a.session || a.weekly);

  return (
    <DashboardCard
      title="Plan limits"
      subtitle="What your subscriptions report during a run"
      Icon={Key}
    >
      {loading ? (
        <CardSkeleton rows={2} />
      ) : rows.length === 0 ? (
        <CardEmpty>
          Nothing reported yet. Claude and Codex mention their 5-hour and weekly windows while they
          work, so this fills in after the next run.
        </CardEmpty>
      ) : (
        <ul class="divide-y divide-white/[0.06]">
          {rows.map((row) => (
            <li key={row.provider} class="px-4 py-2.5">
              <div class="flex flex-wrap items-baseline gap-x-2">
                <span class="text-[13px] font-medium text-ink-100">
                  {agentQuotaLabel(row.provider)}
                </span>
                <span class="text-[11px] text-ink-400">
                  measured {measuredAgo(row.session ?? row.weekly, now)}
                </span>
              </div>
              <WindowRow win={row.session} kind="session" now={now} />
              <WindowRow win={row.weekly} kind="weekly" now={now} />
            </li>
          ))}
        </ul>
      )}
    </DashboardCard>
  );
}

/**
 * One window.
 *
 * A window with a percentage gets a bar. One with only a status gets the word
 * and nothing else — Claude usually reports "allowed" and no number, and a bar
 * drawn at zero would read as "none of your plan is used", which is a claim
 * the CLI never made.
 */
function WindowRow({
  win,
  kind,
  now,
}: {
  win?: QuotaWindow;
  kind: "session" | "weekly";
  now: number;
}) {
  if (!win) return null;
  const tone = quotaTone(win);
  const percent = typeof win.usedPercent === "number" ? Math.round(win.usedPercent) : null;
  const reset = resetsIn(win, now);

  return (
    <div class="mt-1.5">
      <div class="flex items-baseline justify-between gap-2">
        <span class="text-[11.5px] text-ink-300">{windowLabel(kind)}</span>
        <span class={`text-[11.5px] tabular-nums ${TONE_TEXT[tone]}`}>
          {percent == null ? TONE_WORD[tone] : `${percent}% used`}
        </span>
      </div>
      {percent != null && (
        <div class="mt-1 h-1.5 overflow-hidden rounded-full bg-white/[0.08]">
          <div
            class={`h-full rounded-full bg-current ${TONE_TEXT[tone]}`}
            style={{ width: `${Math.min(100, Math.max(2, percent))}%` }}
          />
        </div>
      )}
      {reset && <p class="mt-0.5 text-[11px] text-ink-400">{reset}</p>}
    </div>
  );
}
