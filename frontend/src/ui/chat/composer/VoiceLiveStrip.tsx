import type { VoiceInput } from "../../../state/hooks/chat/useVoiceInput";
import { formatElapsed } from "../../../state/chat/voiceInputState";
import { Loader, Mic } from "../../primitives/icons";

/**
 * The live dictation strip, shown above the composer while a microphone is
 * open.
 *
 * The transcript also lands in the textarea, so this is deliberately
 * redundant — and that redundancy is the point. When dictation produced
 * nothing, the user could not tell whether the microphone was dead, the
 * recognizer never started, or the text simply failed to reach the composer.
 * A strip that is driven by the session rather than by the draft answers that:
 * words here but not in the textarea is a wiring fault, an empty strip with a
 * running timer is a silent microphone.
 */
export function VoiceLiveStrip({ voice }: { voice: VoiceInput }) {
  const { session } = voice;
  if (!voice.active) return null;

  const transcribing = session.status === "transcribing";
  const recording = session.status === "recording";
  const starting = session.status === "starting";
  const heading = transcribing
    ? "Transcribing…"
    : starting
      ? "Starting…"
      : recording
        ? `Recording ${formatElapsed(session.elapsedMs)}`
        : "Listening…";

  return (
    <div
      class="mx-3 mt-2 flex items-start gap-2 rounded-md border border-accent-red/25 bg-accent-red/[0.07] px-2.5 py-2"
      role="status"
      aria-live="polite"
    >
      {transcribing ? (
        <Loader class="mt-0.5 h-3.5 w-3.5 flex-none animate-spin text-accent-blue" aria-hidden="true" />
      ) : (
        <Mic class="mt-0.5 h-3.5 w-3.5 flex-none animate-pulse text-accent-red" aria-hidden="true" />
      )}
      <div class="min-w-0 flex-1">
        <div class="flex flex-wrap items-center gap-x-2 gap-y-0.5 text-[11px] font-semibold text-ink-100">
          <span>{heading}</span>
          <span class="font-normal text-ink-300" dir="auto">
            {voice.languageLabel}
          </span>
          {voice.restarts > 0 && (
            <span class="font-normal text-ink-400">restarted {voice.restarts}×</span>
          )}
        </div>
        {/*
          The hypothesis is rewritten several times a second. Announcing every
          revision would make the composer unusable with a screen reader, so
          only the status line above is live.
        */}
        <p
          class="mt-0.5 break-words text-[12px] leading-4 text-ink-200"
          dir="auto"
          aria-live="off"
        >
          {liveText(voice)}
        </p>
        {session.notice && (
          <p class="mt-1 break-words text-[11px] leading-4 text-accent-yellow">{session.notice}</p>
        )}
      </div>
      {(recording || session.level > 0) && (
        <span
          class="mt-1 h-1.5 w-12 flex-none overflow-hidden rounded-full bg-white/10"
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

/**
 * What the strip shows. The interim hypothesis if there is one, the last words
 * that firmed up if there is not, and an instruction when neither has arrived
 * yet — never a blank line, which reads as a hang.
 */
function liveText(voice: VoiceInput): string {
  const { session } = voice;
  if (session.status === "transcribing") return "Uploading the clip and waiting for the transcript.";
  if (session.status === "recording") return "Recording. Press the square to stop and transcribe.";
  if (session.interim) return session.interim;
  if (session.final) return tail(session.final);
  return "Speak now — words appear here and in the composer as they are recognised.";
}

/** The end of the settled text, so a long dictation does not grow the strip. */
function tail(final: string): string {
  const limit = 140;
  return final.length <= limit ? final : `…${final.slice(final.length - limit)}`;
}
