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
};

/**
 * Opens a session at the caret. `caret` outside the text clamps to its ends,
 * which is what a textarea that has never been focused reports.
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
  };
}

/**
 * Folds a completed server transcript in. It arrives whole, so it lands as
 * final text and clears any interim span left over from a browser session.
 */
export function applyTranscript(session: VoiceSession, transcript: string): VoiceSession {
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
  const clamped = Number.isFinite(level) ? Math.max(0, Math.min(1, level)) : 0;
  return { ...session, level: clamped };
}

/** Reports how long the current recording has run. */
export function withElapsed(session: VoiceSession, elapsedMs: number): VoiceSession {
  if (session.status === "idle") return session;
  return { ...session, elapsedMs: Math.max(0, elapsedMs) };
}

/** The clip is uploaded; the provider has not answered yet. */
export function markTranscribing(session: VoiceSession): VoiceSession {
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
