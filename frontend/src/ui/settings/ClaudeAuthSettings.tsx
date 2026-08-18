import { useEffect, useRef } from "preact/hooks";
import { useAuthContext } from "../../state/context/AuthContext";
import { useClaudeLoginFlow } from "../../state/hooks/auth/useClaudeLoginFlow";
import { Check, ExternalLink, Key, Loader } from "../primitives/icons";

// Admin-only Claude auth pill, matching CodexAuthSettings/KimiAuthSettings.
// Self-contained: live status comes from the shared AuthContext WS (no extra
// socket) and the interactive login is driven by useClaudeLoginFlow. Unlike
// Codex's device grant, Claude prints an OAuth URL and needs a code pasted
// back, so the active state shows a link + input instead of a device code.
export function ClaudeAuthSettings() {
  const { claudeAuth } = useAuthContext();
  const login = useClaudeLoginFlow(() => {});
  const codeRef = useRef<HTMLTextAreaElement>(null);

  useEffect(() => {
    if (login.phase === "awaiting-code") {
      setTimeout(() => codeRef.current?.focus(), 50);
    }
  }, [login.phase]);

  const authenticated = claudeAuth.authenticated;
  const loading = claudeAuth.loading;
  const busy = login.phase === "starting" || login.phase === "submitting";
  const active = login.phase === "awaiting-code";
  const errorMessage = login.errorMessage || claudeAuth.error || claudeAuth.login?.error || "";

  return (
    <section class="rounded-md border border-white/10 bg-white/[0.03] p-3 space-y-3">
      <div class="flex items-start gap-3">
        <div class="h-9 w-9 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none text-ink-200">
          <Key class="w-4 h-4" />
        </div>
        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-2">
            <div class="text-[14px] font-semibold text-ink-100">Claude authentication</div>
            {loading ? (
              <Loader class="w-3.5 h-3.5 text-ink-300 animate-spin" />
            ) : authenticated ? (
              <span class="inline-flex items-center gap-1 text-[11px] text-accent-green">
                <Check class="w-3.5 h-3.5" /> Subscription signed in
              </span>
            ) : (
              <span class="text-[11px] text-ink-400">not configured</span>
            )}
          </div>
          <div class="text-[12px] text-ink-300 mt-1 leading-relaxed">
            Starts <span class="font-mono text-ink-100">claude auth login --claudeai</span> on the host.
            Sign in once with your Anthropic account; tokens land in{" "}
            <span class="font-mono text-ink-100">~/.claude/.credentials.json</span> and seed into every container.
          </div>
        </div>
      </div>

      <div class="flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={() => void login.startLogin()}
          disabled={busy || active}
          class="h-10 px-3 rounded-md bg-accent-blue text-ink-900 hover:bg-accent-blue/85 text-[13px] font-medium disabled:opacity-50"
        >
          {login.phase === "starting"
            ? "Starting..."
            : active
              ? "Login in progress"
              : authenticated
                ? "Refresh Claude login"
                : "Sign in with Claude"}
        </button>
        {loading && <Loader class="w-4 h-4 text-ink-300 animate-spin" />}
      </div>

      {active && (
        <div class="rounded-md border border-accent-blue/25 bg-accent-blue/[0.08] p-3 space-y-2">
          <div class="text-[12px] text-ink-200">
            Open the link, sign in, then paste the code Anthropic shows back here:
          </div>
          <a
            href={login.authUrl}
            target="_blank"
            rel="noreferrer"
            class="block break-all text-accent-blue hover:underline font-mono text-[12px] bg-black/30 border border-white/10 rounded px-2.5 py-2"
          >
            <ExternalLink class="w-3.5 h-3.5 inline mr-1 align-[-2px]" />
            {login.authUrl}
          </a>
          <textarea
            ref={codeRef}
            value={login.code}
            onInput={(event) => login.setCode((event.currentTarget as HTMLTextAreaElement).value)}
            onKeyDown={(event) => {
              if (event.key === "Enter" && !event.shiftKey && !event.isComposing) {
                event.preventDefault();
                void login.submitCode();
              }
            }}
            placeholder="Paste your code here"
            rows={2}
            autocomplete="off"
            autocapitalize="off"
            autocorrect="off"
            spellcheck={false}
            class="w-full resize-none rounded-md bg-black/30 border border-white/10 text-ink-100 placeholder:text-ink-300 px-3 py-2.5 font-mono text-[13px] focus:outline-none focus:border-accent-blue"
          />
          <div class="flex gap-2">
            <button
              type="button"
              onClick={() => void login.cancel()}
              class="px-3 h-10 text-[13px] text-ink-200 hover:text-ink-100 hover:bg-white/[0.08] rounded"
            >
              Cancel
            </button>
            <button
              type="button"
              onClick={() => void login.submitCode()}
              disabled={!login.code.trim()}
              class="flex-1 bg-accent-blue text-ink-900 hover:bg-accent-blue/85 disabled:opacity-50 text-[13px] font-medium rounded-md h-10"
            >
              Submit code
            </button>
          </div>
        </div>
      )}

      {login.phase === "submitting" && (
        <div class="flex items-center gap-2 text-[12px] text-ink-200">
          <Loader class="w-3.5 h-3.5 animate-spin" /> Finishing up
        </div>
      )}

      {errorMessage && !active && (
        <div class="text-[12px] text-accent-red bg-accent-red/[0.08] border border-accent-red/25 rounded px-2.5 py-2 break-words">
          {errorMessage}
        </div>
      )}
    </section>
  );
}
