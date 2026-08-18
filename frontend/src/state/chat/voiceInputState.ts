import {
  VALID_VOICE_ENGINES,
  VALID_VOICE_LANGUAGES,
  VOICE_ENGINE_STORAGE_KEY,
  VOICE_LANGUAGE_STORAGE_KEY,
  currentBrowserLocale,
  defaultVoiceLanguage,
  type VoiceEngine,
  type VoiceLanguage,
} from "../../config/voice.ts";

/**
 * The dictation state machine, kept free of the browser APIs that drive it so
 * the interim/final merge can be pinned by tests.
 *
 * Two recognizers feed the same machine. The Web Speech API streams a growing
 * hypothesis that is rewritten until it firms up, so the composer has to show
 * an *interim* span it can replace wholesale. The server fallback produces one
 * final transcript at the end, which is the same machine with the interim step
 * skipped.
 *
 * The invariant that matters: everything the user had typed before they
 * pressed the mic survives untouched. The session remembers the text on both
 * sides of the caret and only ever rewrites the span between them.
 */
export type VoiceStatus =
  /** Not recording. */
  | "idle"
  /** Permission requested, recognizer not yet running. */
  | "starting"
  /** Web Speech is streaming results. */
  | "listening"
  /** MediaRecorder is capturing for the server fallback. */
  | "recording"
  /** The clip has been uploaded and the provider has not answered yet. */
  | "transcribing"
  /** The session stopped on a failure the user needs to read. */
  | "error";

export interface VoiceSession {
  status: VoiceStatus;
  /** The composer text on either side of the caret when the session began. */
  before: string;
  after: string;
  /** Speech that has firmed up during this session. */
  final: string;
  /** The hypothesis still being rewritten. Always empty once the session ends. */
  interim: string;
  /** Microphone level, 0–1, for the meter. 0 when unavailable. */
  level: number;
  /** How long the current recording has been running, in milliseconds. */
  elapsedMs: number;
  error: string;
  /**
   * Something the recognizer reported that the session survived — a silence
   * timeout, an interruption it restarted from. It is shown beside the live
   * transcript rather than as a banner, because the session is still running
   * and a red box every few seconds of quiet would be worse than the silence
   * it is reporting.
   */
  notice: string;
}

export const IDLE_VOICE_SESSION: VoiceSession = {
  status: "idle",
  before: "",
  after: "",
  final: "",
  interim: "",
  level: 0,
  elapsedMs: 0,
  error: "",
  notice: "",
};

/**
 * Opens a session at the caret. A caret outside the text is clamped, so a
 * stale or out-of-range offset splits at an end rather than throwing away
 * part of the draft.
 */
export function beginSession(
  text: string,
  caret: number,
  status: Extract<VoiceStatus, "starting" | "listening" | "recording"> = "starting",
): VoiceSession {
  const split = Math.max(0, Math.min(caret, text.length));
  return {
    ...IDLE_VOICE_SESSION,
    status,
    before: text.slice(0, split),
    after: text.slice(split),
  };
}

/** Moves a started session into its running state. */
export function markRunning(
  session: VoiceSession,
  status: Extract<VoiceStatus, "listening" | "recording">,
): VoiceSession {
  return session.status === "idle" || session.status === "error"
    ? session
    : { ...session, status, error: "" };
}

/**
 * Folds one Web Speech `result` event into the session. `finalChunk` is only
 * the text that firmed up in *this* event — the caller slices from
 * `event.resultIndex` — while `interim` replaces the previous hypothesis
 * wholesale, which is exactly how the API rewrites it.
 */
export function applyRecognition(
  session: VoiceSession,
  { finalChunk = "", interim = "" }: { finalChunk?: string; interim?: string },
): VoiceSession {
  if (session.status === "idle") return session;
  return {
    ...session,
    final: joinSpeech(session.final, finalChunk),
    interim: interim.trim(),
    // Words arriving means whatever was reported has passed.
    notice: "",
  };
}

/**
 * Records a non-fatal report from the recognizer without ending the session.
 * `no-speech` is the one that matters: the microphone was open and only
 * silence arrived, which is the difference between "this is broken" and "it
 * cannot hear you".
 */
export function applyNotice(session: VoiceSession, notice: string): VoiceSession {
  if (session.status === "idle" || session.status === "error") return session;
  return { ...session, notice: notice.trim() };
}

/**
 * Folds a completed server transcript in. It arrives whole, so it lands as
 * final text and clears any interim span left over from a browser session.
 */
export function applyTranscript(session: VoiceSession, transcript: string): VoiceSession {
  // Same guard the streaming transitions carry: a transcript that arrives
  // after the user ended the session must not revive it and overwrite what
  // they have typed since.
  if (session.status === "idle") return session;
  return {
    ...session,
    status: "idle",
    final: joinSpeech(session.final, transcript),
    interim: "",
    level: 0,
  };
}

/** Reports the microphone level for the meter. */
export function withLevel(session: VoiceSession, level: number): VoiceSession {
  if (session.status === "idle") return session;
  const clamped = clampLevel(level);
  return { ...session, level: clamped };
}

/** Reports how long the current recording has run. */
export function withElapsed(session: VoiceSession, elapsedMs: number): VoiceSession {
  if (session.status === "idle") return session;
  return { ...session, elapsedMs: Math.max(0, elapsedMs) };
}

/** The clip is uploaded; the provider has not answered yet. */
export function markTranscribing(session: VoiceSession): VoiceSession {
  if (session.status === "idle") return session;
  return { ...session, status: "transcribing", interim: "", level: 0 };
}

/**
 * Ends a session cleanly. A hypothesis that never firmed up is kept rather
 * than discarded: the user watched those words appear and would read their
 * disappearance as lost dictation, and the composer is a draft they review
 * before sending anyway.
 */
export function finishSession(session: VoiceSession): VoiceSession {
  return {
    ...session,
    status: "idle",
    final: joinSpeech(session.final, session.interim),
    interim: "",
    level: 0,
    notice: "",
  };
}

/**
 * Ends a session on a failure. Whatever was already dictated is kept — a
 * dropped connection halfway through should not also erase the first half.
 */
export function failSession(session: VoiceSession, error: string): VoiceSession {
  return {
    ...finishSession(session),
    status: "error",
    error: error.trim() || "Voice input failed.",
  };
}

/** Clears an error banner without touching the composer text. */
export function dismissError(session: VoiceSession): VoiceSession {
  return session.status === "error" ? { ...IDLE_VOICE_SESSION } : session;
}

export interface ComposedText {
  text: string;
  /** Logical caret offset: the end of the dictated span. */
  caret: number;
}

/**
 * Renders the composer value for a session, and where the caret belongs.
 *
 * The caret is a logical offset into the string, so it is direction-agnostic:
 * a textarea with `dir="auto"` rendering Arabic right-to-left still places the
 * caret after the same character. That is why the caret is tracked as an
 * offset rather than as anything visual.
 */
export function composeText(session: VoiceSession): ComposedText {
  const spoken = joinSpeech(session.final, session.interim);
  const before = spoken && needsSeparator(session.before) ? `${session.before} ` : session.before;
  const after =
    spoken && needsSeparator(leadingCharacter(session.after)) ? ` ${session.after}` : session.after;
  return { text: `${before}${spoken}${after}`, caret: before.length + spoken.length };
}

/** Whether the mic button should render at all in its current surroundings. */
export function voiceInputAvailable(
  speechRecognitionSupported: boolean,
  serverTranscriptionEnabled: boolean,
): boolean {
  return speechRecognitionSupported || serverTranscriptionEnabled;
}

/**
 * Which engine a session should actually use. A user who asked for the server
 * but lost it (an admin turned it off) falls back to the browser rather than
 * to a dead button, and Firefox gets the server whatever the stored preference
 * says because it has no Web Speech API at all.
 */
export function resolveEngine(
  preferred: VoiceEngine,
  speechRecognitionSupported: boolean,
  serverTranscriptionEnabled: boolean,
): VoiceEngine | null {
  if (preferred === "server" && serverTranscriptionEnabled) return "server";
  if (speechRecognitionSupported) return "browser";
  if (serverTranscriptionEnabled) return "server";
  return null;
}

/** mm:ss for the recording timer. */
export function formatElapsed(elapsedMs: number): string {
  const total = Math.max(0, Math.floor(elapsedMs / 1000));
  const minutes = Math.floor(total / 60);
  const seconds = total % 60;
  return `${minutes}:${String(seconds).padStart(2, "0")}`;
}

/**
 * Joins two runs of speech with exactly one space, unless the second opens
 * with punctuation that should hug the word before it. Arabic and English
 * both read correctly under this rule; Arabic needs no special case because
 * its words are space-separated like Latin ones.
 */
export function joinSpeech(left: string, right: string): string {
  const head = left.replace(/\s+$/, "");
  const tail = right.replace(/^\s+/, "").replace(/\s+$/, "");
  if (!head) return tail;
  if (!tail) return head;
  return CLINGING_PUNCTUATION.test(tail) ? `${head}${tail}` : `${head} ${tail}`;
}

/** Punctuation that belongs against the previous word, Arabic marks included. */
const CLINGING_PUNCTUATION = /^[.,!?;:%)\]}»…،؛؟]/;

function needsSeparator(edge: string): boolean {
  return edge.length > 0 && !/\s$/.test(edge);
}

/** The edge of the trailing text that touches the dictated span. */
function leadingCharacter(after: string): string {
  return after.slice(0, 1);
}

type StorageLike = Pick<Storage, "getItem" | "setItem">;

/**
 * Per-device dictation preferences. They live in localStorage rather than in
 * the account settings on purpose: which microphone language you speak is a
 * property of where you are sitting, not of who you are, and the same account
 * on a shared machine should not drag the choice around.
 */
export class VoicePreferenceStore {
  private readonly storage: StorageLike | null;
  private readonly browserLocale: string | undefined;

  constructor(
    storage: StorageLike | null = defaultStorage(),
    browserLocale: string | undefined = currentBrowserLocale(),
  ) {
    this.storage = storage;
    this.browserLocale = browserLocale;
  }

  language(): VoiceLanguage {
    const stored = this.read(VOICE_LANGUAGE_STORAGE_KEY);
    return stored && VALID_VOICE_LANGUAGES.has(stored)
      ? (stored as VoiceLanguage)
      : defaultVoiceLanguage(this.browserLocale);
  }

  setLanguage(language: VoiceLanguage): void {
    this.write(VOICE_LANGUAGE_STORAGE_KEY, language);
  }

  engine(): VoiceEngine {
    const stored = this.read(VOICE_ENGINE_STORAGE_KEY);
    return stored && VALID_VOICE_ENGINES.has(stored) ? (stored as VoiceEngine) : "browser";
  }

  setEngine(engine: VoiceEngine): void {
    this.write(VOICE_ENGINE_STORAGE_KEY, engine);
  }

  private read(key: string): string | null {
    if (!this.storage) return null;
    try {
      return this.storage.getItem(key);
    } catch {
      return null;
    }
  }

  private write(key: string, value: string): void {
    if (!this.storage) return;
    try {
      this.storage.setItem(key, value);
    } catch {
      // Quota or privacy-mode failures degrade to a per-session choice.
    }
  }
}

function defaultStorage(): StorageLike | null {
  try {
    return typeof window === "undefined" ? null : window.localStorage;
  } catch {
    return null;
  }
}

export const voicePreferenceStore = new VoicePreferenceStore();

/**
 * How many composers currently have a live microphone.
 *
 * Escape means two things in a chat: stop dictating, and cancel the running
 * agent turn. Both are window listeners owned by hooks that cannot see each
 * other, and betting on listener order is how you get a bug that depends on
 * mount sequence. Rather than have dictation swallow the key — which would
 * also rob every modal in the app of its Escape — the cancel shortcut asks
 * this counter and stands down while a microphone is live. One Escape stops
 * the microphone; the next one cancels the run.
 *
 * It is a counter rather than a boolean because nothing guarantees exactly one
 * composer is mounted.
 */
let liveDictations = 0;

/** Registers a live microphone; the returned function retires it. */
export function beginDictationClaim(): () => void {
  liveDictations += 1;
  let released = false;
  return () => {
    if (released) return;
    released = true;
    liveDictations = Math.max(0, liveDictations - 1);
  };
}

/** Whether any composer is dictating right now. */
export function isDictating(): boolean {
  return liveDictations > 0;
}

/* ------------------------------------------------------------------------ *
 * Diagnostics
 *
 * Dictation fails in ways the user cannot see: a denied permission, a muted
 * tab, a speech service the browser cannot reach, a device another application
 * is holding. Every one of those used to end the session the same way a
 * successful one does — silently, with an unchanged composer. The vocabulary
 * below is what turns each of them into a sentence the operator can act on,
 * and it lives here rather than in the hook so the mapping is pinned by tests.
 * ------------------------------------------------------------------------ */

/**
 * Chrome ends continuous recognition on its own after roughly a minute of
 * silence. Restarting keeps a dictation alive across pauses; the budget stops
 * a recognizer that ends instantly from spinning forever.
 */
export const RECOGNITION_RESTART_LIMIT = 20;

/**
 * How many runs that ended at once, having heard nothing, are tolerated before
 * the user is told the microphone is not delivering audio. One retry, then an
 * explanation.
 */
export const RECOGNITION_DEAD_START_LIMIT = 2;

/** A run shorter than this that produced no words never really started. */
export const IMMEDIATE_END_MS = 900;

export const INSECURE_CONTEXT_MESSAGE =
  "Microphones need a secure connection. Open this page over HTTPS (or on localhost) and try again.";

export const NO_AUDIO_MESSAGE =
  "The browser ended the microphone straight away without hearing anything. Check that the tab is not muted, that no other app is holding the microphone, and that the system privacy settings let the browser use it. Run the microphone test in this menu to confirm.";

export const ALREADY_STARTED_MESSAGE =
  "Speech recognition was still running from a previous attempt and would not restart. Reload the tab and try again.";

export const NO_RECOGNITION_MESSAGE = "This browser has no speech recognition.";

/**
 * The user-facing reason for one Web Speech `error` code.
 *
 * Every code the API documents is named, including the two the previous
 * implementation swallowed. "no-speech" after a user has spoken is the single
 * most informative signal there is — it means the audio reached the recognizer
 * as silence — and hiding it is what made this feature undiagnosable.
 */
export function recognitionErrorMessage(code: string): string {
  switch (code) {
    case "not-allowed":
      return "Microphone access is blocked. Open the padlock menu in the address bar, allow the microphone for this site, then reload.";
    case "service-not-allowed":
      return "The browser refused to use its speech service for this page. Check the site's microphone permission and any managed-browser policy.";
    case "audio-capture":
      return "No working microphone was found. Choose an input device in the browser's site settings and try again.";
    case "network":
      return "Speech recognition could not reach the browser vendor's speech service. Chrome uploads the audio to Google to transcribe it, so a blocked or offline network stops dictation — switch to server transcription if that traffic is not allowed here.";
    case "language-not-supported":
      return "The browser's speech service does not recognise the selected language. Choose another one from this menu.";
    case "no-speech":
      return "No speech was detected. The microphone was open but only silence arrived — check that the right input device is selected and that it is not muted.";
    case "aborted":
      return "Dictation was interrupted before it finished.";
    case "bad-grammar":
      return "The speech service rejected the recognition request.";
    case "":
      return "Voice input failed.";
    default:
      return `Voice input failed (${code}).`;
  }
}

/**
 * Whether an error code ends the session for good. The rest are the ordinary
 * ways a continuous recognizer stops between phrases, and are restarted.
 */
export function isFatalRecognitionError(code: string): boolean {
  switch (code) {
    case "not-allowed":
    case "service-not-allowed":
    case "audio-capture":
    case "language-not-supported":
    case "network":
    case "bad-grammar":
      return true;
    default:
      return false;
  }
}

/** The reason a `getUserMedia` call failed, in the user's terms. */
export function microphoneErrorMessage(cause: unknown): string {
  const name = (cause as { name?: string } | null)?.name ?? "";
  switch (name) {
    case "NotAllowedError":
    case "SecurityError":
      return "Microphone access is blocked. Open the padlock menu in the address bar, allow the microphone for this site, then reload.";
    case "NotFoundError":
    case "DevicesNotFoundError":
      return "No microphone was found on this machine.";
    case "NotReadableError":
    case "TrackStartError":
      return "The microphone is held by another application, or the system refused to open it.";
    case "OverconstrainedError":
      return "No microphone matched the requested settings.";
    default:
      return "The microphone could not be opened.";
  }
}

/** Chrome throws this when a recognizer that is already running is started again. */
export function isAlreadyStartedError(cause: unknown): boolean {
  const error = cause as { name?: string; message?: string } | null;
  return error?.name === "InvalidStateError" || /already started/i.test(error?.message ?? "");
}

export interface RecognitionEndInput {
  /** The user asked to stop. Nothing restarts after that. */
  stopRequested: boolean;
  /** The last error code this run reported, or "" when it reported none. */
  errorCode: string;
  /** Restarts already spent this session. */
  restarts: number;
  /** Restarts this session is allowed. */
  maxRestarts: number;
  /** Consecutive runs that ended at once having heard nothing. */
  deadStarts: number;
  /** How long this run lasted, in milliseconds. */
  ranForMs: number;
  /** Whether this run produced any words. */
  heard: boolean;
}

export type RecognitionEndPlan =
  | { action: "restart"; deadStart: boolean }
  | { action: "finish" }
  | { action: "fail"; message: string };

/**
 * What to do when the recognizer ends by itself.
 *
 * Chrome stops continuous recognition after roughly a minute of silence and
 * fires `end` exactly as it does for a successful stop, so a hook that reads
 * every `end` as "the user is done" drops the microphone mid-thought. It also
 * fires `end` immediately when the tab has no usable audio input at all, which
 * looks identical from the outside — hence the distinction between a pause
 * worth restarting and a run that never carried audio.
 */
export function planAfterRecognitionEnd(input: RecognitionEndInput): RecognitionEndPlan {
  if (input.stopRequested) return { action: "finish" };
  if (isFatalRecognitionError(input.errorCode)) {
    return { action: "fail", message: recognitionErrorMessage(input.errorCode) };
  }
  const deadStart = !input.heard && input.ranForMs < IMMEDIATE_END_MS;
  if (deadStart && input.deadStarts + 1 >= RECOGNITION_DEAD_START_LIMIT) {
    return { action: "fail", message: NO_AUDIO_MESSAGE };
  }
  if (input.restarts >= input.maxRestarts) return { action: "finish" };
  return { action: "restart", deadStart };
}

/* ------------------------------------------------------------------------ *
 * Microphone test
 *
 * "Nothing happened" has two very different causes — the microphone never
 * delivered audio, or it did and recognition failed — and the user cannot tell
 * them apart from the composer. A two-second capture with a level readout
 * separates them without involving any speech service at all.
 * ------------------------------------------------------------------------ */

export const MIC_TEST_DURATION_MS = 2000;

export interface MicrophoneTest {
  status: "idle" | "running" | "done" | "error";
  /** Live level while running, 0–1. */
  level: number;
  /** Loudest level observed over the whole capture, 0–1. */
  peak: number;
  message: string;
}

export const IDLE_MICROPHONE_TEST: MicrophoneTest = {
  status: "idle",
  level: 0,
  peak: 0,
  message: "",
};

/** The verdict for a finished capture, from the loudest level it saw. */
export function microphoneTestVerdict(peak: number): string {
  const percent = Math.round(clampLevel(peak) * 100);
  if (percent >= 30) return `Microphone works — peak level ${percent}%.`;
  if (percent >= 5) {
    return `Very quiet — peak level ${percent}%. Move closer, or raise the input level in the system sound settings.`;
  }
  return `No sound reached the browser — peak level ${percent}%. Check that the right input device is selected and that it is not muted.`;
}

/** Levels arrive from an analyser, so they are clamped before anything reads them. */
export function clampLevel(level: number): number {
  return Number.isFinite(level) ? Math.max(0, Math.min(1, level)) : 0;
}
