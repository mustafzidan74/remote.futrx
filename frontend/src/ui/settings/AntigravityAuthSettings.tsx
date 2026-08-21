import { Check, Key, Loader } from "../primitives/icons";

/**
 * Antigravity's status card.
 *
 * There is no Connect button, and that is the point of having a separate
 * component: `agy` signs in through a terminal UI that never exits, so the
 * platform cannot drive the flow the way it drives Codex's or Kimi's. What it
 * can do is say whether a credential has been captured, and tell the operator
 * the three steps that capture one — once, not once per project.
 */
export function AntigravityAuthSettings({
  authenticated,
  loading,
  hint,
}: {
  authenticated: boolean;
  loading: boolean;
  hint?: string;
}) {
  return (
    <section class="rounded-md border border-white/10 bg-white/[0.03] p-3 space-y-3">
      <div class="flex items-start gap-3">
        <div class="h-9 w-9 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none text-ink-200">
          <Key class="w-4 h-4" />
        </div>
        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-2">
            <div class="text-[14px] font-semibold text-ink-100">Antigravity authentication</div>
            {loading ? (
              <Loader class="w-3.5 h-3.5 text-ink-300 animate-spin" />
            ) : authenticated ? (
              <span class="inline-flex items-center gap-1 text-[11px] text-accent-green">
                <Check class="w-3.5 h-3.5" /> Signed in · shared with every project
              </span>
            ) : (
              <span class="text-[11px] text-ink-400">not signed in</span>
            )}
          </div>
          <div class="text-[12px] text-ink-300 mt-1 leading-relaxed">
            Signs in with your Google AI subscription. Antigravity's sign-in runs in its own
            terminal UI, so it cannot be started from here — but you only do it once.
          </div>
        </div>
      </div>

      {!authenticated && !loading && (
        <ol class="text-[12px] text-ink-300 leading-relaxed list-decimal ps-5 space-y-1 rounded-md border border-white/10 bg-black/20 px-3 py-2.5">
          <li>
            Open any project, then its <span class="font-medium text-ink-100">Terminal</span>.
          </li>
          <li>
            Run <span class="font-mono text-ink-100">agy</span> and choose{" "}
            <span class="font-medium text-ink-100">Google OAuth</span>.
          </li>
          <li>Open the URL it prints, sign in, and paste the code back.</li>
        </ol>
      )}

      {authenticated && (
        <div class="text-[12px] text-ink-300 leading-relaxed rounded-md border border-white/10 bg-black/20 px-3 py-2.5">
          The sign-in was copied to the platform. New projects inherit it on launch — no second
          sign-in.
        </div>
      )}

      {hint && !authenticated && !loading && (
        <div class="text-[11.5px] text-ink-400 break-words">{hint}</div>
      )}
    </section>
  );
}
