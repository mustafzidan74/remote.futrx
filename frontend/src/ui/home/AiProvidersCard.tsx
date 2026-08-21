import { useEffect, useState } from "preact/hooks";
import { aiProvidersApi } from "../../api/aiProvidersApi";
import type { QuotaView, UsageMeter } from "../../models/aiProviders";
import {
  PROVIDER_STATUS_LABELS,
  PROVIDER_STATUS_TONE,
  meterLabel,
  meterPercent,
  meterSourceLabel,
  meterTone,
  quotaSubtitle,
  topQuotaRows,
} from "../../state/settings/aiProvidersState";
import type { StatusTone } from "../../state/home/dashboardState";
import { CardEmpty, CardSkeleton, DashboardCard, ToneDot } from "./DashboardCard";
import { Network } from "../primitives/icons";

/** The bar's fill, painted through `currentColor`. */
const TONE_TEXT: Record<StatusTone, string> = {
  green: "text-accent-blue",
  amber: "text-accent-orange",
  red: "text-accent-red",
  grey: "text-ink-400",
};

/**
 * Free quota: how much of each connected free tier is left this month.
 *
 * It fetches its own payload rather than riding the dashboard snapshot,
 * because the pool is optional in a way the rest of the board is not — a
 * deployment with no providers should cost the dashboard service nothing, and
 * a pool that fails to answer must not take the whole board down with it. That
 * is also why a failed request lands in the "not set up" branch instead of an
 * error banner: a card nobody configured has nothing to complain about.
 */
export function AiProvidersCard({ onOpenSettings }: { onOpenSettings: () => void }) {
  const [quota, setQuota] = useState<QuotaView | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    aiProvidersApi
      .quota()
      .then((value) => !cancelled && setQuota(value))
      .catch(() => !cancelled && setQuota(null))
      .finally(() => !cancelled && setLoading(false));
    return () => {
      cancelled = true;
    };
  }, []);

  const rows = topQuotaRows(quota);

  return (
    <DashboardCard
      title="Free quota"
      subtitle={quotaSubtitle(quota)}
      Icon={Network}
      action={
        <button
          type="button"
          onClick={onOpenSettings}
          class="rounded-md px-2 py-1 text-[11.5px] text-ink-300 transition hover:bg-white/[0.07] hover:text-ink-100"
        >
          Manage
        </button>
      }
    >
      {loading ? (
        <CardSkeleton rows={2} />
      ) : !quota?.available ? (
        <CardEmpty>
          No AI providers connected yet. Settings → AI providers connects the free tiers of Gemini,
          Groq and the rest, so the platform's own text chores keep working when one runs out.
        </CardEmpty>
      ) : rows.length === 0 ? (
        <CardEmpty>Nothing has been spent through the pool yet this month.</CardEmpty>
      ) : (
        <ul class="divide-y divide-white/[0.06]">
          {rows.map((row) => (
            <li key={row.id} class="flex items-start gap-2.5 px-4 py-2.5">
              <ToneDot
                tone={PROVIDER_STATUS_TONE[row.status]}
                title={PROVIDER_STATUS_LABELS[row.status]}
              />
              <div class="min-w-0 flex-1">
                <div class="flex flex-wrap items-baseline gap-x-2">
                  <span class="truncate text-[13px] text-ink-100">{row.label}</span>
                  <span class="text-[11.5px] text-ink-400">
                    {PROVIDER_STATUS_LABELS[row.status]}
                  </span>
                </div>
                <QuotaBar label="Tokens this month" meter={row.tokensMonth} />
                <p class="mt-1 text-[11px] tabular-nums text-ink-400">
                  {meterLabel(row.requestsToday)} requests today ·{" "}
                  {meterLabel(row.tokensToday)} tokens
                </p>
              </div>
            </li>
          ))}
        </ul>
      )}
    </DashboardCard>
  );
}

/**
 * One bar. A window the vendor documents no cap for draws an empty track and
 * prints the count — the card would rather say less than imply a ceiling that
 * was never published.
 */
function QuotaBar({ label, meter }: { label: string; meter: UsageMeter }) {
  const percent = meterPercent(meter);
  const value = meterLabel(meter);
  const title =
    percent == null
      ? `${label}: ${value} — no documented cap · ${meterSourceLabel(meter)}`
      : `${label}: ${value} (${percent}%) · ${meterSourceLabel(meter)}`;

  return (
    <div class="mt-1">
      <svg
        viewBox="0 0 100 6"
        preserveAspectRatio="none"
        class="h-1.5 w-full"
        role="img"
        aria-label={title}
      >
        <title>{title}</title>
        <rect x="0" y="0" width="100" height="6" rx="3" fill="currentColor" class="text-white/10" />
        {percent != null && percent > 0 && (
          <rect
            x="0"
            y="0"
            width={Math.max(percent, 1.5)}
            height="6"
            rx="3"
            fill="currentColor"
            class={TONE_TEXT[meterTone(percent)]}
          />
        )}
      </svg>
    </div>
  );
}
