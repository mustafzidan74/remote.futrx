import assert from "node:assert/strict";
import test from "node:test";
import type {
  SpeechRecognitionErrorEvent,
  SpeechRecognitionEvent,
  SpeechRecognitionLike,
} from "../../types/speech.ts";
import {
  ALREADY_STARTED_MESSAGE,
  INSECURE_CONTEXT_MESSAGE,
  NO_AUDIO_MESSAGE,
  NO_RECOGNITION_MESSAGE,
  recognitionErrorMessage,
  type VoiceSession,
} from "./voiceInputState.ts";
import { BrowserDictation, type DictationTimers } from "./voiceDictationController.ts";

/**
 * The regression these tests exist for: a dictation session that ran, ended,
 * and left the composer untouched — no text, no error. Every path from a
 * recognizer event to the composer's draft setter is therefore asserted on the
 * *draft*, not on the session, because the session being right while the draft
 * stays empty is exactly what the user saw.
 */

/** A recognizer whose events the test fires by hand. */
class FakeRecognition {
  lang = "";
  continuous = false;
  interimResults = false;
  maxAlternatives = 0;
  started = false;
  stopCalls = 0;
  abortCalls = 0;
  startThrows: unknown = null;
  resultIndex = 0;

  onstart: (() => void) | null = null;
  onresult: ((event: SpeechRecognitionEvent) => void) | null = null;
  onerror: ((event: SpeechRecognitionErrorEvent) => void) | null = null;
  onend: (() => void) | null = null;

  start(): void {
    if (this.startThrows) {
      const cause = this.startThrows;
      this.startThrows = null;
      throw cause;
    }
    this.started = true;
  }

  stop(): void {
    this.stopCalls += 1;
  }

  abort(): void {
    this.abortCalls += 1;
  }

  emitStart(): void {
    this.onstart?.();
  }

  /**
   * Builds one `result` event the way the API shapes it.
   *
   * `at` overrides the absolute index the batch starts from. That is how
   * Chrome re-delivers something it has already firmed up: a later event
   * points back at an index it has sent before, carrying either the same
   * transcript or a corrected one. Building the event separately from firing
   * it also lets a test hold on to one and deliver it late, which is what an
   * event Chrome had already queued when the user pressed stop looks like.
   */
  resultEvent(
    chunks: { transcript: string; isFinal: boolean }[],
    { at }: { at?: number } = {},
  ): SpeechRecognitionEvent {
    const index = at ?? this.resultIndex;
    const results: Record<number, unknown> & { length: number } = {
      length: index + chunks.length,
    };
    chunks.forEach((chunk, offset) => {
      results[index + offset] = {
        isFinal: chunk.isFinal,
        length: 1,
        0: { transcript: chunk.transcript, confidence: 0.9 },
      };
    });
    // The API only advances resultIndex past entries that have firmed up, and
    // a re-delivery never moves it backwards.
    const firmed = chunks.filter((chunk) => chunk.isFinal).length;
    this.resultIndex = Math.max(this.resultIndex, index + firmed);
    return { resultIndex: index, results } as unknown as SpeechRecognitionEvent;
  }

  /** Fires one `result` event carrying a fresh hypothesis and/or firmed text. */
  emitResult(
    chunks: { transcript: string; isFinal: boolean }[],
    options: { at?: number } = {},
  ): void {
    this.onresult?.(this.resultEvent(chunks, options));
  }

  emitError(code: string): void {
    this.onerror?.({ error: code } as unknown as SpeechRecognitionErrorEvent);
  }

  emitEnd(): void {
    this.onend?.();
  }

  get handle(): SpeechRecognitionLike {
    return this as unknown as SpeechRecognitionLike;
  }
}

function fakeClock() {
  let now = 0;
  let nextId = 1;
  const pending = new Map<number, { at: number; run: () => void }>();
  const timers: DictationTimers = {
    now: () => now,
    setTimeout: (handler, ms) => {
      const id = nextId++;
      pending.set(id, { at: now + ms, run: handler });
      return id;
    },
    clearTimeout: (id) => void pending.delete(id),
  };
  return {
    timers,
    advance(ms: number) {
      now += ms;
      for (const [id, entry] of [...pending].sort((a, b) => a[1].at - b[1].at)) {
        if (entry.at > now) continue;
        pending.delete(id);
        entry.run();
      }
    },
    get pendingCount() {
      return pending.size;
    },
  };
}

interface HarnessOptions {
  secureContext?: boolean;
  maxRestarts?: number;
  languageTag?: string;
  /** Overrides the recognizer factory, for the "no Web Speech API" case. */
  create?: () => SpeechRecognitionLike | null;
}

function harness(options: HarnessOptions = {}) {
  const recognizers: FakeRecognition[] = [];
  /** Everything the mocked composer setter received, oldest first. */
  const drafts: { text: string; caret: number }[] = [];
  const sessions: VoiceSession[] = [];
  const trace: string[] = [];
  const clock = fakeClock();

  const controller = new BrowserDictation({
    create:
      options.create ??
      (() => {
        const recognizer = new FakeRecognition();
        recognizers.push(recognizer);
        return recognizer.handle;
      }),
    secureContext: options.secureContext ?? true,
    languageTag: () => options.languageTag ?? "ar-EG",
    emit: (session) => void sessions.push(session),
    write: (text, caret) => void drafts.push({ text, caret }),
    trace: (line) => void trace.push(line),
    timers: clock.timers,
    maxRestarts: options.maxRestarts,
    flushMs: 1500,
  });

  return {
    controller,
    recognizers,
    drafts,
    sessions,
    trace,
    clock,
    /** The most recent value the composer was given. */
    draft: () => drafts.at(-1)?.text ?? null,
    caret: () => drafts.at(-1)?.caret ?? null,
    live: () => recognizers.at(-1) as FakeRecognition,
  };
}

test("interim results reach the composer draft as they are rewritten", () => {
  const bench = harness();

  bench.controller.start("", 0);
  bench.live().emitStart();
  assert.equal(bench.drafts.length, 0, "opening a session must not rewrite the draft");

  bench.live().emitResult([{ transcript: "افتح", isFinal: false }]);
  assert.equal(bench.draft(), "افتح");

  bench.live().emitResult([{ transcript: "افتح الملف", isFinal: false }]);
  assert.equal(bench.draft(), "افتح الملف", "the hypothesis is replaced, not appended");

  bench.live().emitResult([{ transcript: "افتح الملف الرئيسي", isFinal: true }]);
  assert.equal(bench.draft(), "افتح الملف الرئيسي");
  assert.equal(bench.controller.current.final, "افتح الملف الرئيسي");
  assert.equal(bench.controller.current.interim, "");
});

test("interim then final merge keeps both runs of speech", () => {
  const bench = harness({ languageTag: "en-US" });

  bench.controller.start("", 0);
  bench.live().emitStart();
  bench.live().emitResult([{ transcript: "open the file", isFinal: true }]);
  bench.live().emitResult([{ transcript: "and run", isFinal: false }]);

  assert.equal(bench.draft(), "open the file and run");

  bench.live().emitResult([{ transcript: "and run the tests", isFinal: true }]);
  assert.equal(bench.draft(), "open the file and run the tests");
});

test("a result that beats the start event still reaches the composer", () => {
  const bench = harness();

  bench.controller.start("", 0);
  // Some builds dispatch the first hypothesis before `start`. The previous
  // implementation refused to write while the session was still "starting",
  // so those words were dropped on the floor.
  bench.live().emitResult([{ transcript: "hello", isFinal: false }]);

  assert.equal(bench.draft(), "hello");
  assert.equal(bench.controller.current.status, "listening");
});

test("dictation lands at the caret and leaves the rest of the draft alone", () => {
  const bench = harness();

  bench.controller.start("Please  then commit.", 7);
  bench.live().emitStart();
  bench.live().emitResult([{ transcript: "run the linter", isFinal: true }]);

  assert.equal(bench.draft(), "Please run the linter then commit.");
  assert.equal(bench.caret(), "Please run the linter".length);
});

test("stopping asks the recognizer to flush instead of aborting its results", () => {
  const bench = harness();

  bench.controller.start("", 0);
  const recognizer = bench.live();
  recognizer.emitStart();
  recognizer.emitResult([{ transcript: "run the", isFinal: false }]);

  bench.controller.stop();
  assert.equal(recognizer.stopCalls, 1, "stop() flushes");
  assert.equal(recognizer.abortCalls, 0, "abort() would discard the pending results");

  // Chrome dispatches whatever firmed up during the flush, then ends.
  recognizer.emitResult([{ transcript: "run the tests", isFinal: true }]);
  recognizer.emitEnd();

  assert.equal(bench.draft(), "run the tests");
  assert.equal(bench.controller.current.status, "idle");
});

test("stopping with a hypothesis that never firmed up flushes it as final", () => {
  const bench = harness();

  bench.controller.start("note: ", 6);
  bench.live().emitStart();
  bench.live().emitResult([{ transcript: "ship it today", isFinal: false }]);

  bench.controller.stop();
  bench.live().emitEnd();

  assert.equal(bench.draft(), "note: ship it today");
  assert.equal(bench.controller.current.interim, "");
  assert.equal(bench.controller.current.status, "idle");
});

test("a stop whose end event never arrives still ends the session", () => {
  const bench = harness();

  bench.controller.start("", 0);
  bench.live().emitStart();
  bench.live().emitResult([{ transcript: "half a sentence", isFinal: false }]);
  bench.controller.stop();

  assert.equal(bench.controller.current.status, "listening", "still waiting for the flush");

  bench.clock.advance(1500);

  assert.equal(bench.controller.current.status, "idle");
  assert.equal(bench.draft(), "half a sentence", "the watchdog keeps the words");
  assert.equal(bench.live().abortCalls, 1, "the recognizer is released once the wait is over");
});

test("the browser ending a continuous session restarts it under the user", () => {
  const bench = harness();

  bench.controller.start("", 0);
  const first = bench.live();
  first.emitStart();
  first.emitResult([{ transcript: "first sentence", isFinal: true }]);

  // Chrome ends continuous recognition after roughly a minute of silence.
  bench.clock.advance(60_000);
  first.emitEnd();

  assert.equal(bench.recognizers.length, 2, "a fresh recognizer took over");
  assert.equal(bench.controller.restartCount, 1);
  assert.equal(bench.controller.current.status, "listening", "the session never dropped");

  const second = bench.live();
  second.emitStart();
  second.emitResult([{ transcript: "second sentence", isFinal: true }]);

  assert.equal(bench.draft(), "first sentence second sentence");
});

test("restarting stops at the budget and keeps what was dictated", () => {
  const bench = harness({ maxRestarts: 1 });

  bench.controller.start("", 0);
  bench.live().emitStart();
  bench.live().emitResult([{ transcript: "kept", isFinal: true }]);

  bench.clock.advance(60_000);
  bench.live().emitEnd();
  assert.equal(bench.recognizers.length, 2);

  bench.live().emitStart();
  bench.clock.advance(60_000);
  bench.live().emitEnd();

  assert.equal(bench.recognizers.length, 2, "the budget is spent");
  assert.equal(bench.controller.current.status, "idle");
  assert.equal(bench.draft(), "kept");
});

test("a microphone that ends immediately twice is explained, not retried forever", () => {
  const bench = harness();

  bench.controller.start("", 0);
  // Chrome does this when the tab is muted or no input device is usable: the
  // session starts and ends within the same breath, having heard nothing.
  bench.live().emitStart();
  bench.live().emitEnd();
  assert.equal(bench.recognizers.length, 2, "one retry is worth trying");

  bench.live().emitStart();
  bench.live().emitEnd();

  assert.equal(bench.recognizers.length, 2, "and then it is reported");
  assert.equal(bench.controller.current.status, "error");
  assert.equal(bench.controller.current.error, NO_AUDIO_MESSAGE);
});

test("every fatal error code becomes a sentence the user can read", () => {
  for (const code of [
    "not-allowed",
    "audio-capture",
    "network",
    "language-not-supported",
    "service-not-allowed",
  ]) {
    const bench = harness();
    bench.controller.start("draft ", 6);
    bench.live().emitStart();
    bench.live().emitResult([{ transcript: "half a sentence", isFinal: true }]);
    bench.live().emitError(code);

    assert.equal(bench.controller.current.status, "error", code);
    assert.equal(bench.controller.current.error, recognitionErrorMessage(code), code);
    assert.equal(bench.live().abortCalls, 1, `${code} releases the recognizer`);
    // A failure halfway through must not also erase the first half.
    assert.equal(bench.draft(), "draft half a sentence", code);
  }
});

test("no-speech is recorded and restarted rather than silently ending the session", () => {
  const bench = harness();

  bench.controller.start("", 0);
  bench.live().emitStart();
  bench.live().emitResult([{ transcript: "something", isFinal: true }]);
  bench.clock.advance(8_000);
  bench.live().emitError("no-speech");
  bench.live().emitEnd();

  assert.equal(bench.controller.current.status, "listening", "a silent stretch is not a failure");
  assert.equal(
    bench.controller.current.notice,
    recognitionErrorMessage("no-speech"),
    "but the reason is shown beside the live transcript",
  );
  assert.ok(bench.trace.some((line) => line.includes("no-speech")));

  // Words arriving mean the report has passed.
  bench.live().emitStart();
  bench.live().emitResult([{ transcript: "back again", isFinal: false }]);
  assert.equal(bench.controller.current.notice, "");
});

test("an insecure page says so before it opens anything", () => {
  const bench = harness({ secureContext: false });

  bench.controller.start("", 0);

  assert.equal(bench.recognizers.length, 0, "nothing is opened on plain HTTP");
  assert.equal(bench.controller.current.status, "error");
  assert.equal(bench.controller.current.error, INSECURE_CONTEXT_MESSAGE);
});

test("a browser with no Web Speech API says so", () => {
  const bench = harness({ create: () => null });

  bench.controller.start("", 0);

  assert.equal(bench.controller.current.status, "error");
  assert.equal(bench.controller.current.error, NO_RECOGNITION_MESSAGE);
});

test("a recognizer that reports it is already started is retried once", () => {
  const bench = harness();
  const failing = new FakeRecognition();
  failing.startThrows = Object.assign(new Error("recognition has already started"), {
    name: "InvalidStateError",
  });
  const recognizers: FakeRecognition[] = [failing];
  const retry = new FakeRecognition();
  recognizers.push(retry);
  let handed = 0;

  const controller = new BrowserDictation({
    create: () => recognizers[handed++]?.handle ?? null,
    secureContext: true,
    languageTag: () => "en-US",
    emit: () => {},
    write: () => {},
    timers: bench.clock.timers,
  });

  controller.start("", 0);
  assert.equal(controller.current.status, "starting", "no banner while the retry is pending");
  assert.equal(failing.abortCalls, 1, "the stuck recognizer is released first");

  bench.clock.advance(250);

  assert.equal(retry.started, true, "the retry took over");
  assert.equal(controller.current.status, "starting");
});

test("a recognizer that will not start at all explains how to recover", () => {
  const stuck = () => {
    const recognizer = new FakeRecognition();
    recognizer.startThrows = Object.assign(new Error("recognition has already started"), {
      name: "InvalidStateError",
    });
    return recognizer;
  };
  const clock = fakeClock();
  const made: FakeRecognition[] = [];
  const controller = new BrowserDictation({
    create: () => {
      const recognizer = stuck();
      made.push(recognizer);
      return recognizer.handle;
    },
    secureContext: true,
    languageTag: () => "en-US",
    emit: () => {},
    write: () => {},
    timers: clock.timers,
  });

  controller.start("", 0);
  clock.advance(250);

  assert.equal(made.length, 2, "one retry, then it gives up");
  assert.equal(controller.current.status, "error");
  assert.equal(controller.current.error, ALREADY_STARTED_MESSAGE);
});

test("abandoning a session releases the recognizer and writes nothing", () => {
  const bench = harness();

  bench.controller.start("draft", 5);
  bench.live().emitStart();
  bench.live().emitResult([{ transcript: "words", isFinal: false }]);
  const writes = bench.drafts.length;

  bench.controller.abandon();

  assert.equal(bench.drafts.length, writes, "an unmount must not rewrite a composer that is gone");
  assert.equal(bench.live().abortCalls, 1);
  assert.equal(bench.controller.current.status, "idle");

  // Nothing the abandoned recognizer says afterwards may reach the composer.
  bench.live().emitResult([{ transcript: "late", isFinal: true }]);
  bench.live().emitEnd();
  assert.equal(bench.drafts.length, writes);
});

test("a second start while a session is live is ignored", () => {
  const bench = harness();

  bench.controller.start("", 0);
  bench.live().emitStart();
  bench.controller.start("", 0);

  assert.equal(bench.recognizers.length, 1);
});

test("dismissing an error clears the banner without touching the draft", () => {
  const bench = harness();

  bench.controller.start("keep me", 7);
  bench.live().emitStart();
  bench.live().emitError("not-allowed");
  const writes = bench.drafts.length;

  bench.controller.dismiss();

  assert.equal(bench.controller.current.status, "idle");
  assert.equal(bench.controller.current.error, "");
  assert.equal(bench.drafts.length, writes);
});

test("the selected language is handed to every recognizer, restarts included", () => {
  const bench = harness({ languageTag: "ar-EG" });

  bench.controller.start("", 0);
  assert.equal(bench.live().lang, "ar-EG");
  assert.equal(bench.live().continuous, true);
  assert.equal(bench.live().interimResults, true);

  bench.live().emitStart();
  bench.clock.advance(60_000);
  bench.live().emitEnd();

  assert.equal(bench.live().lang, "ar-EG", "the restart recognises the same language");
});

/* ------------------------------------------------------------------------ *
 * Duplication
 *
 * The operator dictates one Arabic sentence in Chrome and the composer ends up
 * holding it twice. Three separate mechanisms could produce that, and the
 * tests below pin all three shut:
 *
 *  1. the recognizer re-delivering a result it has already firmed up, which an
 *     accumulator folds in a second time;
 *  2. the flush a `stop()` asks for landing after the hypothesis was already
 *     promoted, so the tail is written twice;
 *  3. two recognizers attached at once, each transcribing the same microphone
 *     into the same session.
 *
 * Every assertion is on the composer draft, because the draft is what the
 * operator reads.
 * ------------------------------------------------------------------------ */

test("a final Chrome delivers again at the same index is not a second copy", () => {
  const bench = harness();

  bench.controller.start("", 0);
  const recognizer = bench.live();
  recognizer.emitStart();
  recognizer.emitResult([{ transcript: "ابدأ تشغيل الموقع", isFinal: true }]);
  assert.equal(bench.draft(), "ابدأ تشغيل الموقع");

  // Chrome re-dispatches an already-final result while it refines the tail of
  // the utterance. The event points back at the index it has already sent.
  recognizer.emitResult([{ transcript: "ابدأ تشغيل الموقع", isFinal: true }], { at: 0 });
  recognizer.emitResult([{ transcript: "ابدأ تشغيل الموقع", isFinal: true }], { at: 0 });

  assert.equal(bench.draft(), "ابدأ تشغيل الموقع", "spoken once, written once");
  assert.equal(bench.controller.current.final, "ابدأ تشغيل الموقع");
});

test("a re-delivered final that corrects itself replaces the first reading", () => {
  const bench = harness();

  bench.controller.start("", 0);
  const recognizer = bench.live();
  recognizer.emitStart();
  recognizer.emitResult([{ transcript: "ابدا تشغيل الموقع", isFinal: true }]);
  recognizer.emitResult([{ transcript: "ابدأ تشغيل الموقع", isFinal: true }], { at: 0 });

  assert.equal(bench.draft(), "ابدأ تشغيل الموقع", "the correction wins, and stands alone");
});

test("a batch that repeats one final and adds another keeps one of each", () => {
  const bench = harness();

  bench.controller.start("", 0);
  const recognizer = bench.live();
  recognizer.emitStart();
  recognizer.emitResult([{ transcript: "ابدأ تشغيل الموقع", isFinal: true }]);
  // resultIndex points at the first *changed* entry, not the first new one, so
  // a settled result rides along with the new one in the same event.
  recognizer.emitResult(
    [
      { transcript: "ابدأ تشغيل الموقع", isFinal: true },
      { transcript: "الموقع شغال دلوقتي", isFinal: true },
    ],
    { at: 0 },
  );

  assert.equal(bench.draft(), "ابدأ تشغيل الموقع الموقع شغال دلوقتي");
});

test("a phrase that firms up out of its own hypothesis is written once", () => {
  const bench = harness();

  bench.controller.start("", 0);
  const recognizer = bench.live();
  recognizer.emitStart();
  recognizer.emitResult([{ transcript: "الموقع", isFinal: false }]);
  recognizer.emitResult([{ transcript: "الموقع شغال", isFinal: false }]);
  recognizer.emitResult([{ transcript: "الموقع شغال دلوقتي", isFinal: true }]);

  assert.equal(bench.draft(), "الموقع شغال دلوقتي");
  assert.equal(bench.controller.current.interim, "");
});

test("a final that lands after a stop promoted the hypothesis is not written twice", () => {
  const bench = harness();

  bench.controller.start("", 0);
  const recognizer = bench.live();
  recognizer.emitStart();
  recognizer.emitResult([{ transcript: "الموقع شغال دلوقتي", isFinal: false }]);

  // The event Chrome has already queued, and the handler it holds, captured
  // before the stop tears anything down.
  const deliver = recognizer.onresult;
  const queued = recognizer.resultEvent([{ transcript: "الموقع شغال دلوقتي", isFinal: true }], {
    at: 0,
  });

  bench.controller.stop();
  recognizer.emitEnd();
  assert.equal(bench.draft(), "الموقع شغال دلوقتي", "the hypothesis was promoted");

  deliver?.(queued);

  assert.equal(bench.draft(), "الموقع شغال دلوقتي", "and the late final changed nothing");
  assert.equal(bench.controller.current.status, "idle");
});

test("a final that lands after the flush watchdog fired is not written twice", () => {
  const bench = harness();

  bench.controller.start("note: ", 6);
  const recognizer = bench.live();
  recognizer.emitStart();
  recognizer.emitResult([{ transcript: "ship it today", isFinal: false }]);

  const deliver = recognizer.onresult;
  const queued = recognizer.resultEvent([{ transcript: "ship it today", isFinal: true }], {
    at: 0,
  });

  bench.controller.stop();
  // The recognizer never fires `end`, so the watchdog ends the session.
  bench.clock.advance(1500);
  assert.equal(bench.draft(), "note: ship it today");

  deliver?.(queued);
  recognizer.emitEnd();

  assert.equal(bench.draft(), "note: ship it today");
  assert.equal(bench.controller.current.status, "idle");
});

test("a restarted recognizer numbers from zero without losing or repeating a run", () => {
  const bench = harness();

  bench.controller.start("", 0);
  const first = bench.live();
  first.emitStart();
  first.emitResult([{ transcript: "ابدأ تشغيل الموقع", isFinal: true }]);

  // Chrome's own cut-off after about a minute.
  bench.clock.advance(60_000);
  first.emitEnd();

  const second = bench.live();
  assert.notEqual(second, first, "a fresh recognizer took over");
  assert.equal(second.resultIndex, 0, "which numbers its own results from zero");
  second.emitStart();
  second.emitResult([{ transcript: "الموقع شغال دلوقتي", isFinal: true }]);

  assert.equal(
    bench.draft(),
    "ابدأ تشغيل الموقع الموقع شغال دلوقتي",
    "session one is kept and session two is appended once",
  );

  // The new instance re-delivering its own index 0 is still just one copy.
  second.emitResult([{ transcript: "الموقع شغال دلوقتي", isFinal: true }], { at: 0 });
  assert.equal(bench.draft(), "ابدأ تشغيل الموقع الموقع شغال دلوقتي");

  // And the recognizer that ended has been cut loose entirely.
  assert.equal(first.onresult, null, "the ended recognizer's handlers are gone");
  first.emitResult([{ transcript: "ابدأ تشغيل الموقع", isFinal: true }], { at: 0 });
  assert.equal(bench.draft(), "ابدأ تشغيل الموقع الموقع شغال دلوقتي");
});

test("a rapid start, stop, start keeps exactly one recognizer and one copy", () => {
  const bench = harness();

  bench.controller.start("", 0);
  const first = bench.live();
  first.emitStart();
  first.emitResult([{ transcript: "ابدأ تشغيل الموقع", isFinal: true }]);

  // Two stop presses and a start, all before the recognizer has finished
  // flushing. Nothing here may open a second recognizer against the same
  // microphone: two of them transcribe every phrase twice.
  bench.controller.stop();
  bench.controller.stop();
  bench.controller.start("", 0);
  assert.equal(bench.recognizers.length, 1, "no second recognizer while the first is live");

  first.emitEnd();
  assert.equal(bench.controller.current.status, "idle");
  assert.equal(bench.draft(), "ابدأ تشغيل الموقع");
  assert.equal(first.onresult, null, "the first recognizer was released");

  const carried = bench.draft() ?? "";
  bench.controller.start(carried, carried.length);
  assert.equal(bench.recognizers.length, 2);
  const second = bench.live();
  second.emitStart();
  second.emitResult([{ transcript: "الموقع شغال دلوقتي", isFinal: true }]);

  assert.equal(bench.draft(), "ابدأ تشغيل الموقع الموقع شغال دلوقتي");
});

test("a stop does not settle a tail the firmed-up text already ends with", () => {
  const bench = harness();

  bench.controller.start("", 0);
  const recognizer = bench.live();
  recognizer.emitStart();
  // Chrome routinely reports the sentence it has just firmed up together with
  // a hypothesis for what it thinks comes next — and that hypothesis is very
  // often the tail it has only this moment settled.
  recognizer.emitResult([
    { transcript: "الموقع شغال دلوقتي", isFinal: true },
    { transcript: "دلوقتي", isFinal: false },
  ]);
  assert.equal(
    bench.draft(),
    "الموقع شغال دلوقتي دلوقتي",
    "a hypothesis is shown for as long as it is a hypothesis",
  );

  bench.controller.stop();
  recognizer.emitEnd();

  assert.equal(bench.draft(), "الموقع شغال دلوقتي", "but stopping does not settle it twice");
  assert.equal(bench.controller.current.status, "idle");
});
