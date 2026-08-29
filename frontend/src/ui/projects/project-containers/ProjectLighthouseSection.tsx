import { useState } from "preact/hooks";
import {
  CATEGORIES,
  MAX_PATHS,
  previousReport,
  reportMeasured,
  runHeadline,
  scoreBand,
  scoreDelta,
  type LighthouseReport,
  type LighthouseRun,
  type ScoreBand,
} from "../../../models/lighthouse";
import type { ProjectLighthouseState } from "../../../state/hooks/projects/useProjectLighthouse";
import { AlertCircle, Activity, Check, Download, Loader, Trash } from "../../primitives/icons";
import { Empty, Loading } from "./ProjectContainerPrimitives";
import { formatEpochMillis } from "./projectContainerFormat";

const BAND_TEXT: Record<ScoreBand, string> = {
  good: "text-accent-green",
  average: "text-accent-orange",
  poor: "text-accent-red",
  unknown: "text-ink-400",
};

const BAND_RING: Record<ScoreBand, string> = {
  good: "border-accent-green/40",
  average: "border-accent-orange/40",
  poor: "border-accent-red/40",
  unknown: "border-line",
};

const SUGGESTED_PATHS = "/";

export function ProjectLighthouseSection({
  state,
  ports,
  running,
}: {
  state: ProjectLighthouseState;
  /** The project's listening application ports, so the operator picks rather than types. */
  ports: number[];
  /** Whether the container is up; every action needs it. */
  running: boolean;
}) {
  const { overview, loading, busy, installing, error } = state;
  const [port, setPort] = useState<number | null>(null);
  const [paths, setPaths] = useState(SUGGESTED_PATHS);
  const [formFactor, setFormFactor] = useState<"mobile" | "desktop">("mobile");
  const [label, setLabel] = useState("");

  if (loading && !overview) return <Loading text="Loading audits…" />;

  const inFlight = overview?.running ?? false;
  const chosenPort = port ?? ports[0] ?? null;
  const pathList = paths
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);
  // undefined means the container is down, so the question was never asked.
  const needsInstall = overview?.installed === false;

  return (
    <div class="space-y-4">
      {!running && (
        <p class="rounded-card border border-line bg-surface px-4 py-3 text-[12.5px] text-ink-300">
          The container is stopped. Start the project to audit its pages.
        </p>
      )}

      {error && (
        <p class="flex items-start gap-2 rounded-card border border-accent-red/30 bg-accent-red/5 px-4 py-3 text-[12.5px] text-accent-red">
          <AlertCircle class="w-4 h-4 shrink-0 mt-px" />
          <span>{error}</span>
        </p>
      )}

      {needsInstall && (
        <div class="space-y-2 rounded-card border border-line bg-surface px-4 py-3">
          <p class="text-[12.5px] text-ink-200">
            This container was built before Lighthouse shipped in the base image, so the CLI is not
            there yet. Installing it takes about a minute and only has to happen once.
          </p>
          <button
            type="button"
            disabled={installing || !running}
            onClick={() => void state.install()}
            class="inline-flex h-9 items-center gap-2 rounded-md border border-line px-3 text-[12.5px] font-medium text-ink-100 transition-colors hover:bg-tint disabled:opacity-50"
          >
            {installing ? <Loader class="w-4 h-4 animate-spin" /> : <Download class="w-4 h-4" />}
            {installing ? "Installing…" : "Install Lighthouse here"}
          </button>
        </div>
      )}

      <section class="rounded-card border border-line bg-surface">
        <header class="flex flex-wrap items-center gap-2 border-b border-line px-4 py-3">
          <Activity class="w-4 h-4 text-ink-300" />
          <h3 class="text-[13px] font-medium text-ink-100">Run an audit</h3>
          <span class="text-[11.5px] text-ink-400">
            real Lighthouse, in this container, no API key
          </span>
        </header>

        <div class="space-y-3 px-4 py-3">
          <div class="flex flex-wrap items-center gap-2">
            <label class="text-[12px] text-ink-300">Preview port</label>
            {ports.length === 0 ? (
              <span class="text-[12px] text-ink-400">
                nothing is listening yet — start your dev server first
              </span>
            ) : (
              <select
                value={chosenPort ?? undefined}
                onChange={(event) =>
                  setPort(Number((event.currentTarget as HTMLSelectElement).value))
                }
                class="h-8 rounded-md border border-line bg-base px-2 text-[12.5px] text-ink-100"
              >
                {ports.map((value) => (
                  <option key={value} value={value}>
                    {value}
                  </option>
                ))}
              </select>
            )}

            <div class="ml-auto inline-flex overflow-hidden rounded-md border border-line">
              {(["mobile", "desktop"] as const).map((option) => (
                <button
                  key={option}
                  type="button"
                  onClick={() => setFormFactor(option)}
                  aria-pressed={formFactor === option}
                  class={`h-8 px-3 text-[12px] font-medium transition-colors ${
                    formFactor === option
                      ? "bg-tint-active text-ink-50"
                      : "text-ink-300 hover:bg-tint hover:text-ink-100"
                  }`}
                >
                  {option}
                </button>
              ))}
            </div>
          </div>
          <p class="text-[11px] text-ink-400">
            Mobile is the default because it is what Google ranks on.
          </p>

          <div>
            <label class="mb-1 block text-[12px] text-ink-300">
              Pages, one path per line (up to {MAX_PATHS})
            </label>
            <textarea
              value={paths}
              rows={3}
              spellcheck={false}
              onInput={(event) => setPaths((event.currentTarget as HTMLTextAreaElement).value)}
              class="w-full rounded-md border border-line bg-base px-2 py-1.5 font-mono text-[12px] text-ink-100"
            />
          </div>

          <div class="flex flex-wrap items-center gap-2">
            <input
              value={label}
              placeholder="What is this run about? (optional)"
              onInput={(event) => setLabel((event.currentTarget as HTMLInputElement).value)}
              class="h-9 min-w-[14rem] flex-1 rounded-md border border-line bg-base px-2 text-[12.5px] text-ink-100"
            />
            <button
              type="button"
              disabled={
                !running || busy || inFlight || needsInstall ||
                pathList.length === 0 || chosenPort === null
              }
              onClick={() =>
                void state
                  .run({ port: chosenPort as number, paths: pathList, formFactor, label })
                  .then(() => setLabel(""))
              }
              class="inline-flex h-9 items-center gap-2 rounded-md border border-line px-3 text-[12.5px] font-medium text-ink-100 transition-colors hover:bg-tint disabled:opacity-50"
            >
              {inFlight ? <Loader class="w-4 h-4 animate-spin" /> : <Check class="w-4 h-4" />}
              Audit now
            </button>
          </div>
        </div>
      </section>

      <section class="rounded-card border border-line bg-surface">
        <header class="flex flex-wrap items-center gap-2 border-b border-line px-4 py-3">
          <h3 class="text-[13px] font-medium text-ink-100">History</h3>
          <span class="text-[11.5px] text-ink-400">newest first</span>
        </header>
        {overview && overview.runs.length === 0 ? (
          <Empty text="No audit yet." />
        ) : (
          <ul class="divide-y divide-line">
            {overview?.runs.map((run) => (
              <RunRow
                key={run.id}
                run={run}
                runs={overview.runs}
                onDelete={() => void state.remove(run.id)}
                busy={busy}
              />
            ))}
          </ul>
        )}
      </section>
    </div>
  );
}

function RunRow({
  run,
  runs,
  onDelete,
  busy,
}: {
  run: LighthouseRun;
  runs: LighthouseRun[];
  onDelete: () => void;
  busy: boolean;
}) {
  const [open, setOpen] = useState(false);

  return (
    <li class="px-4 py-3">
      <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
        <button type="button" onClick={() => setOpen((value) => !value)} class="flex-1 text-left">
          <span class="text-[13px] font-medium text-ink-50">{runHeadline(run)}</span>
          {run.label && <span class="ml-2 text-[12px] text-ink-400">{run.label}</span>}
        </button>
        <span class="rounded border border-line px-1.5 text-[11px] text-ink-400">
          {run.formFactor}
        </span>
        <span class="text-[11px] text-ink-400">{formatEpochMillis(run.createdAt)}</span>
        <button
          type="button"
          disabled={busy}
          onClick={onDelete}
          title="Delete this run"
          class="rounded-md p-1 text-ink-400 transition-colors hover:bg-tint hover:text-ink-200 disabled:opacity-50"
        >
          <Trash class="w-3.5 h-3.5" />
        </button>
      </div>

      {open && (
        <ul class="mt-2 space-y-2">
          {run.reports.map((report) => (
            <ReportRow
              key={report.path}
              report={report}
              previous={previousReport(runs, run, report.path)}
            />
          ))}
        </ul>
      )}
    </li>
  );
}

function ReportRow({
  report,
  previous,
}: {
  report: LighthouseReport;
  previous: LighthouseReport | null;
}) {
  return (
    <li class="rounded-md border border-line px-3 py-2">
      <div class="flex flex-wrap items-center gap-2">
        <span class="font-mono text-[12px] text-ink-100">{report.path}</span>
        {report.version && (
          <span class="ml-auto text-[11px] text-ink-400">Lighthouse {report.version}</span>
        )}
      </div>

      {report.error ? (
        <p class="mt-1 text-[11.5px] text-ink-400">{report.error}</p>
      ) : (
        <>
          <div class="mt-2 flex flex-wrap gap-2">
            {CATEGORIES.map(({ key, label }) => (
              <ScoreChip
                key={key as string}
                label={label}
                score={report[key] as number | undefined}
                delta={scoreDelta(report, previous, key)}
              />
            ))}
          </div>

          {report.metrics && report.metrics.length > 0 && (
            <dl class="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 sm:grid-cols-3">
              {report.metrics.map((metric) => (
                <div key={metric.id} class="flex items-baseline justify-between gap-2">
                  <dt class="truncate text-[11.5px] text-ink-400">{metric.label}</dt>
                  <dd
                    class={`shrink-0 text-[11.5px] tabular-nums ${
                      BAND_TEXT[
                        scoreBand(typeof metric.score === "number" ? metric.score * 100 : undefined)
                      ]
                    }`}
                  >
                    {metric.display ?? metric.value}
                  </dd>
                </div>
              ))}
            </dl>
          )}

          {report.opportunities && report.opportunities.length > 0 && (
            <ul class="mt-2 space-y-0.5">
              {report.opportunities.map((finding) => (
                <li key={finding.id} class="flex items-baseline gap-2 text-[11.5px]">
                  <span class="text-ink-300">{finding.title}</span>
                  {finding.savingsMs ? (
                    <span class="ml-auto shrink-0 tabular-nums text-accent-orange">
                      ~{Math.round(finding.savingsMs)} ms
                    </span>
                  ) : (
                    finding.display && (
                      <span class="ml-auto shrink-0 text-ink-400">{finding.display}</span>
                    )
                  )}
                </li>
              ))}
            </ul>
          )}
        </>
      )}
    </li>
  );
}

/**
 * One category's score, with the change since the last comparable run.
 *
 * An absent score renders as a dash rather than a zero: Lighthouse omits what
 * it could not compute, and a 0 in a red ring would say the page failed
 * completely when nothing was measured at all.
 */
function ScoreChip({
  label,
  score,
  delta,
}: {
  label: string;
  score?: number;
  delta: number | null;
}) {
  const band = scoreBand(score);
  return (
    <div class={`rounded-md border px-2 py-1 ${BAND_RING[band]}`}>
      <div class="flex items-baseline gap-1.5">
        <span class={`text-[15px] font-semibold tabular-nums ${BAND_TEXT[band]}`}>
          {typeof score === "number" ? score : "—"}
        </span>
        {delta !== null && delta !== 0 && (
          <span
            class={`text-[11px] tabular-nums ${
              delta > 0 ? "text-accent-green" : "text-accent-red"
            }`}
          >
            {delta > 0 ? "+" : ""}
            {delta}
          </span>
        )}
      </div>
      <div class="text-[10.5px] text-ink-400">{label}</div>
    </div>
  );
}
