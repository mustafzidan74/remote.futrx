import { useState } from "preact/hooks";
import {
  byMostChanged,
  changeLabel,
  changeTone,
  comparisonHeadline,
  MAX_PATHS,
  pageChanged,
  type ChangeTone,
  type VisualComparison,
  type VisualPageDiff,
} from "../../../models/visualDiff";
import type { ProjectVisualState } from "../../../state/hooks/projects/useProjectVisual";
import { AlertCircle, Camera, Check, Layers, Loader, Trash } from "../../primitives/icons";
import { Empty, Loading } from "./ProjectContainerPrimitives";
import { formatEpochMillis } from "./projectContainerFormat";

const TONE_TEXT: Record<ChangeTone, string> = {
  none: "text-ink-400",
  slight: "text-accent-blue",
  notable: "text-accent-orange",
  major: "text-accent-red",
  failed: "text-ink-400",
};

const TONE_DOT: Record<ChangeTone, string> = {
  none: "bg-ink-500",
  slight: "bg-accent-blue",
  notable: "bg-accent-orange",
  major: "bg-accent-red",
  failed: "bg-ink-500",
};

/** The pages worth photographing on almost any site, offered as a starting point. */
const SUGGESTED_PATHS = "/\n/about\n/contact";

export function ProjectVisualSection({
  state,
  ports,
  running,
}: {
  state: ProjectVisualState;
  /** The project's listening application ports, so the operator picks rather than types. */
  ports: number[];
  /** Whether the container is up; every action needs it. */
  running: boolean;
}) {
  const { overview, loading, busy, error } = state;
  const [port, setPort] = useState<number | null>(ports[0] ?? null);
  const [paths, setPaths] = useState(SUGGESTED_PATHS);
  const [fullPage, setFullPage] = useState(true);
  const [label, setLabel] = useState("");

  if (loading && !overview) return <Loading text="Loading visual comparison…" />;

  const baseline = overview?.baseline;
  const inFlight = overview?.running ?? false;
  const chosenPort = port ?? ports[0] ?? null;

  const pathList = paths
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);

  return (
    <div class="space-y-4">
      {!running && (
        <p class="rounded-card border border-line bg-surface px-4 py-3 text-[12.5px] text-ink-300">
          The container is stopped. Start the project to photograph its pages.
        </p>
      )}

      {error && (
        <p class="flex items-start gap-2 rounded-card border border-accent-red/30 bg-accent-red/5 px-4 py-3 text-[12.5px] text-accent-red">
          <AlertCircle class="w-4 h-4 shrink-0 mt-px" />
          <span>{error}</span>
        </p>
      )}

      <section class="rounded-card border border-line bg-surface">
        <header class="flex flex-wrap items-center gap-2 border-b border-line px-4 py-3">
          <Layers class="w-4 h-4 text-ink-300" />
          <h3 class="text-[13px] font-medium text-ink-100">Baseline</h3>
          <span class="text-[11.5px] text-ink-400">what this project is supposed to look like</span>
        </header>

        {baseline ? (
          <BaselineSummary state={state} inFlight={inFlight} running={running} />
        ) : (
          <p class="px-4 py-3 text-[12.5px] text-ink-300">
            Take a baseline before you start editing. Afterwards, one comparison re-photographs the
            same pages and tells you which ones moved — including the ones you did not touch.
          </p>
        )}

        <div class="space-y-3 border-t border-line px-4 py-3">
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
            <label class="ml-auto flex items-center gap-1.5 text-[12px] text-ink-300">
              <input
                type="checkbox"
                checked={fullPage}
                onChange={(event) => setFullPage((event.currentTarget as HTMLInputElement).checked)}
              />
              Full page
            </label>
          </div>

          <div>
            <label class="mb-1 block text-[12px] text-ink-300">
              Pages, one path per line (up to {MAX_PATHS})
            </label>
            <textarea
              value={paths}
              rows={4}
              spellcheck={false}
              onInput={(event) => setPaths((event.currentTarget as HTMLTextAreaElement).value)}
              class="w-full rounded-md border border-line bg-base px-2 py-1.5 font-mono text-[12px] text-ink-100"
            />
            <p class="mt-1 text-[11px] text-ink-400">
              Cover your real templates, not just the home page: the point is to catch the page you
              were not looking at.
            </p>
          </div>

          <button
            type="button"
            disabled={!running || busy || inFlight || pathList.length === 0 || chosenPort === null}
            onClick={() =>
              void state.setBaseline({ port: chosenPort as number, paths: pathList, fullPage })
            }
            class="inline-flex h-9 items-center gap-2 rounded-md border border-line px-3 text-[12.5px] font-medium text-ink-100 transition-colors hover:bg-tint disabled:opacity-50"
          >
            <Camera class="w-4 h-4" />
            {baseline ? "Replace baseline" : "Take baseline"}
          </button>
          {baseline && (
            <p class="text-[11px] text-ink-400">
              Replacing it discards the comparisons below: they measure against the old pictures.
            </p>
          )}
        </div>
      </section>

      {baseline?.status === "ready" && (
        <section class="rounded-card border border-line bg-surface">
          <header class="flex flex-wrap items-center gap-2 border-b border-line px-4 py-3">
            <h3 class="text-[13px] font-medium text-ink-100">Comparisons</h3>
            <span class="text-[11.5px] text-ink-400">newest first</span>
          </header>

          <div class="flex flex-wrap items-center gap-2 border-b border-line px-4 py-3">
            <input
              value={label}
              placeholder="What changed? (optional)"
              onInput={(event) => setLabel((event.currentTarget as HTMLInputElement).value)}
              class="h-9 min-w-[14rem] flex-1 rounded-md border border-line bg-base px-2 text-[12.5px] text-ink-100"
            />
            <button
              type="button"
              disabled={!running || busy || inFlight}
              onClick={() =>
                void state.compare(label).then(() => setLabel(""))
              }
              class="inline-flex h-9 items-center gap-2 rounded-md border border-line px-3 text-[12.5px] font-medium text-ink-100 transition-colors hover:bg-tint disabled:opacity-50"
            >
              {inFlight ? <Loader class="w-4 h-4 animate-spin" /> : <Check class="w-4 h-4" />}
              Compare now
            </button>
          </div>

          {overview && overview.comparisons.length === 0 ? (
            <Empty text="No comparison yet. Make your change, then press Compare now." />
          ) : (
            <ul class="divide-y divide-line">
              {overview?.comparisons.map((comparison) => (
                <ComparisonRow
                  key={comparison.id}
                  comparison={comparison}
                  onDelete={() => void state.remove(comparison.id)}
                  busy={busy}
                />
              ))}
            </ul>
          )}
        </section>
      )}
    </div>
  );
}

function BaselineSummary({
  state,
  inFlight,
}: {
  state: ProjectVisualState;
  inFlight: boolean;
  running: boolean;
}) {
  const baseline = state.overview?.baseline;
  if (!baseline) return null;
  const failed = baseline.pages.filter((page) => page.error);
  const captured = baseline.pages.filter((page) => !page.error);

  return (
    <div class="space-y-2 px-4 py-3">
      <div class="flex flex-wrap items-baseline gap-x-2 text-[12.5px]">
        {baseline.status === "running" || inFlight ? (
          <span class="inline-flex items-center gap-1.5 text-ink-300">
            <Loader class="w-3.5 h-3.5 animate-spin" />
            photographing {baseline.pages.length} of {baseline.paths.length}…
          </span>
        ) : baseline.status === "failed" ? (
          <span class="text-accent-red">{baseline.error || "the baseline failed"}</span>
        ) : (
          <span class="text-ink-100">
            {captured.length} {captured.length === 1 ? "page" : "pages"} on port {baseline.port}
          </span>
        )}
        <span class="text-[11px] text-ink-400">
          taken {formatEpochMillis(baseline.createdAt)}
          {baseline.fullPage ? " · full page" : ""}
        </span>
      </div>

      {failed.length > 0 && (
        <ul class="space-y-0.5 text-[11.5px] text-ink-400">
          {failed.map((page) => (
            <li key={page.path}>
              <span class="font-mono text-ink-300">{page.path}</span> — {page.error}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function ComparisonRow({
  comparison,
  onDelete,
  busy,
}: {
  comparison: VisualComparison;
  onDelete: () => void;
  busy: boolean;
}) {
  const [open, setOpen] = useState(false);
  const changed = comparison.changedPages > 0;

  return (
    <li class="px-4 py-3">
      <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
        <button
          type="button"
          onClick={() => setOpen((value) => !value)}
          class="flex-1 text-left"
        >
          <span
            class={`text-[13px] font-medium ${changed ? "text-ink-50" : "text-ink-200"}`}
          >
            {comparisonHeadline(comparison)}
          </span>
          {comparison.label && (
            <span class="ml-2 text-[12px] text-ink-400">{comparison.label}</span>
          )}
        </button>
        <span class="text-[11px] text-ink-400">{formatEpochMillis(comparison.createdAt)}</span>
        <button
          type="button"
          disabled={busy}
          onClick={onDelete}
          title="Delete this comparison"
          class="rounded-md p-1 text-ink-400 transition-colors hover:bg-tint hover:text-ink-200 disabled:opacity-50"
        >
          <Trash class="w-3.5 h-3.5" />
        </button>
      </div>

      {comparison.status === "running" && (
        <p class="mt-1 inline-flex items-center gap-1.5 text-[12px] text-ink-300">
          <Loader class="w-3.5 h-3.5 animate-spin" />
          {comparison.pages.length} pages done
        </p>
      )}

      {open && comparison.pages.length > 0 && (
        <ul class="mt-2 space-y-2">
          {byMostChanged(comparison.pages).map((page) => (
            <PageRow key={page.path} page={page} />
          ))}
        </ul>
      )}
    </li>
  );
}

function PageRow({ page }: { page: VisualPageDiff }) {
  const [view, setView] = useState<"diff" | "before" | "after">("diff");
  const tone = changeTone(page);
  const shown = view === "before" ? page.beforeUrl : view === "after" ? page.afterUrl : page.diffUrl;

  return (
    <li class="rounded-md border border-line px-3 py-2">
      <div class="flex flex-wrap items-center gap-x-2 gap-y-1">
        <span class={`h-1.5 w-1.5 shrink-0 rounded-full ${TONE_DOT[tone]}`} />
        <span class="font-mono text-[12px] text-ink-100">{page.path}</span>
        <span class={`ml-auto text-[11.5px] tabular-nums ${TONE_TEXT[tone]}`}>
          {changeLabel(page)}
        </span>
      </div>

      {page.error && <p class="mt-1 text-[11.5px] text-ink-400">{page.error}</p>}

      {pageChanged(page) && (
        <>
          <div class="mt-2 inline-flex overflow-hidden rounded-md border border-line">
            {(["diff", "before", "after"] as const).map((option) => (
              <button
                key={option}
                type="button"
                onClick={() => setView(option)}
                aria-pressed={view === option}
                class={`h-7 px-2.5 text-[11.5px] font-medium transition-colors ${
                  view === option
                    ? "bg-tint-active text-ink-50"
                    : "text-ink-300 hover:bg-tint hover:text-ink-100"
                }`}
              >
                {option}
              </button>
            ))}
          </div>
          {shown && (
            <div class="mt-2 max-h-[28rem] overflow-auto rounded-md border border-line bg-base">
              <img src={shown} alt={`${page.path} (${view})`} class="w-full" loading="lazy" />
            </div>
          )}
        </>
      )}
    </li>
  );
}
