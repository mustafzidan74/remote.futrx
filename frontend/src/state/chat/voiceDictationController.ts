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
  commitRecognizer,
  composeText,
  dismissError,
  failSession,
  finishSession,
  isAlreadyStartedError,
  markRunning,
  planAfterRecognitionEnd,
  recognitionErrorMessage,
  type RecognitionFinal,
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
 *  - **A phrase spoken once appears once.** Chrome re-dispatches results it
 *    has already firmed up, keeps dispatching them during the flush a `stop()`
 *    asks for, and numbers a restarted recognizer's results from zero again.
 *    None of those may add a second copy, so the settled text is derived from
 *    the recognizer's result list by index instead of accumulated, exactly one
 *    recognizer is ever attached, and a closed session accepts nothing.
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
  /**
   * The recognizer whose events count. Identity *is* the ownership token, and
   * there is never more than one: `launch()` refuses to build a second while
   * this is set, so a rapid toggle or a restart racing a stop cannot end up
   * with two live recognizers feeding the same session.
   */
  private recognition: SpeechRecognitionLike | null = null;
  /**
   * True once the session has been finished, failed, or abandoned. A closed
   * session accepts nothing: no late `result` from a recognizer that was on
   * its way out may write to the composer after the text was settled.
   */
  private closed = true;
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
    this.closed = false;
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
    this.closed = true;
    this.clearFlushTimer();
    const instance = this.detach();
    abortQuietly(instance);
    this.session = IDLE_VOICE_SESSION;
  }

  /** Clears an error banner without touching the composer text. */
  dismiss(): void {
    this.apply(dismissError, { write: false });
  }

  /**
   * Opens one recognizer for the running session.
   *
   * It is a no-op while an instance is still attached. Two live recognizers
   * transcribe the same microphone into the same session, so every phrase
   * lands twice — the third way this feature could duplicate text, reachable
   * through a rapid toggle, the already-started retry, or a restart racing a
   * stop. Refusing here is what makes `this.recognition` a real invariant
   * rather than a convention.
   */
  private launch(): void {
    if (this.recognition) {
      this.trace("launch ignored — a recognizer is already attached");
      return;
    }
    if (this.closed) return;
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
      if (!this.owns(instance)) return;
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
      // Two guards, not one. Identity rejects a recognizer that is no longer
      // the owner — an abandoned one, or the instance a restart replaced — and
      // `closed` rejects an event that arrives for the *current* owner after
      // the session's text was already settled by a stop, a watchdog, or a
      // failure. Without the second one, a final that lands a beat after the
      // flush deadline is folded on top of the hypothesis it duplicates.
      if (!this.owns(instance)) return;
      const { finals, interim } = readResults(event);
      if (finals.length === 0 && !interim) return;
      this.heardThisRun = true;
      // markRunning first: a result that arrives while the session is still
      // "starting" is real speech and must reach the composer, which the
      // previous implementation dropped.
      this.apply((current) =>
        applyRecognition(markRunning(current, "listening"), { finals, interim }),
      );
    };

    instance.onerror = (event: SpeechRecognitionErrorEvent) => {
      if (!this.owns(instance)) return;
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
      if (!this.owns(instance)) return;
      this.recognition = null;
      // A recognizer that has ended is finished talking to us. Cutting its
      // handlers here — not only in `abortQuietly` — is what stops a build
      // that dispatches one last `result` after `end` from writing into the
      // session the restart is about to reuse.
      releaseHandlers(instance);
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
      // The replacement recognizer numbers its results from 0 again, so what
      // this one firmed up is frozen into `committed` before it starts. Skip
      // this and the second run's first sentence overwrites the first run's;
      // do it with an accumulator instead and every restart duplicates.
      this.apply(commitRecognizer, { write: false });
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

  /**
   * Ends cleanly, folding any unfirmed hypothesis into the composer.
   *
   * The session is closed *before* the recognizer is released, so that even a
   * handler dispatched synchronously out of `abort()` finds a closed session
   * and writes nothing.
   */
  private finish(): void {
    this.closed = true;
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
    this.closed = true;
    this.clearFlushTimer();
    const instance = this.detach();
    abortQuietly(instance);
    this.apply((current) => failSession(current, message));
  }

  /** Whether an event from `instance` may still write to this session. */
  private owns(instance: SpeechRecognitionLike): boolean {
    return this.recognition === instance && !this.closed;
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
 * Reads one `result` event into the finals it carries — each with the index it
 * occupies in the recognizer's own result list — and one replacement
 * hypothesis.
 *
 * The indices are the important part. `event.resultIndex` is the first entry
 * that *changed*, not the first entry that is new, so a re-delivered or
 * corrected final shows up here again at the index it already had. Returning
 * the index rather than a concatenated chunk lets the session store it by
 * position and keep exactly one copy; the previous version returned only text,
 * which left the caller with no way to tell a new sentence from the same
 * sentence said once.
 */
export function readResults(event: SpeechRecognitionEvent): {
  finals: RecognitionFinal[];
  interim: string;
} {
  const finals: RecognitionFinal[] = [];
  let interim = "";
  const first = Math.max(0, event.resultIndex ?? 0);
  for (let index = first; index < event.results.length; index += 1) {
    const result = event.results[index];
    const transcript = result?.[0]?.transcript ?? "";
    if (result?.isFinal) finals.push({ index, transcript });
    else interim += transcript;
  }
  return { finals, interim };
}

function startFailureMessage(cause: unknown): string {
  const name = (cause as { name?: string } | null)?.name ?? "";
  if (name === "NotAllowedError" || name === "SecurityError") {
    return recognitionErrorMessage("not-allowed");
  }
  return "Voice input could not start.";
}

/**
 * Cuts every wire from a recognizer to the controller.
 *
 * Detaching the handlers is the strongest of the guards against a late result:
 * identity checks and the `closed` flag decide not to *use* an event, this one
 * means the event is never delivered at all.
 */
function releaseHandlers(instance: SpeechRecognitionLike): void {
  instance.onstart = null;
  instance.onresult = null;
  instance.onerror = null;
  instance.onend = null;
}

function abortQuietly(instance: SpeechRecognitionLike | null): void {
  if (!instance) return;
  releaseHandlers(instance);
  try {
    instance.abort?.();
  } catch {
    // A recognizer that never started throws here on some builds.
  }
}
