import type { DashboardClientSites } from "../../models/dashboard";
import { formatDuration, formatRelative, type StatusTone } from "../../state/home/dashboardState";
import { CardEmpty, CardSkeleton, DashboardCard, ToneDot } from "./DashboardCard";
import { ExternalLink, Globe } from "../primitives/icons";

/** A watched site's status mapped onto the board's shared tones. */
const SITE_TONE: Record<string, StatusTone> = {
  down: "red",
  slow: "amber",
  up: "green",
  unknown: "grey",
};

const SITE_LABEL: Record<string, string> = {
  down: "Down",
  slow: "Slow",
  up: "Up",
  unknown: "Not checked",
};

/**
 * Client sites: the operator's *clients'* websites that are not currently
 * green.
 *
 * The card lists only what is wrong, because a board naming forty healthy
 * shops is a board nobody reads — and because "nothing here" is then the
 * whole message. A deployment that watches nothing says so rather than
 * reporting a green fleet it does not have.
 */
export function ClientSitesCard({
  sites,
  loading,
  now,
  onOpenSettings,
}: {
  sites: DashboardClientSites;
  loading: boolean;
  now: number;
  onOpenSettings: () => void;
}) {
  const rows = sites?.sites ?? [];

  return (
    <DashboardCard
      title="Client sites"
      subtitle={
        !sites?.available
          ? "Not set up"
          : rows.length === 0
            ? "Every watched site is up"
            : `${rows.length} site${rows.length === 1 ? "" : "s"} need attention`
      }
      Icon={Globe}
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
      ) : !sites?.available ? (
        <CardEmpty>
          Nothing is being watched yet. Settings → Client sites adds the websites you built for
          other people; each one costs a single HEAD request per interval and no agent tokens.
        </CardEmpty>
      ) : rows.length === 0 ? (
        <CardEmpty>All watched client sites are answering normally.</CardEmpty>
      ) : (
        <ul class="divide-y divide-white/[0.06]">
          {rows.map((site) => {
            const tone = SITE_TONE[site.status] ?? "grey";
            const label = SITE_LABEL[site.status] ?? site.status;
            const forHowLong = site.since ? formatDuration(Math.max(0, now - site.since)) : "";
            return (
              <li key={site.id} class="flex items-start gap-2.5 px-4 py-2.5">
                <ToneDot tone={tone} title={label} pulse={site.status === "down"} />
                <div class="min-w-0 flex-1">
                  <div class="flex flex-wrap items-baseline gap-x-2">
                    <span class="truncate text-[13px] text-ink-100">{site.label}</span>
                    <span class="text-[11.5px] text-ink-400">
                      {label}
                      {forHowLong ? ` for ${forHowLong}` : ""}
                    </span>
                  </div>
                  {site.detail && (
                    <p class="mt-0.5 break-words text-[11.5px] leading-snug text-ink-300">
                      {site.detail}
                    </p>
                  )}
                  <p class="mt-0.5 text-[11px] text-ink-400">
                    checked {formatRelative(site.lastCheckedAt, now) || "just now"}
                  </p>
                </div>
                <a
                  href={site.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  class="grid h-7 w-7 flex-none place-items-center rounded-md text-ink-300 transition hover:bg-white/[0.09] hover:text-ink-100"
                  title={`Open ${site.url}`}
                  aria-label={`Open ${site.label}`}
                >
                  <ExternalLink class="h-3.5 w-3.5" />
                </a>
              </li>
            );
          })}
        </ul>
      )}
    </DashboardCard>
  );
}
