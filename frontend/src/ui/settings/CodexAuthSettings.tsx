import type { CodexDeviceLogin } from "../../models/auth";
import { Check, ExternalLink, Key, Loader } from "../primitives/icons";

export function CodexAuthSettings({
  authenticated,
  usesApiKey,
  deviceLogin,
  loading,
  starting,
  error,
  onStartDeviceLogin,
}: {
  authenticated: boolean;
  usesApiKey: boolean;
  deviceLogin?: CodexDeviceLogin;
  loading: boolean;
  starting: boolean;
  error: string | null;
  onStartDeviceLogin: () => Promise<void>;
}) {
  const loginActive = !!deviceLogin?.active;
  const expiresAt = deviceLogin?.expiresAt
    ? new Date(deviceLogin.expiresAt * 1000).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" })
    : "";

  return (
    <section class="rounded-md border border-white/10 bg-white/[0.03] p-3 space-y-3">
      <div class="flex items-start gap-3">
        <div class="h-9 w-9 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none text-ink-200">
          <Key class="w-4 h-4" />
        </div>
        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-2">
            <div class="text-[14px] font-semibold text-ink-100">Codex authentication</div>
            {loading ? (
              <Loader class="w-3.5 h-3.5 text-ink-300 animate-spin" />
            ) : authenticated ? (
              <span class="inline-flex items-center gap-1 text-[11px] text-accent-green">
                <Check class="w-3.5 h-3.5" /> ChatGPT signed in
              </span>
            ) : usesApiKey ? (
              <span class="text-[11px] text-accent-red">API-key login detected</span>
            ) : (
              <span class="text-[11px] text-ink-400">not configured</span>
            )}
          </div>
          <div class="text-[12px] text-ink-300 mt-1 leading-relaxed">
            Starts <span class="font-mono text-ink-100">codex login --device-auth</span> on the host.
            This signs in with ChatGPT so Codex uses subscription limits, not API-key billing.
          </div>
        </div>
      </div>

      <div class="flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={() => void onStartDeviceLogin()}
          disabled={starting || loginActive}
          class="h-10 px-3 rounded-md bg-accent-blue text-ink-900 hover:bg-accent-blue/85 text-[13px] font-medium disabled:opacity-50"
        >
          {starting ? "Starting..." : loginActive ? "Login in progress" : authenticated ? "Refresh ChatGPT login" : "Sign in with ChatGPT"}
        </button>
        {loading && <Loader class="w-4 h-4 text-ink-300 animate-spin" />}
      </div>

      {usesApiKey && !authenticated && (
        <div class="text-[12px] text-accent-red bg-accent-red/[0.08] border border-accent-red/25 rounded px-2.5 py-2">
          Codex is currently logged in with an API key. Complete the ChatGPT login before running Codex so prompts use subscription limits.
        </div>
      )}

      {loginActive && (
        <div class="rounded-md border border-accent-blue/25 bg-accent-blue/[0.08] p-3 space-y-2">
          <div class="text-[12px] text-ink-200">Open the link and enter this code:</div>
          <div class="grid gap-2 sm:grid-cols-[1fr_auto]">
            <div class="font-mono text-[18px] tracking-wide text-ink-50 bg-black/30 border border-white/10 rounded px-3 py-2">
              {deviceLogin?.userCode || "Waiting for code..."}
            </div>
            {deviceLogin?.verificationUri && (
              <a
                href={deviceLogin.verificationUri}
                target="_blank"
                rel="noreferrer"
                class="h-10 px-3 rounded bg-white/[0.08] hover:bg-white/[0.12] text-ink-100 text-[13px] font-medium inline-flex items-center justify-center gap-2"
              >
                <ExternalLink class="w-4 h-4" /> Open
              </a>
            )}
          </div>
          {expiresAt && <div class="text-[11px] text-ink-400">Code expires around {expiresAt}.</div>}
        </div>
      )}

      {(deviceLogin?.error || error) && (
        <div class="text-[12px] text-accent-red bg-accent-red/[0.08] border border-accent-red/25 rounded px-2.5 py-2 break-words">
          {deviceLogin?.error || error}
        </div>
      )}
    </section>
  );
}
