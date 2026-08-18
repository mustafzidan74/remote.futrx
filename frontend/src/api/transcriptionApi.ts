import { requestJson } from "./apiRequest";
import { API_ROUTES } from "../config/routes";
import type {
  TranscriptionClientConfig,
  TranscriptionResult,
  TranscriptionSettings,
  TranscriptionTestResult,
  UpdateTranscriptionSettingsInput,
} from "../models/transcription";

export interface TranscribeRequest {
  audio: Blob;
  /** BCP-47 tag the user picked in the composer, or "auto". */
  language: string;
  durationMs: number;
  chatId?: string;
}

export const transcriptionApi = {
  /** What the composer may know: is the server fallback available, and how long may a clip be. */
  clientConfig: () =>
    requestJson<TranscriptionClientConfig>("GET", API_ROUTES.transcription.clientConfig),

  /**
   * Sends one recorded clip. The blob is posted as multipart so the backend
   * can stream it straight through to the provider rather than buffering a
   * base64 copy of it.
   *
   * Field order is load bearing: the backend walks the parts in order so the
   * audio never has to be spooled to its disk, which means the text hints have
   * to be appended *before* the recording. FormData preserves insertion order.
   */
  transcribe: ({ audio, language, durationMs, chatId }: TranscribeRequest) => {
    const form = new FormData();
    form.append("language", language);
    form.append("durationMs", String(Math.max(0, Math.round(durationMs))));
    if (chatId) form.append("chatId", chatId);
    form.append("audio", audio, audioFilename(audio.type));
    return requestJson<TranscriptionResult>("POST", API_ROUTES.transcription.transcribe, form);
  },

  settings: () => requestJson<TranscriptionSettings>("GET", API_ROUTES.transcription.settings),
  save: (input: UpdateTranscriptionSettingsInput) =>
    requestJson<TranscriptionSettings>("PUT", API_ROUTES.transcription.settings, input),
  test: () => requestJson<TranscriptionTestResult>("POST", API_ROUTES.transcription.test),
};

/** The provider dispatches on the extension, so the container type has to reach it. */
function audioFilename(mimeType: string): string {
  if (mimeType.includes("ogg")) return "dictation.ogg";
  if (mimeType.includes("mp4")) return "dictation.mp4";
  if (mimeType.includes("wav")) return "dictation.wav";
  return "dictation.webm";
}
