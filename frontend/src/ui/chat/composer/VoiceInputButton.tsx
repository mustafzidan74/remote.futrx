import { useEffect, useRef, useState } from "preact/hooks";
import { VOICE_LANGUAGE_OPTIONS, voiceLanguageLabel } from "../../../config/voice";
import type { VoiceInput } from "../../../state/hooks/chat/useVoiceInput";
import { formatElapsed } from "../../../state/chat/voiceInputState";
import { AlertCircle, Check, ChevronDown, Loader, Mic, Square, X } from "../../primitives/icons";

/**
 * The composer's dictation control: a mic button, a small menu for language
 * and engine, and the live state readout that tells the user their voice is
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

  return (
    <div ref={rootRef} class="codex-voice-control relative flex flex-none items-end gap-1">
      <div class="flex flex-none flex-col items-stretch gap-1">
        {busy && <VoiceStatusReadout voice={voice} />}
        {failed && (
          <div
            role="alert"
            class="flex max-w-[15rem] items-start gap-1.5 rounded-md border border-accent-red/30
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
            aria-label={busy ? "Stop dictation" : "Dictate a prompt"}
            title={
              !voice.engine
                ? "Voice input is unavailable in this browser"
                : busy
                  ? "Stop dictation (Esc)"
                  : `Dictate a prompt · ${voiceLanguageLabel(voice.language)}`
            }
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
            aria-label={`Voice input options. Language: ${voiceLanguageLabel(voice.language)}`}
          >
            <ChevronDown class="h-3 w-3" />
          </button>
        </div>
      </div>

      {menuOpen && (
        <div
          class="theme-menu-surface absolute bottom-full right-0 z-40 mb-1.5 w-[min(15rem,calc(100vw-1.5rem))]
                 rounded-lg border border-white/10 bg-[#14161d] p-1 shadow-2xl"
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
        </div>
      )}
    </div>
  );
}

/**
 * The live readout. Web Speech streams words as they are spoken, so it says
 * "Listening"; the server path is silent until the upload finishes, so it
 * shows an elapsed timer instead — a recording with no feedback at all reads
 * as a hung button.
 */
function VoiceStatusReadout({ voice }: { voice: VoiceInput }) {
  const { session } = voice;
  const transcribing = session.status === "transcribing";
  const label = transcribing
    ? "Transcribing…"
    : session.status === "starting"
      ? "Starting…"
      : session.status === "recording"
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
      {!transcribing && (
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
