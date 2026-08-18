import { useEffect, useRef, useState } from "preact/hooks";
import { VOICE_LANGUAGE_OPTIONS } from "../../../config/voice";
import type { VoiceInput } from "../../../state/hooks/chat/useVoiceInput";
import { formatElapsed } from "../../../state/chat/voiceInputState";
import {
  AlertCircle,
  Check,
  ChevronDown,
  Loader,
  Mic,
  Square,
  TestTube,
  X,
} from "../../primitives/icons";

/**
 * The composer's dictation control: a mic button, a menu for language, engine
 * and self-test, and the live state readout that tells the user their voice is
 * actually being heard.
 *
 * Nothing here ever sends the prompt. Dictated text lands in the textarea and
 * the user reviews it — an agent prompt is too consequential to fire on a
 * silence timeout.
 */
export function VoiceInputButton({ voice, disabled }: { voice: VoiceInput; disabled: boolean }) {
  const [menuOpen, setMenuOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const { session } = voice;

  useEffect(() => {
    if (!menuOpen) return;
    function closeOnOutsideClick(event: MouseEvent) {
      const target = event.target as Node | null;
      if (target && !rootRef.current?.contains(target)) setMenuOpen(false);
    }
    window.addEventListener("mousedown", closeOnOutsideClick);
    return () => window.removeEventListener("mousedown", closeOnOutsideClick);
  }, [menuOpen]);

  // No engine on this browser and no server fallback configured: there is
  // nothing the button could do, so it is not rendered at all.
  if (!voice.available) return null;

  const listening = session.status === "listening";
  const recording = session.status === "recording";
  const transcribing = session.status === "transcribing";
  const starting = session.status === "starting";
  const failed = session.status === "error";
  const busy = listening || recording || starting || transcribing;
  // The language is in the tooltip whatever the button is doing. "I spoke and
  // nothing happened" is very often "it was listening for the other language",
  // and that is invisible unless it is named where the user is already looking.
  const tooltip = !voice.engine
    ? "Voice input is unavailable in this browser"
    : busy
      ? `Stop dictation (Esc) · listening in ${voice.languageLabel}`
      : `Dictate a prompt · ${voice.languageLabel}`;

  return (
    <div ref={rootRef} class="codex-voice-control relative flex flex-none items-end gap-1">
      <div class="flex flex-none flex-col items-stretch gap-1">
        {busy && <VoiceStatusReadout voice={voice} />}
        {failed && (
          <div
            role="alert"
            class="flex max-w-[18rem] items-start gap-1.5 rounded-md border border-accent-red/30
                   bg-accent-red/[0.09] px-2 py-1.5 text-[11px] leading-4 text-ink-100"
          >
            <AlertCircle class="mt-0.5 h-3 w-3 flex-none text-accent-red" aria-hidden="true" />
            <span class="min-w-0 flex-1 break-words">{session.error}</span>
            <button
              type="button"
              onClick={voice.dismiss}
              class="grid h-4 w-4 flex-none place-items-center rounded text-ink-300 hover:bg-white/[0.08] hover:text-ink-50"
              aria-label="Dismiss voice input error"
            >
              <X class="h-2.5 w-2.5" />
            </button>
          </div>
        )}

        <div class="flex items-center gap-1">
          <button
            type="button"
            onClick={voice.toggle}
            disabled={disabled || !voice.engine || transcribing}
            aria-pressed={busy}
            aria-label={tooltip}
            title={tooltip}
            class={`codex-voice-button grid h-10 w-10 flex-none place-items-center rounded-lg transition
                    active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-50 disabled:active:scale-100
                    ${
                      busy
                        ? "bg-accent-red/90 text-ink-900 hover:bg-accent-red"
                        : "bg-white/[0.045] text-ink-300 hover:bg-white/10 hover:text-ink-100"
                    }`}
          >
            {transcribing ? (
              <Loader class="h-4 w-4 animate-spin" />
            ) : busy ? (
              <Square class="h-3.5 w-3.5" />
            ) : (
              <Mic class="h-4 w-4" />
            )}
          </button>

          <button
            type="button"
            onClick={() => setMenuOpen((open) => !open)}
            disabled={disabled}
            class={`grid h-10 w-5 flex-none place-items-center rounded-md text-ink-400 transition
                    hover:bg-white/[0.07] hover:text-ink-100 disabled:cursor-not-allowed disabled:opacity-50
                    ${menuOpen ? "bg-white/[0.08] text-ink-100" : ""}`}
            aria-haspopup="menu"
            aria-expanded={menuOpen}
            aria-label={`Voice input options. Language: ${voice.languageLabel}`}
          >
            <ChevronDown class="h-3 w-3" />
          </button>
        </div>
      </div>

      {menuOpen && (
        <div
          class="theme-menu-surface absolute bottom-full right-0 z-40 mb-1.5 max-h-[70vh] w-[min(19rem,calc(100vw-1.5rem))]
                 overflow-y-auto rounded-lg border border-white/10 bg-[#14161d] p-1 shadow-2xl"
          role="menu"
        >
          <div class="px-2.5 pb-1 pt-1.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-ink-400">
            Dictation language
          </div>
          {VOICE_LANGUAGE_OPTIONS.map((option) => {
            const active = option.value === voice.language;
            return (
              <button
                key={option.value}
                type="button"
                role="menuitemradio"
                aria-checked={active}
                onClick={() => {
                  voice.setLanguage(option.value);
                  setMenuOpen(false);
                }}
                // The Arabic labels render right-to-left inside a left-to-right
                // menu; dir="auto" lets each row pick its own direction.
                dir="auto"
                class={`flex w-full items-center gap-2 rounded-md px-2.5 py-2 text-left text-[12px] font-medium transition
                        ${active ? "bg-accent-blue/[0.14] text-accent-blue" : "text-ink-100 hover:bg-white/[0.07]"}`}
              >
                <span class="min-w-0 flex-1 truncate">{option.label}</span>
                {active && <Check class="h-3 w-3 flex-none" />}
              </button>
            );
          })}

          {voice.serverAvailable && (
            <>
              <div class="mx-2 my-1 border-t border-white/[0.07]" />
              <label class="flex cursor-pointer items-start gap-2 rounded-md px-2.5 py-2 hover:bg-white/[0.07]">
                <input
                  type="checkbox"
                  checked={voice.preferredEngine === "server"}
                  onChange={(event) =>
                    voice.setPreferredEngine(
                      (event.currentTarget as HTMLInputElement).checked ? "server" : "browser",
                    )
                  }
                  class="mt-0.5 h-3.5 w-3.5 flex-none accent-accent-blue"
                />
                <span class="min-w-0">
                  <span class="block text-[12px] text-ink-100">
                    Use server transcription (better Arabic)
                  </span>
                  <span class="block text-[11px] leading-4 text-ink-300">
                    Records the clip and transcribes it on the server after you stop.
                  </span>
                </span>
              </label>
            </>
          )}

          {!voice.browserAvailable && (
            <div class="px-2.5 pb-2 pt-1 text-[11px] leading-4 text-ink-300">
              This browser has no built-in speech recognition, so dictation is transcribed on the
              server.
            </div>
          )}

          <div class="mx-2 my-1 border-t border-white/[0.07]" />
          <MicrophoneTestSection voice={voice} disabled={voice.active} />

          {!voice.secureContext && (
            <div class="mx-1 mb-1 rounded-md border border-accent-red/30 bg-accent-red/[0.08] px-2.5 py-2 text-[11px] leading-4 text-ink-100">
              This page is not on a secure connection, so the browser will refuse the microphone.
              Open it over HTTPS.
            </div>
          )}

          {voice.diagnostics.length > 0 && (
            <details class="mx-1 mb-1 rounded-md bg-white/[0.03] px-2.5 py-1.5">
              <summary class="cursor-pointer text-[11px] font-medium text-ink-300">
                What happened last time
              </summary>
              <ul class="mt-1 space-y-0.5">
                {voice.diagnostics.map((line, index) => (
                  <li key={`${index}-${line}`} class="break-words text-[10.5px] leading-4 text-ink-400">
                    {line}
                  </li>
                ))}
              </ul>
            </details>
          )}
        </div>
      )}
    </div>
  );
}

/**
 * The microphone self-test.
 *
 * Dictation failing tells the user almost nothing, because two independent
 * things have to work: the operating system has to hand the browser audio, and
 * the browser's speech service has to turn that audio into words. Two seconds
 * of capture with a level meter isolates the first half, which is the half the
 * user can actually fix.
 */
function MicrophoneTestSection({ voice, disabled }: { voice: VoiceInput; disabled: boolean }) {
  const test = voice.microphoneTest;
  const running = test.status === "running";

  return (
    <div class="px-1 pb-1">
      <button
        type="button"
        role="menuitem"
        onClick={voice.runMicrophoneTest}
        disabled={disabled || running}
        class="flex w-full items-center gap-2 rounded-md px-1.5 py-2 text-left text-[12px] font-medium
               text-ink-100 transition hover:bg-white/[0.07] disabled:cursor-not-allowed disabled:opacity-50"
      >
        <TestTube class="h-3.5 w-3.5 flex-none text-ink-300" aria-hidden="true" />
        <span class="min-w-0 flex-1">{running ? "Testing microphone…" : "Test microphone (2s)"}</span>
        {running && <Loader class="h-3 w-3 flex-none animate-spin text-accent-blue" />}
      </button>

      {running && (
        <div class="px-1.5 pb-1" aria-hidden="true">
          <span class="block h-1.5 w-full overflow-hidden rounded-full bg-white/10">
            <span
              class="block h-full rounded-full bg-accent-green transition-[width] duration-75"
              style={{ width: `${Math.round(test.level * 100)}%` }}
            />
          </span>
        </div>
      )}

      {test.message && (
        <p
          role="status"
          class={`px-1.5 pb-1 text-[11px] leading-4 ${
            test.status === "error" ? "text-accent-red" : "text-ink-300"
          }`}
        >
          {test.message}
        </p>
      )}

      {disabled && (
        <p class="px-1.5 pb-1 text-[11px] leading-4 text-ink-400">
          Stop dictation first — opening a second microphone while the recognizer is running stops
          it.
        </p>
      )}
    </div>
  );
}

/**
 * The compact readout beside the button. The full live transcript lives in the
 * strip above the composer; this is the part that stays visible when the
 * composer is scrolled or the window is narrow.
 */
function VoiceStatusReadout({ voice }: { voice: VoiceInput }) {
  const { session } = voice;
  const transcribing = session.status === "transcribing";
  const recording = session.status === "recording";
  const label = transcribing
    ? "Transcribing…"
    : session.status === "starting"
      ? "Starting…"
      : recording
        ? `Recording ${formatElapsed(session.elapsedMs)}`
        : "Listening…";

  return (
    <div
      class="flex items-center gap-1.5 rounded-md border border-white/10 bg-white/[0.04] px-2 py-1"
      role="status"
      aria-live="polite"
    >
      {transcribing ? (
        <Loader class="h-2.5 w-2.5 flex-none animate-spin text-accent-blue" aria-hidden="true" />
      ) : (
        <span
          class="h-2 w-2 flex-none animate-pulse rounded-full bg-accent-red"
          aria-hidden="true"
        />
      )}
      <span class="whitespace-nowrap text-[11px] font-medium text-ink-100">{label}</span>
      {/*
        Only the server engine has a level to show. The browser engine used to
        open a second capture just to feed this bar, which is precisely what
        stopped Chrome's recognizer from hearing anything.
      */}
      {recording && (
        <span
          class="h-1.5 w-10 flex-none overflow-hidden rounded-full bg-white/10"
          aria-hidden="true"
        >
          <span
            class="block h-full rounded-full bg-accent-green transition-[width] duration-100"
            style={{ width: `${Math.round(session.level * 100)}%` }}
          />
        </span>
      )}
    </div>
  );
}
