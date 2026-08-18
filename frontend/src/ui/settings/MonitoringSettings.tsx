import { useState } from "preact/hooks";
import { useMonitoringSettings } from "../../state/hooks/settings/useMonitoringSettings";
import {
  describeLastPing,
  healthCheckUrl,
  minutesUntilNextPing,
} from "../../state/settings/monitoringState";
import { AlertCircle, Check, Copy, ExternalLink, Loader, Send, Server, X } from "../primitives/icons";

const inputClass =
  "w-full h-10 rounded-md bg-black/30 border border-white/10 px-3 text-sm text-ink-100 " +
  "placeholder:text-ink-400 focus:outline-none focus:border-accent-blue";

/**
 * External uptime monitoring. A box cannot alert about its own death from
 * inside, so this panel configures the two ways the outside world can notice:
 * a public endpoint an HTTP monitor polls, and an outbound heartbeat this
 * server pushes while it is healthy.
 */
export function MonitoringSettings() {
  const editor = useMonitoringSettings();
  const { settings } = editor;
  const lastPing = describeLastPing(settings);
  const dueInMinutes = minutesUntilNextPing(settings, Date.now());

  return (
    <div class="space-y-4">
      <HealthEndpointCard healthPath={settings?.healthPath ?? "/healthz"} />

      <section class="rounded-lg border border-white/10 bg-[#101318] overflow-hidden">
        <header class="px-4 py-3 flex items-start gap-3 border-b border-white/[0.06]">
          <div class="h-9 w-9 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none">
            <Send class="w-4 h-4 text-ink-200" />
          </div>
          <div class="flex-1 min-w-0">
            <div class="flex items-center gap-2">
              <div class="text-[14.5px] font-semibold text-ink-50">Heartbeat push</div>
              {editor.loading ? (
                <Loader class="w-3.5 h-3.5 text-ink-300 animate-spin" />
              ) : settings?.enabled ? (
                <span class="inline-flex items-center gap-1 text-[11px] text-accent-green">
                  <Check class="w-3.5 h-3.5" /> on
                </span>
              ) : (
                <span class="text-[11px] text-ink-400">off</span>
              )}
            </div>
            <div class="text-[12px] text-ink-300 mt-1 leading-relaxed">
              This server calls a URL you paste below on a timer, but only while it is healthy. If
              the box dies — or its data store or LXD stops answering — the calls stop and the
              monitoring service raises the alarm for you. It is the half a polling monitor cannot
              cover on its own.
            </div>
          </div>
        </header>

        <form onSubmit={editor.save} class="p-3 space-y-3">
          <label class="flex items-start gap-2.5 rounded-md border border-white/10 bg-white/[0.03] p-2.5 cursor-pointer">
            <input
              type="checkbox"
              checked={editor.enabled}
              onChange={(event) =>
                editor.setEnabled((event.currentTarget as HTMLInputElement).checked)
              }
              class="mt-0.5 h-4 w-4 flex-none accent-accent-blue"
            />
            <span class="min-w-0">
              <span class="block text-[13px] text-ink-100">Push a heartbeat</span>
              <span class="block text-[12px] text-ink-300 leading-relaxed">
                Add a URL below before turning this on. A failed push is logged and retried at the
                next interval, never in a tight loop.
              </span>
            </span>
          </label>

          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">Heartbeat URL</span>
            <input
              type="password"
              value={editor.heartbeatUrl}
              onInput={(event) =>
                editor.setHeartbeatUrl((event.currentTarget as HTMLInputElement).value)
              }
              placeholder={
                settings?.configured
                  ? `Stored (${settings.heartbeatUrlMasked}) — enter a new URL to replace it`
                  : "https://hc-ping.com/<your-uuid>"
              }
              autocomplete="off"
              spellcheck={false}
              class={inputClass}
            />
            <span class="flex flex-wrap items-center gap-2 text-[11.5px] text-ink-400">
              {settings?.configured ? (
                <>
                  <span>
                    A URL is stored for <span class="text-ink-200">{settings.heartbeatHost}</span>.
                    Leave this blank to keep it.
                  </span>
                  <button
                    type="button"
                    onClick={() => void editor.clearHeartbeatUrl()}
                    disabled={editor.saving}
                    class="inline-flex items-center gap-1 rounded border border-white/10 px-1.5 py-0.5
                           text-ink-200 hover:bg-white/[0.07] disabled:opacity-50"
                  >
                    <X class="w-3 h-3" /> Remove stored URL
                  </button>
                </>
              ) : (
                <span>
                  No URL stored yet. It is a bearer token — anyone holding it can tell your monitor
                  this box is alive — so it is kept at{" "}
                  <code class="rounded bg-black/30 border border-white/10 px-1 py-0.5 text-[11px]">
                    DATA_DIR/monitoring.json
                  </code>{" "}
                  with mode 0600 and never returned to this page.
                </span>
              )}
            </span>
          </label>

          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">Interval</span>
            <div class="flex items-center gap-2">
              <input
                type="number"
                min={editor.bounds.min}
                max={editor.bounds.max}
                step={1}
                value={editor.intervalMinutes}
                onInput={(event) =>
                  editor.setIntervalMinutes(
                    Number.parseInt((event.currentTarget as HTMLInputElement).value, 10),
                  )
                }
                class={`${inputClass} w-24`}
              />
              <span class="text-[12px] text-ink-300">
                minutes ({editor.bounds.min}–{editor.bounds.max}). Set your monitor's grace period
                to at least twice this.
              </span>
            </div>
          </label>

          <div class="rounded-md border border-white/10 bg-white/[0.03] p-2.5 space-y-1">
            <div class="flex flex-wrap items-center gap-2 text-[12px]">
              <span class="text-ink-300">Last push:</span>
              <span
                class={
                  lastPing.tone === "ok"
                    ? "text-accent-green"
                    : lastPing.tone === "failed"
                      ? "text-accent-red"
                      : "text-ink-400"
                }
              >
                {lastPing.label}
              </span>
              {lastPing.at && (
                <span class="text-ink-400">{new Date(lastPing.at).toLocaleString()}</span>
              )}
              {dueInMinutes !== undefined && (
                <span class="text-ink-400">
                  · next in {dueInMinutes === 0 ? "under a minute" : `~${dueInMinutes} min`}
                </span>
              )}
            </div>
            {lastPing.detail && (
              <div class="text-[12px] text-accent-red leading-relaxed break-words">
                {lastPing.detail}
              </div>
            )}
          </div>

          {editor.pingResult && (
            <div
              class={`flex items-start gap-2 rounded-md border p-2.5 text-[12px] leading-relaxed ${
                editor.pingResult.delivered
                  ? "border-accent-green/30 bg-accent-green/5 text-accent-green"
                  : "border-accent-red/30 bg-accent-red/5 text-accent-red"
              }`}
            >
              {editor.pingResult.delivered ? (
                <Check class="w-3.5 h-3.5 mt-0.5 flex-none" />
              ) : (
                <AlertCircle class="w-3.5 h-3.5 mt-0.5 flex-none" />
              )}
              <span class="min-w-0 break-words">
                {editor.pingResult.delivered
                  ? "Heartbeat delivered."
                  : `Heartbeat failed: ${editor.pingResult.error ?? "unknown error"}`}
              </span>
            </div>
          )}

          {editor.error && (
            <div class="rounded-md border border-accent-red/30 bg-accent-red/5 p-2.5 text-[12px] text-accent-red leading-relaxed break-words">
              {editor.error}
            </div>
          )}

          <div class="flex flex-wrap items-center gap-2 pt-1">
            <button
              type="submit"
              disabled={editor.saving || editor.loading}
              class="h-9 px-3 rounded-md bg-accent-blue text-white text-[13px] font-medium
                     hover:brightness-110 disabled:opacity-50 inline-flex items-center gap-1.5"
            >
              {editor.saving && <Loader class="w-3.5 h-3.5 animate-spin" />}
              Save
            </button>
            <button
              type="button"
              onClick={() => void editor.pingNow()}
              disabled={editor.pinging || !settings?.configured}
              class="h-9 px-3 rounded-md border border-white/10 text-ink-200 text-[13px]
                     hover:bg-white/[0.07] disabled:opacity-50 inline-flex items-center gap-1.5"
              title={settings?.configured ? undefined : "Save a heartbeat URL first"}
            >
              {editor.pinging ? (
                <Loader class="w-3.5 h-3.5 animate-spin" />
              ) : (
                <Send class="w-3.5 h-3.5" />
              )}
              Ping now
            </button>
            {editor.saved && (
              <span class="inline-flex items-center gap-1 text-[12px] text-accent-green">
                <Check class="w-3.5 h-3.5" /> Saved
              </span>
            )}
          </div>
        </form>
      </section>
    </div>
  );
}

/**
 * The polling half. There is nothing to configure here — the endpoint is
 * always on — so the card exists to hand the operator the exact URL and the
 * exact keyword, and to be honest about what a public endpoint gives away.
 */
function HealthEndpointCard({ healthPath }: { healthPath: string }) {
  const [copied, setCopied] = useState(false);
  const url = healthCheckUrl(
    typeof location === "undefined" ? "" : location.origin,
    healthPath,
  );

  async function copy() {
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard access can be refused; the URL is on screen either way.
    }
  }

  return (
    <section class="rounded-lg border border-white/10 bg-[#101318] overflow-hidden">
      <header class="px-4 py-3 flex items-start gap-3 border-b border-white/[0.06]">
        <div class="h-9 w-9 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none">
          <Server class="w-4 h-4 text-ink-200" />
        </div>
        <div class="flex-1 min-w-0">
          <div class="text-[14.5px] font-semibold text-ink-50">Health endpoint</div>
          <div class="text-[12px] text-ink-300 mt-1 leading-relaxed">
            Always on, no session required. It answers 200 while the data store and LXD are usable
            and 503 when either is not, so an external HTTP monitor can watch this server from
            outside.
          </div>
        </div>
      </header>

      <div class="p-3 space-y-3">
        <div class="flex flex-wrap items-center gap-2">
          <code class="flex-1 min-w-0 rounded-md bg-black/30 border border-white/10 px-2.5 py-2 text-[12.5px] text-ink-100 break-all">
            {url}
          </code>
          <button
            type="button"
            onClick={() => void copy()}
            class="h-9 px-3 rounded-md border border-white/10 text-ink-200 text-[13px]
                   hover:bg-white/[0.07] inline-flex items-center gap-1.5"
          >
            {copied ? <Check class="w-3.5 h-3.5" /> : <Copy class="w-3.5 h-3.5" />}
            {copied ? "Copied" : "Copy"}
          </button>
        </div>

        <div class="rounded-md border border-white/10 bg-white/[0.03] p-2.5 text-[12px] text-ink-300 leading-relaxed space-y-1.5">
          <div>
            In UptimeRobot, Better Stack, or any other HTTP monitor, watch this URL and add a
            keyword check for{" "}
            <code class="rounded bg-black/30 border border-white/10 px-1 py-0.5 text-[11.5px]">
              "status":"ok"
            </code>
            . A degraded platform still answers, so status code alone is not the whole story.
          </div>
          <div>
            Because it is public, it deliberately reveals only the application version — no
            hostname, no paths, no probe error text. Unauthenticated hits are rate limited to 60 a
            minute per IP.
          </div>
          <a
            href="https://docs.remote.futrx.com/operations/uptime-monitoring/"
            target="_blank"
            rel="noreferrer"
            class="inline-flex items-center gap-1 text-accent-blue hover:underline"
          >
            Uptime monitoring setup guide <ExternalLink class="w-3.5 h-3.5" />
          </a>
        </div>
      </div>
    </section>
  );
}
