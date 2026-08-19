import { useEffect, useState } from "preact/hooks";
import type { ProjectMeta } from "../../models/project";
import type {
  SiteCandidate,
  SiteCheckReport,
  SiteImportResult,
  WatchedSiteView,
} from "../../models/sitewatch";
import { useClientSites } from "../../state/hooks/settings/useClientSites";
import {
  describeTls,
  emptySiteForm,
  formToInput,
  formatAgo,
  formatCountdown,
  formatMs,
  formatUptime,
  parsePastedUrls,
  siteDot,
  siteHost,
  siteName,
  siteToForm,
  sortSites,
  sparkline,
  summarizeSites,
  validateSiteForm,
  type SiteForm,
  type SiteTone,
} from "../../state/settings/clientSitesState";
import {
  AlertCircle,
  Check,
  ExternalLink,
  Globe,
  Loader,
  Play,
  Plus,
  RotateCcw,
  Trash,
  Upload,
  X,
} from "../primitives/icons";

const inputClass =
  "w-full h-9 rounded-md bg-black/30 border border-white/10 px-2.5 text-[13px] text-ink-100 " +
  "placeholder:text-ink-400 focus:outline-none focus:border-accent-blue";

const TONE_DOT: Record<SiteTone, string> = {
  green: "bg-accent-green",
  amber: "bg-accent-yellow",
  red: "bg-accent-red",
  grey: "bg-ink-400",
};

const TONE_TEXT: Record<SiteTone, string> = {
  green: "text-accent-green",
  amber: "text-accent-yellow",
  red: "text-accent-red",
  grey: "text-ink-300",
};

/**
 * Settings → Insights → Client sites.
 *
 * This watches the operator's *clients'* websites, not this server — the
 * platform's own liveness is Settings → Monitoring. It is deliberately the
 * cheapest thing that can honestly say a shop is down: one HEAD request per
 * site per interval, from this host, with no agent involved and therefore no
 * tokens spent.
 *
 * Members see the table read-only, and only the sites linked to a project
 * they belong to; the server does that filtering, so this component never has
 * to decide what to hide.
 */
export function ClientSitesSettings({
  isAdmin,
  projects,
}: {
  isAdmin: boolean;
  projects: ProjectMeta[];
}) {
  const sites = useClientSites(true);
  const [editing, setEditing] = useState<{ id: string | null; form: SiteForm } | null>(null);
  const [importing, setImporting] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState<string | null>(null);

  const rows = sortSites(sites.sites);
  const projectName = (id: string | undefined) =>
    projects.find((project) => project.id === id)?.name ?? "";

  async function submit(event: Event) {
    event.preventDefault();
    if (!editing) return;
    const problem = validateSiteForm(editing.form, sites.bounds, sites.maxExtraUrls);
    if (problem) {
      setFormError(problem);
      return;
    }
    setFormError(null);
    const input = formToInput(editing.form);
    const ok = editing.id
      ? await sites.update(editing.id, input)
      : await sites.create(input);
    if (ok) setEditing(null);
  }

  return (
    <div class="space-y-4">
      <section class="rounded-lg border border-white/10 bg-[#101318] overflow-hidden">
        <header class="px-4 py-3 flex items-start gap-3 border-b border-white/[0.06]">
          <div class="h-9 w-9 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none">
            <Globe class="w-4 h-4 text-ink-200" />
          </div>
          <div class="flex-1 min-w-0">
            <div class="text-[14.5px] font-semibold text-ink-50">Client sites</div>
            <div class="text-[12px] text-ink-300 mt-1 leading-relaxed">
              An always-on watcher for the websites you built for other people. Each site gets one{" "}
              <code class="rounded bg-black/30 border border-white/10 px-1 py-0.5 text-[11px]">HEAD</code>{" "}
              request per interval from this server — no agent runs, no tokens, no container time.
              You are alerted after two consecutive failures, and again when it comes back.
            </div>
          </div>
          <button
            type="button"
            onClick={() => void sites.refresh()}
            disabled={sites.refreshing || sites.loading}
            class="h-8 flex-none px-2.5 rounded-md border border-white/10 text-ink-200 text-[12px]
                   hover:bg-white/[0.07] disabled:opacity-50 inline-flex items-center gap-1.5"
            title="Refresh now"
          >
            <RotateCcw class={`w-3.5 h-3.5${sites.refreshing ? " animate-spin" : ""}`} />
            <span class="hidden sm:inline">Refresh</span>
          </button>
        </header>

        <div class="p-3 space-y-3">
          <div class="flex flex-wrap items-center gap-2">
            <span class="text-[12.5px] text-ink-300">{summarizeSites(rows)}</span>
            <span class="flex-1" />
            {isAdmin && (
              <>
                <button
                  type="button"
                  onClick={() => {
                    setImporting((open) => !open);
                    setEditing(null);
                  }}
                  disabled={rows.length >= sites.maxSites}
                  class="h-8 px-2.5 rounded-md border border-white/10 text-ink-200 text-[12px]
                         hover:bg-white/[0.07] disabled:opacity-50 inline-flex items-center gap-1.5"
                >
                  <Upload class="w-3.5 h-3.5" /> Bulk import
                </button>
                <button
                  type="button"
                  onClick={() => {
                    setEditing({ id: null, form: emptySiteForm() });
                    setImporting(false);
                    setFormError(null);
                  }}
                  disabled={rows.length >= sites.maxSites}
                  class="h-8 px-2.5 rounded-md bg-accent-blue text-white text-[12px] font-medium
                         hover:brightness-110 disabled:opacity-50 inline-flex items-center gap-1.5"
                  title={
                    rows.length >= sites.maxSites
                      ? `This server watches at most ${sites.maxSites} sites`
                      : undefined
                  }
                >
                  <Plus class="w-3.5 h-3.5" /> Add site
                </button>
              </>
            )}
          </div>

          {sites.error && (
            <p class="flex items-start gap-2 rounded-md border border-accent-red/30 bg-accent-red/5 p-2.5 text-[12px] text-accent-red leading-relaxed">
              <AlertCircle class="w-3.5 h-3.5 mt-0.5 flex-none" />
              <span class="min-w-0 break-words">{sites.error}</span>
            </p>
          )}

          {importing && isAdmin && (
            <ImportPanel
              projects={projects}
              saving={sites.saving}
              onImport={sites.importSites}
              onCandidates={sites.loadCandidates}
              onClose={() => setImporting(false)}
            />
          )}

          {editing && isAdmin && (
            <SiteEditor
              form={editing.form}
              editingExisting={editing.id !== null}
              projects={projects}
              bounds={sites.bounds}
              maxExtraUrls={sites.maxExtraUrls}
              saving={sites.saving}
              error={formError}
              onChange={(form) => setEditing({ id: editing.id, form })}
              onSubmit={submit}
              onCancel={() => {
                setEditing(null);
                setFormError(null);
              }}
            />
          )}

          {sites.report && (
            <CheckReportPanel report={sites.report} onDismiss={sites.dismissReport} />
          )}

          {sites.loading && rows.length === 0 ? (
            <TableSkeleton />
          ) : rows.length === 0 ? (
            <p class="rounded-md border border-white/10 bg-white/[0.03] px-3 py-6 text-center text-[12.5px] leading-relaxed text-ink-300">
              {isAdmin
                ? "No client sites yet. Add one, or paste a list of addresses with Bulk import."
                : "No client sites are linked to your projects."}
            </p>
          ) : (
            <div class="overflow-x-auto rounded-md border border-white/10">
              <table class="w-full min-w-[860px] text-[12.5px]">
                <thead>
                  <tr class="text-left text-[11px] uppercase tracking-wide text-ink-400">
                    <th class="px-3 py-2 font-medium">Site</th>
                    <th class="px-3 py-2 font-medium">Status</th>
                    <th class="px-3 py-2 font-medium">Last check</th>
                    <th class="px-3 py-2 font-medium">Response</th>
                    <th class="px-3 py-2 font-medium">Uptime 24h / 7d</th>
                    <th class="px-3 py-2 font-medium">TLS</th>
                    <th class="px-3 py-2 font-medium">Next</th>
                    <th class="px-3 py-2 font-medium text-right">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((site) => (
                    <SiteRow
                      key={site.id}
                      site={site}
                      now={sites.now}
                      isAdmin={isAdmin}
                      projectName={projectName(site.projectId)}
                      checking={sites.checkingId === site.id}
                      confirming={confirmDelete === site.id}
                      onCheck={() => void sites.checkNow(site.id)}
                      onEdit={() => {
                        setEditing({ id: site.id, form: siteToForm(site) });
                        setImporting(false);
                        setFormError(null);
                      }}
                      onDelete={() => {
                        if (confirmDelete !== site.id) {
                          setConfirmDelete(site.id);
                          return;
                        }
                        setConfirmDelete(null);
                        void sites.remove(site.id);
                      }}
                      onCancelDelete={() => setConfirmDelete(null)}
                    />
                  ))}
                </tbody>
              </table>
            </div>
          )}

          <p class="text-[11.5px] leading-relaxed text-ink-400">
            Checks run on this server's bandwidth, so watching {sites.maxSites} sites every minute
            is real outbound traffic. The default five-minute interval is the cheap setting; keep it
            unless a client's contract needs faster.
          </p>
        </div>
      </section>
    </div>
  );
}

function SiteRow({
  site,
  now,
  isAdmin,
  projectName,
  checking,
  confirming,
  onCheck,
  onEdit,
  onDelete,
  onCancelDelete,
}: {
  site: WatchedSiteView;
  now: number;
  isAdmin: boolean;
  projectName: string;
  checking: boolean;
  confirming: boolean;
  onCheck: () => void;
  onEdit: () => void;
  onDelete: () => void;
  onCancelDelete: () => void;
}) {
  const dot = siteDot(site);
  const tls = describeTls(site);

  return (
    <tr class="border-t border-white/[0.06] align-middle">
      <td class="px-3 py-2">
        <div class="flex items-center gap-2 min-w-0">
          <span class="grid h-3 w-3 flex-none place-items-center" title={dot.title}>
            <span class={`block h-2 w-2 rounded-full ${TONE_DOT[dot.tone]}`} aria-hidden="true" />
          </span>
          <span class="min-w-0">
            <span class="block truncate text-ink-100">{siteName(site)}</span>
            <a
              href={site.url}
              target="_blank"
              rel="noopener noreferrer"
              class="inline-flex items-center gap-1 text-[11.5px] text-ink-400 hover:text-accent-blue"
            >
              {siteHost(site.url)} <ExternalLink class="w-3 h-3" />
            </a>
            {projectName && (
              <span class="ml-1.5 text-[11px] text-ink-400">· {projectName}</span>
            )}
          </span>
        </div>
      </td>
      <td class={`px-3 py-2 whitespace-nowrap ${TONE_TEXT[dot.tone]}`} title={dot.title}>
        {dot.label}
        {site.lastCode ? <span class="ml-1 text-ink-400">{site.lastCode}</span> : null}
      </td>
      <td class="px-3 py-2 whitespace-nowrap text-ink-300">{formatAgo(site.lastCheckedAt, now)}</td>
      <td class="px-3 py-2 whitespace-nowrap">
        <div class="flex items-center gap-2">
          <span class="tabular-nums text-ink-200">{formatMs(site.lastDurationMs)}</span>
          <Sparkline points={site.spark} />
        </div>
      </td>
      <td class="px-3 py-2 whitespace-nowrap tabular-nums text-ink-200">
        {formatUptime(site.uptime?.day)}
        <span class="text-ink-400"> / {formatUptime(site.uptime?.week)}</span>
      </td>
      <td class={`px-3 py-2 whitespace-nowrap tabular-nums ${TONE_TEXT[tls.tone]}`}>{tls.label}</td>
      <td class="px-3 py-2 whitespace-nowrap text-ink-400">
        {site.enabled ? formatCountdown(site.nextCheckAt, now) : "paused"}
      </td>
      <td class="px-3 py-2">
        <div class="flex items-center justify-end gap-1.5">
          <button
            type="button"
            onClick={onCheck}
            disabled={checking}
            class="h-7 px-2 rounded-md border border-white/10 text-ink-200 text-[11.5px]
                   hover:bg-white/[0.07] disabled:opacity-50 inline-flex items-center gap-1"
            title="Run every check for this site now and show the raw results"
          >
            {checking ? (
              <Loader class="w-3 h-3 animate-spin" />
            ) : (
              <Play class="w-3 h-3" />
            )}
            Check now
          </button>
          {isAdmin && (
            <>
              <button
                type="button"
                onClick={onEdit}
                class="h-7 px-2 rounded-md border border-white/10 text-ink-200 text-[11.5px] hover:bg-white/[0.07]"
              >
                Edit
              </button>
              {confirming ? (
                <span class="inline-flex items-center gap-1">
                  <button
                    type="button"
                    onClick={onDelete}
                    class="h-7 px-2 rounded-md border border-accent-red/40 text-accent-red text-[11.5px] hover:bg-accent-red/10"
                  >
                    Remove
                  </button>
                  <button
                    type="button"
                    onClick={onCancelDelete}
                    class="grid h-7 w-7 place-items-center rounded-md text-ink-300 hover:bg-white/[0.07]"
                    aria-label="Cancel"
                  >
                    <X class="w-3.5 h-3.5" />
                  </button>
                </span>
              ) : (
                <button
                  type="button"
                  onClick={onDelete}
                  class="grid h-7 w-7 place-items-center rounded-md text-ink-300 hover:bg-white/[0.07] hover:text-accent-red"
                  aria-label={`Stop watching ${siteName(site)}`}
                >
                  <Trash class="w-3.5 h-3.5" />
                </button>
              )}
            </>
          )}
        </div>
      </td>
    </tr>
  );
}

/**
 * The response-time sparkline. Inline SVG with no library: forty points per
 * row across up to two hundred rows is exactly the case a charting dependency
 * would make slow. Failed checks are drawn as red ticks along the baseline
 * rather than as a fast response.
 */
function Sparkline({ points }: { points?: number[] }) {
  const chart = sparkline(points);
  if (!chart.path && chart.failures.length === 0) {
    return <span class="text-ink-400" aria-hidden="true">—</span>;
  }
  return (
    <svg
      width={chart.width}
      height={chart.height}
      viewBox={`0 0 ${chart.width} ${chart.height}`}
      class="flex-none overflow-visible"
      role="img"
      aria-label={`Response time trend, peaking at ${formatMs(chart.peakMs)}`}
    >
      <title>{`Response time, newest at the right — peak ${formatMs(chart.peakMs)}`}</title>
      {chart.path && (
        <path
          d={chart.path}
          fill="none"
          stroke="currentColor"
          stroke-width="1.25"
          stroke-linejoin="round"
          stroke-linecap="round"
          class="text-accent-blue"
        />
      )}
      {chart.failures.map((x) => (
        <line
          key={x}
          x1={x}
          y1={chart.height - 1}
          x2={x}
          y2={1}
          stroke="currentColor"
          stroke-width="1"
          class="text-accent-red/60"
        />
      ))}
    </svg>
  );
}

function SiteEditor({
  form,
  editingExisting,
  projects,
  bounds,
  maxExtraUrls,
  saving,
  error,
  onChange,
  onSubmit,
  onCancel,
}: {
  form: SiteForm;
  editingExisting: boolean;
  projects: ProjectMeta[];
  bounds: { min: number; max: number };
  maxExtraUrls: number;
  saving: boolean;
  error: string | null;
  onChange: (form: SiteForm) => void;
  onSubmit: (event: Event) => void;
  onCancel: () => void;
}) {
  const set = <K extends keyof SiteForm>(key: K, value: SiteForm[K]) =>
    onChange({ ...form, [key]: value });

  return (
    <form
      onSubmit={onSubmit}
      class="rounded-md border border-white/10 bg-white/[0.03] p-3 space-y-3"
    >
      <div class="text-[13px] font-semibold text-ink-100">
        {editingExisting ? "Edit site" : "Add a client site"}
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        <label class="block space-y-1">
          <span class="text-[11.5px] text-ink-300">Address</span>
          <input
            value={form.url}
            onInput={(event) => set("url", (event.currentTarget as HTMLInputElement).value)}
            placeholder="shop.example.com"
            autocomplete="off"
            spellcheck={false}
            class={inputClass}
          />
        </label>
        <label class="block space-y-1">
          <span class="text-[11.5px] text-ink-300">Label (optional)</span>
          <input
            value={form.label}
            onInput={(event) => set("label", (event.currentTarget as HTMLInputElement).value)}
            placeholder="Acme shop"
            class={inputClass}
          />
        </label>
        <label class="block space-y-1">
          <span class="text-[11.5px] text-ink-300">
            Check every ({bounds.min}–{bounds.max} minutes)
          </span>
          <input
            type="number"
            min={bounds.min}
            max={bounds.max}
            step={1}
            value={form.intervalMinutes}
            onInput={(event) =>
              set(
                "intervalMinutes",
                Number.parseInt((event.currentTarget as HTMLInputElement).value, 10),
              )
            }
            class={inputClass}
          />
        </label>
        <label class="block space-y-1">
          <span class="text-[11.5px] text-ink-300">Project (who can see this site)</span>
          <select
            value={form.projectId}
            onChange={(event) => set("projectId", (event.currentTarget as HTMLSelectElement).value)}
            class={inputClass}
          >
            <option value="">Not linked — admins only</option>
            {projects.map((project) => (
              <option key={project.id} value={project.id}>
                {project.name}
              </option>
            ))}
          </select>
        </label>
        <label class="block space-y-1">
          <span class="text-[11.5px] text-ink-300">Request method</span>
          <select
            value={form.method}
            onChange={(event) =>
              set("method", (event.currentTarget as HTMLSelectElement).value as "HEAD" | "GET")
            }
            class={inputClass}
          >
            <option value="HEAD">HEAD — cheapest, falls back to GET if refused</option>
            <option value="GET">GET — always download the page</option>
          </select>
        </label>
        <label class="block space-y-1">
          <span class="text-[11.5px] text-ink-300">Expected status (blank = any 2xx/3xx)</span>
          <input
            value={form.expectStatus}
            onInput={(event) =>
              set("expectStatus", (event.currentTarget as HTMLInputElement).value)
            }
            placeholder="200"
            inputMode="numeric"
            class={inputClass}
          />
        </label>
        <label class="block space-y-1">
          <span class="text-[11.5px] text-ink-300">Page must contain (optional)</span>
          <input
            value={form.mustContain}
            onInput={(event) => set("mustContain", (event.currentTarget as HTMLInputElement).value)}
            placeholder="Add to cart"
            class={inputClass}
          />
        </label>
        <label class="block space-y-1">
          <span class="text-[11.5px] text-ink-300">Page must not contain (optional)</span>
          <input
            value={form.mustNotContain}
            onInput={(event) =>
              set("mustNotContain", (event.currentTarget as HTMLInputElement).value)
            }
            placeholder="Error establishing a database connection"
            class={inputClass}
          />
        </label>
        <label class="block space-y-1">
          <span class="text-[11.5px] text-ink-300">Warn this many days before the certificate expires (0 = off)</span>
          <input
            type="number"
            min={0}
            max={90}
            step={1}
            value={form.tlsWarnDays}
            onInput={(event) =>
              set(
                "tlsWarnDays",
                Number.parseInt((event.currentTarget as HTMLInputElement).value, 10) || 0,
              )
            }
            class={inputClass}
          />
        </label>
        <label class="block space-y-1">
          <span class="text-[11.5px] text-ink-300">Slow above (ms, blank = never)</span>
          <input
            value={form.maxResponseMs}
            onInput={(event) =>
              set("maxResponseMs", (event.currentTarget as HTMLInputElement).value)
            }
            placeholder="2000"
            inputMode="numeric"
            class={inputClass}
          />
        </label>
      </div>

      <fieldset class="space-y-2">
        <legend class="text-[11.5px] text-ink-300">
          Extra pages to watch (up to {maxExtraUrls}) — the checkout page, the login page
        </legend>
        {form.extraUrls.map((extra, index) => (
          <div key={index} class="flex flex-wrap items-center gap-2">
            <input
              value={extra.label}
              onInput={(event) => {
                const next = [...form.extraUrls];
                next[index] = { ...extra, label: (event.currentTarget as HTMLInputElement).value };
                set("extraUrls", next);
              }}
              placeholder="Checkout"
              class={`${inputClass} sm:w-40`}
            />
            <input
              value={extra.url}
              onInput={(event) => {
                const next = [...form.extraUrls];
                next[index] = { ...extra, url: (event.currentTarget as HTMLInputElement).value };
                set("extraUrls", next);
              }}
              placeholder="shop.example.com/checkout"
              class={`${inputClass} flex-1 min-w-[200px]`}
            />
            <button
              type="button"
              onClick={() => set("extraUrls", form.extraUrls.filter((_, at) => at !== index))}
              class="grid h-9 w-9 flex-none place-items-center rounded-md text-ink-300 hover:bg-white/[0.07]"
              aria-label="Remove this page"
            >
              <X class="w-3.5 h-3.5" />
            </button>
          </div>
        ))}
        {form.extraUrls.length < maxExtraUrls && (
          <button
            type="button"
            onClick={() => set("extraUrls", [...form.extraUrls, { label: "", url: "" }])}
            class="h-8 px-2.5 rounded-md border border-white/10 text-ink-200 text-[12px] hover:bg-white/[0.07] inline-flex items-center gap-1.5"
          >
            <Plus class="w-3.5 h-3.5" /> Add a page
          </button>
        )}
      </fieldset>

      <fieldset class="space-y-2">
        <legend class="text-[11.5px] text-ink-300">
          Extra request headers (for a staging site behind a shared token)
        </legend>
        {form.headers.map((header, index) => (
          <div key={index} class="flex flex-wrap items-center gap-2">
            <input
              value={header.name}
              onInput={(event) => {
                const next = [...form.headers];
                next[index] = { ...header, name: (event.currentTarget as HTMLInputElement).value };
                set("headers", next);
              }}
              placeholder="X-Monitor-Token"
              class={`${inputClass} sm:w-52`}
            />
            <input
              value={header.value}
              onInput={(event) => {
                const next = [...form.headers];
                next[index] = { ...header, value: (event.currentTarget as HTMLInputElement).value };
                set("headers", next);
              }}
              placeholder="value"
              class={`${inputClass} flex-1 min-w-[160px]`}
            />
            <button
              type="button"
              onClick={() => set("headers", form.headers.filter((_, at) => at !== index))}
              class="grid h-9 w-9 flex-none place-items-center rounded-md text-ink-300 hover:bg-white/[0.07]"
              aria-label="Remove this header"
            >
              <X class="w-3.5 h-3.5" />
            </button>
          </div>
        ))}
        <button
          type="button"
          onClick={() => set("headers", [...form.headers, { name: "", value: "" }])}
          class="h-8 px-2.5 rounded-md border border-white/10 text-ink-200 text-[12px] hover:bg-white/[0.07] inline-flex items-center gap-1.5"
        >
          <Plus class="w-3.5 h-3.5" /> Add a header
        </button>
      </fieldset>

      <div class="flex flex-wrap gap-3">
        <label class="inline-flex items-center gap-2 text-[12.5px] text-ink-200">
          <input
            type="checkbox"
            checked={form.enabled}
            onChange={(event) => set("enabled", (event.currentTarget as HTMLInputElement).checked)}
            class="h-4 w-4 accent-accent-blue"
          />
          Watch this site
        </label>
        <label class="inline-flex items-center gap-2 text-[12.5px] text-ink-200">
          <input
            type="checkbox"
            checked={form.notify}
            onChange={(event) => set("notify", (event.currentTarget as HTMLInputElement).checked)}
            class="h-4 w-4 accent-accent-blue"
          />
          Send notifications for this site
        </label>
      </div>

      {error && (
        <p class="rounded-md border border-accent-red/30 bg-accent-red/5 p-2.5 text-[12px] text-accent-red leading-relaxed">
          {error}
        </p>
      )}

      <div class="flex flex-wrap items-center gap-2">
        <button
          type="submit"
          disabled={saving}
          class="h-9 px-3 rounded-md bg-accent-blue text-white text-[13px] font-medium
                 hover:brightness-110 disabled:opacity-50 inline-flex items-center gap-1.5"
        >
          {saving && <Loader class="w-3.5 h-3.5 animate-spin" />}
          {editingExisting ? "Save" : "Add site"}
        </button>
        <button
          type="button"
          onClick={onCancel}
          class="h-9 px-3 rounded-md border border-white/10 text-ink-200 text-[13px] hover:bg-white/[0.07]"
        >
          Cancel
        </button>
      </div>
    </form>
  );
}

/**
 * Bulk import: paste a list, or take the domains the projects already store
 * in their own HESTIA_DOMAIN-style secrets. Everything created gets the
 * defaults, so forty client sites are one action rather than forty forms.
 */
function ImportPanel({
  projects,
  saving,
  onImport,
  onCandidates,
  onClose,
}: {
  projects: ProjectMeta[];
  saving: boolean;
  onImport: (input: {
    urls: string;
    fromProjects: boolean;
    projectId?: string;
    notify: boolean;
  }) => Promise<SiteImportResult | null>;
  onCandidates: () => Promise<SiteCandidate[]>;
  onClose: () => void;
}) {
  const [urls, setUrls] = useState("");
  const [fromProjects, setFromProjects] = useState(false);
  const [projectId, setProjectId] = useState("");
  const [notify, setNotify] = useState(true);
  const [candidates, setCandidates] = useState<SiteCandidate[] | null>(null);
  const [result, setResult] = useState<SiteImportResult | null>(null);

  useEffect(() => {
    if (!fromProjects || candidates !== null) return;
    void onCandidates().then(setCandidates);
  }, [fromProjects, candidates, onCandidates]);

  const parsed = parsePastedUrls(urls);
  const total = parsed.length + (fromProjects ? (candidates?.length ?? 0) : 0);

  async function run(event: Event) {
    event.preventDefault();
    const outcome = await onImport({ urls, fromProjects, projectId, notify });
    setResult(outcome);
    if (outcome && outcome.created.length > 0) {
      setUrls("");
      setCandidates(null);
    }
  }

  return (
    <form onSubmit={run} class="rounded-md border border-white/10 bg-white/[0.03] p-3 space-y-3">
      <div class="flex items-center gap-2">
        <span class="text-[13px] font-semibold text-ink-100">Bulk import</span>
        <span class="flex-1" />
        <button
          type="button"
          onClick={onClose}
          class="grid h-7 w-7 place-items-center rounded-md text-ink-300 hover:bg-white/[0.07]"
          aria-label="Close bulk import"
        >
          <X class="w-3.5 h-3.5" />
        </button>
      </div>

      <label class="block space-y-1">
        <span class="text-[11.5px] text-ink-300">
          One address per line. Blank lines and anything after a “#” are ignored.
        </span>
        <textarea
          value={urls}
          onInput={(event) => setUrls((event.currentTarget as HTMLTextAreaElement).value)}
          rows={5}
          spellcheck={false}
          placeholder={"shop.example.com\nblog.example.com   # the client's news site\nhttps://app.example.com/health"}
          class="w-full rounded-md bg-black/30 border border-white/10 px-2.5 py-2 text-[12.5px]
                 font-mono text-ink-100 placeholder:text-ink-400 focus:outline-none focus:border-accent-blue"
        />
      </label>

      <label class="flex items-start gap-2.5 rounded-md border border-white/10 bg-white/[0.03] p-2.5 cursor-pointer">
        <input
          type="checkbox"
          checked={fromProjects}
          onChange={(event) => setFromProjects((event.currentTarget as HTMLInputElement).checked)}
          class="mt-0.5 h-4 w-4 flex-none accent-accent-blue"
        />
        <span class="min-w-0">
          <span class="block text-[12.5px] text-ink-100">Also take domains from the projects</span>
          <span class="block text-[12px] text-ink-300 leading-relaxed">
            Reads each project's own{" "}
            <code class="rounded bg-black/30 border border-white/10 px-1 py-0.5 text-[11px]">
              HESTIA_DOMAIN
            </code>
            -style secrets and links every site it finds to the project it came from, so that
            project's members can see it.
          </span>
          {fromProjects && candidates !== null && (
            <span class="mt-1 block text-[11.5px] text-ink-400">
              {candidates.length === 0
                ? "No new domains found in any project's secrets."
                : `${candidates.length} new domain${candidates.length === 1 ? "" : "s"}: ${candidates
                    .slice(0, 6)
                    .map((candidate) => siteHost(candidate.url))
                    .join(", ")}${candidates.length > 6 ? "…" : ""}`}
            </span>
          )}
        </span>
      </label>

      <div class="grid gap-3 sm:grid-cols-2">
        <label class="block space-y-1">
          <span class="text-[11.5px] text-ink-300">Link the pasted sites to a project</span>
          <select
            value={projectId}
            onChange={(event) => setProjectId((event.currentTarget as HTMLSelectElement).value)}
            class={inputClass}
          >
            <option value="">Not linked — admins only</option>
            {projects.map((project) => (
              <option key={project.id} value={project.id}>
                {project.name}
              </option>
            ))}
          </select>
        </label>
        <label class="inline-flex items-center gap-2 self-end pb-1 text-[12.5px] text-ink-200">
          <input
            type="checkbox"
            checked={notify}
            onChange={(event) => setNotify((event.currentTarget as HTMLInputElement).checked)}
            class="h-4 w-4 accent-accent-blue"
          />
          Send notifications for the imported sites
        </label>
      </div>

      {result && (
        <div class="space-y-1 rounded-md border border-white/10 bg-black/20 p-2.5 text-[12px] leading-relaxed">
          <div class="text-accent-green">
            Added {result.created.length} site{result.created.length === 1 ? "" : "s"}.
          </div>
          {(result.skipped ?? []).map((item) => (
            <div key={item.url} class="text-ink-300 break-words">
              Skipped <span class="text-ink-200">{item.url}</span> — {item.reason}
            </div>
          ))}
        </div>
      )}

      <button
        type="submit"
        disabled={saving || total === 0}
        class="h-9 px-3 rounded-md bg-accent-blue text-white text-[13px] font-medium
               hover:brightness-110 disabled:opacity-50 inline-flex items-center gap-1.5"
      >
        {saving && <Loader class="w-3.5 h-3.5 animate-spin" />}
        {total === 0 ? "Nothing to import yet" : `Import ${total} site${total === 1 ? "" : "s"}`}
      </button>
    </form>
  );
}

/** The raw results of a "Check now": every URL, what it answered, how long
 *  it took, and exactly which rule it failed. */
function CheckReportPanel({
  report,
  onDismiss,
}: {
  report: SiteCheckReport;
  onDismiss: () => void;
}) {
  return (
    <section class="rounded-md border border-white/10 bg-black/20 p-3 space-y-2">
      <div class="flex items-center gap-2">
        <span class="text-[13px] font-semibold text-ink-100">
          Check of {siteName(report.site)}
        </span>
        <span class="text-[11.5px] text-ink-400">
          {new Date(report.checkedAt).toLocaleString()}
        </span>
        <span class="flex-1" />
        <button
          type="button"
          onClick={onDismiss}
          class="grid h-7 w-7 place-items-center rounded-md text-ink-300 hover:bg-white/[0.07]"
          aria-label="Dismiss the check result"
        >
          <X class="w-3.5 h-3.5" />
        </button>
      </div>
      <div class="space-y-1.5">
        {report.endpoints.map((endpoint) => {
          const tone: SiteTone =
            endpoint.status === "up" ? "green" : endpoint.status === "slow" ? "amber" : "red";
          return (
            <div key={endpoint.url} class="rounded border border-white/[0.06] bg-white/[0.02] p-2">
              <div class="flex flex-wrap items-center gap-x-3 gap-y-1 text-[12px]">
                <span class={`inline-flex items-center gap-1.5 ${TONE_TEXT[tone]}`}>
                  <span class={`h-2 w-2 rounded-full ${TONE_DOT[tone]}`} aria-hidden="true" />
                  {endpoint.status}
                </span>
                {endpoint.label && <span class="text-ink-200">{endpoint.label}</span>}
                <span class="font-mono text-[11.5px] text-ink-300 break-all">{endpoint.url}</span>
                <span class="text-ink-400">{endpoint.method}</span>
                {endpoint.code ? <span class="tabular-nums text-ink-200">HTTP {endpoint.code}</span> : null}
                <span class="tabular-nums text-ink-300">{formatMs(endpoint.durationMs)}</span>
                {endpoint.sizeBytes ? (
                  <span class="tabular-nums text-ink-400">{endpoint.sizeBytes} B</span>
                ) : null}
                {endpoint.tlsDaysLeft !== undefined && (
                  <span class="text-ink-400">cert {endpoint.tlsDaysLeft} d left</span>
                )}
              </div>
              {(endpoint.reasons ?? []).map((reason) => (
                <div key={reason} class="mt-1 text-[12px] text-accent-red leading-relaxed break-words">
                  {reason}
                </div>
              ))}
              {endpoint.error && (
                <div class="mt-1 text-[12px] text-accent-red leading-relaxed break-words">
                  {endpoint.error}
                </div>
              )}
              {!endpoint.error && (endpoint.reasons ?? []).length === 0 && (
                <div class="mt-1 inline-flex items-center gap-1 text-[12px] text-accent-green">
                  <Check class="w-3.5 h-3.5" /> every check passed
                </div>
              )}
            </div>
          );
        })}
      </div>
    </section>
  );
}

function TableSkeleton() {
  return (
    <div class="space-y-2" aria-hidden="true">
      {Array.from({ length: 3 }, (_, index) => (
        <div key={index} class="flex items-center gap-3 rounded-md bg-white/[0.04] p-3">
          <span class="h-3 w-3 flex-none animate-pulse rounded-full bg-white/[0.07]" />
          <span class="flex-1 space-y-1.5">
            <span class="block h-3 w-1/4 animate-pulse rounded bg-white/[0.07]" />
            <span class="block h-2.5 w-1/2 animate-pulse rounded bg-white/[0.05]" />
          </span>
        </div>
      ))}
    </div>
  );
}
