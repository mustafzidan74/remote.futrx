import type {
  SpeechRecognitionErrorEvent,
  SpeechRecognitionEvent,
  SpeechRecognitionLike,
} from "../../types/speech.ts";
import {
  ALREADY_STARTED_MESSAGE,
  IDLE_VOICE_SESSION,
  INSECURE_CONTEXT_MESSAGE,
  NO_RECOGNITION_MESSAGE,
  RECOGNITION_RESTART_LIMIT,
  applyNotice,
  applyRecognition,
  beginSession,
  composeText,
  dismissError,
  failSession,
  finishSession,
  isAlreadyStartedError,
  markRunning,
  planAfterRecognitionEnd,
  recognitionErrorMessage,
  type VoiceSession,
} from "./voiceInputState.ts";

/**
 * The Web Speech half of dictation, with every browser object injected.
 *
 * This used to live inside the hook, tangled with React refs and a generation
 * counter, and it was untestable in a repository whose frontend tests run
 * under plain `node --test` with no DOM. That is why the defect that broke it
 * shipped: nothing could exercise "Chrome delivered interim results and then
 * ended the session by itself" without a browser.
 *
 * Everything here is therefore driven through three seams — a recognizer
 * factory, a clock plus timer pair, and two output callbacks — so the whole
 * lifecycle can be played out in a unit test.
 *
 * The rules the controller exists to enforce:
 *
 *  - **The composer is written on every transition that changes the text.**
 *    A session that updates its own state without writing the draft is exactly
 *    the failure the user reported: they speak, and nothing appears.
 *  - **Stopping flushes.** `stop()` asks the recognizer to finish and keeps
 *    accepting results until it does, because `abort()` throws away everything
 *    the recognizer has not yet dispatched. A watchdog ends the session anyway
 *    if `end` never arrives.
 *  - **The recognizer ending is not the user being done.** Chrome ends
 *    continuous recognition after about a minute of silence; the session
 *    restarts underneath the user until they press stop.
 *  - **Every failure produces a sentence.** No error code is swallowed.
 */

/** The clock and timers, injected so tests do not wait in real time. */
export interface DictationTimers {
  now(): number;
  setTimeout(handler: () => void, ms: number): number;
  clearTimeout(id: number): void;
}

export const systemTimers: DictationTimers = {
  now: () => Date.now(),
  setTimeout: (handler, ms) => setTimeout(handler, ms) as unknown as number,
  clearTimeout: (id) => clearTimeout(id),
};

export interface DictationHost {
  /** A fresh recognizer, or null when this browser has no Web Speech API. */
  create: () => SpeechRecognitionLike | null;
  /** False on plain HTTP, where browsers refuse the microphone outright. */
  secureContext: boolean;
  /** The BCP-47 tag to recognise; "" leaves the browser's own default. */
  languageTag: () => string;
  /** Mirrors every session change out (into React state, in the app). */
  emit: (session: VoiceSession) => void;
  /** Writes the composed draft into the composer, with the caret offset. */
  write: (text: string, caret: number) => void;
  /** One line per lifecycle event, shown in the mic menu's diagnostics. */
  trace?: (line: string) => void;
  timers?: DictationTimers;
  /** Restarts allowed across Chrome's own session cut-offs. */
  maxRestarts?: number;
  /** How long to keep accepting results after `stop()` before forcing the end. */
  flushMs?: number;
}

/** How long a stop waits for the recognizer's final flush before giving up. */
export const DEFAULT_FLUSH_MS = 1500;

/** How long to wait before retrying a recognizer that reported it was already running. */
export const ALREADY_STARTED_RETRY_MS = 250;

export class BrowserDictation {
  private readonly host: DictationHost;
  private readonly timers: DictationTimers;
  private readonly maxRestarts: number;
  private readonly flushMs: number;

  private session: VoiceSession = IDLE_VOICE_SESSION;
  /** The recognizer whose events count. Identity *is* the ownership token. */
  private recognition: SpeechRecognitionLike | null = null;
  private stopRequested = false;
  private restarts = 0;
  private deadStarts = 0;
  private runStartedAt = 0;
  private heardThisRun = false;
  private lastErrorCode = "";
  private flushTimer: number | null = null;
  private retryTimer: number | null = null;
  private retriedAlreadyStarted = false;

  constructor(host: DictationHost) {
    this.host = host;
    this.timers = host.timers ?? systemTimers;
    this.maxRestarts = host.maxRestarts ?? RECOGNITION_RESTART_LIMIT;
    this.flushMs = host.flushMs ?? DEFAULT_FLUSH_MS;
  }

  get current(): VoiceSession {
    return this.session;
  }

  /** How many times Chrome ended this session and it was restarted. */
  get restartCount(): number {
    return this.restarts;
  }

  get active(): boolean {
    return this.session.status !== "idle" && this.session.status !== "error";
  }

  /** Opens a session at `caret` in `text` and starts the recognizer. */
  start(text: string, caret: number): void {
    if (this.active) return;
    this.stopRequested = false;
    this.restarts = 0;
    this.deadStarts = 0;
    this.retriedAlreadyStarted = false;
    this.lastErrorCode = "";
    this.apply(() => beginSession(text, caret), { write: false });

    if (!this.host.secureContext) {
      this.trace("insecure context");
      this.fail(INSECURE_CONTEXT_MESSAGE);
      return;
    }
    this.launch();
  }

  /**
   * Ends the session the way the user asked: the recognizer is told to
   * `stop()`, not `abort()`, so it dispatches whatever it has already
   * recognised before it ends. Results keep being folded in until then.
   */
  stop(): void {
    if (!this.active) return;
    this.stopRequested = true;
    const instance = this.recognition;
    if (!instance) {
      this.finish();
      return;
    }
    this.trace("stop requested");
    try {
      instance.stop();
    } catch {
      this.forceFinish();
      return;
    }
    // Chrome normally flushes and fires `end` within a few hundred
    // milliseconds. When it does not — a muted tab, a device that vanished —
    // nothing else would ever end the session, so the watchdog does.
    this.clearFlushTimer();
    this.flushTimer = this.timers.setTimeout(() => {
      this.flushTimer = null;
      this.trace("flush timed out");
      this.forceFinish();
    }, this.flushMs);
  }

  /**
   * Drops the session without writing anything: unmount, or a switch to
   * another chat. Late events from the abandoned recognizer are ignored
   * because it is no longer the owner.
   */
  abandon(): void {
    this.clearFlushTimer();
    const instance = this.detach();
    abortQuietly(instance);
    this.session = IDLE_VOICE_SESSION;
  }

  /** Clears an error banner without touching the composer text. */
  dismiss(): void {
    this.apply(dismissError, { write: false });
  }

  /** Replaces the session wholesale; the hook uses it to seed a restart. */
  private launch(): void {
    const instance = this.host.create();
    if (!instance) {
      this.fail(NO_RECOGNITION_MESSAGE);
      return;
    }
    instance.continuous = true;
    instance.interimResults = true;
    instance.maxAlternatives = 1;
    const tag = this.host.languageTag();
    if (tag) instance.lang = tag;

    instance.onstart = () => {
      if (this.recognition !== instance) return;
      // Re-stamped here so "how long did this run last" measures from the
      // moment audio actually opened. `heardThisRun` is deliberately not
      // reset: a result can beat `start` on some builds, and that is speech.
      this.runStartedAt = this.timers.now();
      this.trace(`listening (${tag || "browser default"})`);
      // Results can beat `start` on some builds, so this transition never
      // clobbers text — it only moves the status forward.
      this.apply((current) => markRunning(current, "listening"), { write: false });
    };

    instance.onresult = (event: SpeechRecognitionEvent) => {
      if (this.recognition !== instance) return;
      const { finalChunk, interim } = readResults(event);
      if (!finalChunk && !interim) return;
      this.heardThisRun = true;
      // markRunning first: a result that arrives while the session is still
      // "starting" is real speech and must reach the composer, which the
      // previous implementation dropped.
      this.apply((current) =>
        applyRecognition(markRunning(current, "listening"), { finalChunk, interim }),
      );
    };

    instance.onerror = (event: SpeechRecognitionErrorEvent) => {
      if (this.recognition !== instance) return;
      this.lastErrorCode = event.error;
      this.trace(`error: ${event.error}`);
      // A user-initiated stop reports itself as "aborted" or "no-speech";
      // that is the session ending, not a failure. Everything else is decided
      // in `end`, which the API always fires afterwards — except for the fatal
      // codes, which are ended here so a browser that skips `end` still
      // reports them.
      if (this.stopRequested) return;
      const plan = planAfterRecognitionEnd({
        stopRequested: false,
        errorCode: event.error,
        restarts: this.restarts,
        maxRestarts: this.maxRestarts,
        deadStarts: this.deadStarts,
        ranForMs: this.timers.now() - this.runStartedAt,
        heard: this.heardThisRun,
      });
      if (plan.action !== "fail") {
        // The session survives, so the reason is shown beside the live
        // transcript instead of as a banner. Swallowing it entirely is what
        // made a microphone that heard nothing indistinguishable from one
        // that was never opened.
        this.apply((current) => applyNotice(current, recognitionErrorMessage(event.error)), {
          write: false,
        });
        return;
      }
      this.detach();
      abortQuietly(instance);
      this.fail(plan.message);
    };

    instance.onend = () => {
      if (this.recognition !== instance) return;
      this.recognition = null;
      this.clearFlushTimer();
      const errorCode = this.lastErrorCode;
      this.lastErrorCode = "";
      const plan = planAfterRecognitionEnd({
        stopRequested: this.stopRequested,
        errorCode,
        restarts: this.restarts,
        maxRestarts: this.maxRestarts,
        deadStarts: this.deadStarts,
        ranForMs: this.timers.now() - this.runStartedAt,
        heard: this.heardThisRun,
      });
      this.trace(`ended → ${plan.action}`);
      if (plan.action === "fail") {
        this.fail(plan.message);
        return;
      }
      if (plan.action === "finish") {
        this.finish();
        return;
      }
      this.restarts += 1;
      this.deadStarts = plan.deadStart ? this.deadStarts + 1 : 0;
      this.launch();
    };

    // Stamped before `start` as well, so a recognizer that ends without ever
    // firing `start` is still measured against this launch rather than the
    // previous one — otherwise a dead input reads as a long, healthy run.
    this.runStartedAt = this.timers.now();
    this.heardThisRun = false;
    this.recognition = instance;
    try {
      instance.start();
    } catch (cause) {
      this.recognition = null;
      abortQuietly(instance);
      // Chrome throws InvalidStateError when a recognizer is still running.
      // One retry after the abort clears almost every case; a second failure
      // needs the tab reloaded and says so.
      if (isAlreadyStartedError(cause) && !this.retriedAlreadyStarted) {
        this.retriedAlreadyStarted = true;
        this.trace("already started — retrying once");
        this.retryTimer = this.timers.setTimeout(() => {
          this.retryTimer = null;
          if (this.active) this.launch();
        }, ALREADY_STARTED_RETRY_MS);
        return;
      }
      this.trace("start threw");
      this.fail(isAlreadyStartedError(cause) ? ALREADY_STARTED_MESSAGE : startFailureMessage(cause));
    }
  }

  /** Ends cleanly, folding any unfirmed hypothesis into the composer. */
  private finish(): void {
    this.clearFlushTimer();
    const instance = this.detach();
    abortQuietly(instance);
    this.apply(finishSession);
  }

  /** The watchdog path: the recognizer never flushed, so end it ourselves. */
  private forceFinish(): void {
    this.finish();
  }

  private fail(message: string): void {
    this.clearFlushTimer();
    const instance = this.detach();
    abortQuietly(instance);
    this.apply((current) => failSession(current, message));
  }

  private detach(): SpeechRecognitionLike | null {
    const instance = this.recognition;
    this.recognition = null;
    return instance;
  }

  private clearFlushTimer(): void {
    if (this.flushTimer !== null) {
      this.timers.clearTimeout(this.flushTimer);
      this.flushTimer = null;
    }
    if (this.retryTimer !== null) {
      this.timers.clearTimeout(this.retryTimer);
      this.retryTimer = null;
    }
  }

  private trace(line: string): void {
    this.host.trace?.(line);
  }

  /**
   * The single place the composer is written. Every transition goes through
   * here, which is what makes "the hook updated its own state but never told
   * the composer" impossible to reintroduce.
   */
  private apply(next: (current: VoiceSession) => VoiceSession, { write = true } = {}): void {
    const updated = next(this.session);
    this.session = updated;
    this.host.emit(updated);
    if (!write) return;
    const composed = composeText(updated);
    this.host.write(composed.text, composed.caret);
  }
}

/**
 * Folds one `result` event into a final chunk and a replacement hypothesis.
 * Only entries from `resultIndex` onward are new; the rest have already been
 * merged.
 */
export function readResults(event: SpeechRecognitionEvent): {
  finalChunk: string;
  interim: string;
} {
  let finalChunk = "";
  let interim = "";
  for (let index = event.resultIndex; index < event.results.length; index += 1) {
    const result = event.results[index];
    const transcript = result?.[0]?.transcript ?? "";
    if (result?.isFinal) finalChunk += transcript;
    else interim += transcript;
  }
  return { finalChunk, interim };
}

function startFailureMessage(cause: unknown): string {
  const name = (cause as { name?: string } | null)?.name ?? "";
  if (name === "NotAllowedError" || name === "SecurityError") {
    return recognitionErrorMessage("not-allowed");
  }
  return "Voice input could not start.";
}

function abortQuietly(instance: SpeechRecognitionLike | null): void {
  if (!instance) return;
  instance.onstart = null;
  instance.onresult = null;
  instance.onerror = null;
  instance.onend = null;
  try {
    instance.abort?.();
  } catch {
    // A recognizer that never started throws here on some builds.
  }
}
