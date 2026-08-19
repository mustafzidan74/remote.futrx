import type { DashboardAlert, DashboardPlatform } from "../../models/dashboard";
import type { Dashboard as DashboardData } from "../../state/hooks/home/useDashboard";
import { formatBytes, formatRelative, type StatusTone } from "../../state/home/dashboardState";
import { Meter } from "../primitives/Meter";
import { TONE_DOT, TONE_TEXT } from "./DashboardCard";
import { AttentionCard } from "./AttentionCard";
import { ClientSitesCard } from "./ClientSitesCard";
import { KpiTiles } from "./KpiTiles";
import { ProjectsCard } from "./ProjectsCard";
import { RecentActivityCard } from "./RecentActivityCard";
import { UpcomingCard } from "./UpcomingCard";
import { UsageCard } from "./UsageCard";
import { AlertCircle, Menu, RotateCcw, Server } from "../primitives/icons";

/**
 * Everything the home screen can ask the rest of the app to do. The dashboard
 * itself navigates nowhere: it hands the intent back to the workspace, which
 * is the only place that knows what a view change means.
 */
export interface DashboardHandlers {
  openChat: (chatId: string) => void;
  openProject: (projectId: string, tab?: string) => void;
  openSettings: (tabId: string) => void;
  newProject: () => void;
  runTaskNow: (taskId: string) => Promise<void>;
  onHamburger: () => void;
}

/**
 * The home dashboard: one screen that answers "what is happening?".
 *
 * It is the landing view when no chat is selected, and it is reachable at any
 * time from the sidebar, the app title, and the command palette. Everything
 * on it comes from one request that the server fans out, refreshed once a
 * minute while the tab is visible.
 */
export function Dashboard({
  data,
  handlers,
}: {
  data: DashboardData;
  handlers: DashboardHandlers;
}) {
  const { snapshot, loading, refreshing, error, now } = data;
  const windowDays = snapshot?.windowDays ?? 7;

  function act(alert: DashboardAlert) {
    switch (alert.action) {
      case "open-project":
        if (alert.projectId) handlers.openProject(alert.projectId, "info");
        return;
      case "open-chat":
        if (alert.chatId) handlers.openChat(alert.chatId);
        return;
      case "snapshot-now":
        if (alert.projectId) handlers.openProject(alert.projectId, "snapshots");
        return;
      case "restore-trash":
        handlers.openSettings("trash");
        return;
      case "enable-notifications":
        handlers.openSettings("notifications");
        return;
      case "open-monitoring":
        handlers.openSettings("monitoring");
        return;
      case "open-resources":
        handlers.openSettings("resources");
        return;
      case "open-client-sites":
        handlers.openSettings("client-sites");
        return;
      default:
        // An action a newer server offers and this build does not know. The
        // row renders without a button, so this is unreachable in practice.
        return;
    }
  }

  return (
    <div class="flex min-h-0 flex-1 flex-col">
      <header
        class="codex-header top-chrome z-20 flex min-h-[52px] flex-none items-center gap-2
               border-b border-white/10 bg-[#101318] px-3 pb-2"
      >
        <button
          type="button"
          onClick={handlers.onHamburger}
          class="grid h-10 w-10 place-items-center rounded-md text-ink-100 hover:bg-white/[0.08] md:hidden"
          aria-label="Toggle sidebar"
        >
          <Menu class="h-5 w-5" />
        </button>
        <div class="min-w-0 flex-1">
          <span class="text-sm font-semibold text-ink-100">Home</span>
          <span class="ml-2 text-[12px] text-ink-400">
            {snapshot ? `updated ${formatRelative(snapshot.generatedAt, now) || "just now"}` : ""}
          </span>
        </div>
        <button
          type="button"
          onClick={() => void data.refresh()}
          disabled={refreshing || loading}
          class="inline-flex h-8 flex-none items-center gap-1.5 rounded-md bg-white/[0.08] px-2.5
                 text-[12px] font-medium text-ink-100 transition hover:bg-white/[0.12] disabled:opacity-50"
          title="Refresh now"
        >
          <RotateCcw class={`h-3.5 w-3.5${refreshing ? " animate-spin" : ""}`} />
          <span class="hidden sm:inline">Refresh</span>
        </button>
      </header>

      <div class="min-h-0 flex-1 overflow-y-auto touch-scroll p-3 sm:p-4">
        <div class="mx-auto flex w-full max-w-[1180px] flex-col gap-3">
          {error && (
            <p
              class="flex items-center gap-2 rounded-lg border border-accent-red/30 bg-accent-red/10
                     px-3 py-2 text-[12.5px] text-accent-red"
              role="alert"
            >
              <AlertCircle class="h-4 w-4 flex-none" />
              Could not load the dashboard: {error}
            </p>
          )}

          {loading && !snapshot ? (
            <KpiSkeleton />
          ) : (
            snapshot && (
              <KpiTiles
                kpis={snapshot.kpis}
                alerts={snapshot.alerts}
                windowDays={windowDays}
                onOpenUsage={() => handlers.openSettings("usage")}
                onOpenAlerts={scrollToAttention}
              />
            )
          )}

          {snapshot && <PlatformStrip platform={snapshot.platform} now={now} />}

          {/* Single column on mobile; the two columns pair "what exists" with
              "what needs you" and "what happened" with "what is next". */}
          <div class="grid grid-cols-1 gap-3 lg:grid-cols-2">
            <ProjectsCard
              projects={snapshot?.projects ?? []}
              loading={loading && !snapshot}
              now={now}
              onOpenChat={handlers.openChat}
              onOpenProject={(projectId) => handlers.openProject(projectId, "info")}
              onSnapshotProject={(projectId) => handlers.openProject(projectId, "snapshots")}
              onNewProject={handlers.newProject}
            />
            <AttentionCard
              id={ATTENTION_ANCHOR}
              alerts={snapshot?.alerts ?? []}
              loading={loading && !snapshot}
              onAct={act}
            />
            <RecentActivityCard
              runs={snapshot?.recent ?? []}
              loading={loading && !snapshot}
              now={now}
              onOpenChat={handlers.openChat}
            />
            <UpcomingCard
              tasks={snapshot?.upcoming ?? []}
              loading={loading && !snapshot}
              now={now}
              onRunNow={handlers.runTaskNow}
              onOpenChat={handlers.openChat}
            />
            <ClientSitesCard
              sites={snapshot?.sites ?? EMPTY_SITES}
              loading={loading && !snapshot}
              now={now}
              onOpenSettings={() => handlers.openSettings("client-sites")}
            />
          </div>

          <UsageCard
            usage={snapshot?.usage ?? EMPTY_USAGE}
            windowDays={windowDays}
            loading={loading && !snapshot}
            onOpenUsage={() => handlers.openSettings("usage")}
          />
        </div>
      </div>
    </div>
  );
}

/**
 * The alert tile counts the Attention panel, so it scrolls to it rather than
 * navigating anywhere. On a phone that panel is well below the fold, which is
 * exactly when the count is the only thing telling you it is there.
 */
const ATTENTION_ANCHOR = "dashboard-attention";

function scrollToAttention() {
  document
    .getElementById(ATTENTION_ANCHOR)
    ?.scrollIntoView({ behavior: "smooth", block: "start" });
}

const EMPTY_SITES = { available: false, sites: [] };

const EMPTY_USAGE = {
  available: false,
  daily: [],
  thisWeek: {
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheWriteTokens: 0,
    totalTokens: 0,
    costUsd: 0,
    estimatedCostUsd: 0,
    runs: 0,
    unpricedRuns: 0,
  },
  lastWeek: {
    inputTokens: 0,
    outputTokens: 0,
    cacheReadTokens: 0,
    cacheWriteTokens: 0,
    totalTokens: 0,
    costUsd: 0,
    estimatedCostUsd: 0,
    runs: 0,
    unpricedRuns: 0,
  },
};

/**
 * The one-line state of the box itself: the checks /healthz answers with, and
 * how much of the host's container memory budget is already committed —
 * which is what decides whether the next project start succeeds.
 */
function PlatformStrip({ platform, now }: { platform: DashboardPlatform; now: number }) {
  const tone: StatusTone = platform.status === "ok" ? "green" : platform.status === "degraded" ? "red" : "grey";
  const committedPct =
    platform.memoryBudgetBytes && platform.memoryBudgetBytes > 0
      ? Math.min(100, ((platform.memoryCommittedBytes ?? 0) / platform.memoryBudgetBytes) * 100)
      : undefined;

  return (
    <section class="flex flex-wrap items-center gap-x-5 gap-y-3 rounded-lg border border-white/10 bg-[#101318] px-4 py-3">
      <div class="flex items-center gap-2">
        <Server class="h-4 w-4 flex-none text-ink-300" />
        <span class={`h-2 w-2 flex-none rounded-full ${TONE_DOT[tone]}`} aria-hidden="true" />
        <span class="text-[12.5px] text-ink-200">
          Platform <span class={TONE_TEXT[tone]}>{platform.status}</span>
        </span>
        {platform.version && (
          <span class="font-mono text-[11.5px] tabular-nums text-ink-400">v{platform.version}</span>
        )}
      </div>

      <div class="flex items-center gap-3 text-[12px] text-ink-300">
        {(["backend", "lxd", "caddy"] as const).map((check) => (
          <span key={check} class="inline-flex items-center gap-1">
            <span
              class={`h-1.5 w-1.5 rounded-full ${
                platform.checks[check] === "ok"
                  ? TONE_DOT.green
                  : platform.checks[check] === "degraded"
                    ? TONE_DOT.red
                    : TONE_DOT.grey
              }`}
              aria-hidden="true"
            />
            {check} {platform.checks[check] || "unknown"}
          </span>
        ))}
      </div>

      {committedPct != null && (
        <div class="min-w-[180px] flex-1">
          <Meter
            label={`Memory committed · ${platform.runningContainers} running`}
            detail={`${formatBytes(platform.memoryCommittedBytes)} / ${formatBytes(platform.memoryBudgetBytes)}`}
            percent={committedPct}
          />
        </div>
      )}

      <span class="text-[11.5px] text-ink-400">
        {platform.backupReadable
          ? `Host backup ${platform.backupAt ? formatRelative(platform.backupAt, now) : "never taken"}`
          : "No host backup directory"}
      </span>
    </section>
  );
}

function KpiSkeleton() {
  return (
    <div class="grid grid-cols-2 gap-3 lg:grid-cols-4" aria-hidden="true">
      {Array.from({ length: 4 }, (_, index) => (
        <div key={index} class="rounded-lg border border-white/10 bg-[#101318] p-3.5">
          <span class="block h-3 w-2/3 animate-pulse rounded bg-white/[0.07]" />
          <span class="mt-3 block h-6 w-1/3 animate-pulse rounded bg-white/[0.07]" />
          <span class="mt-2 block h-2.5 w-3/4 animate-pulse rounded bg-white/[0.05]" />
        </div>
      ))}
    </div>
  );
}
