import assert from "node:assert/strict";
import test from "node:test";
import {
  defaultVoiceLanguage,
  describeVoiceLanguage,
  resolveVoiceLanguage,
} from "../../config/voice.ts";
import {
  IDLE_VOICE_SESSION,
  VoicePreferenceStore,
  applyRecognition,
  applyTranscript,
  beginDictationClaim,
  beginSession,
  commitRecognizer,
  composeText,
  dismissError,
  failSession,
  finishSession,
  formatElapsed,
  isDictating,
  joinSpeech,
  joinSpeechOnce,
  markRunning,
  markTranscribing,
  resolveEngine,
  voiceInputAvailable,
  withElapsed,
  withLevel,
  NO_AUDIO_MESSAGE,
  clampLevel,
  isAlreadyStartedError,
  isFatalRecognitionError,
  microphoneErrorMessage,
  microphoneTestVerdict,
  planAfterRecognitionEnd,
  recognitionErrorMessage,
  type RecognitionEndInput,
} from "./voiceInputState.ts";

test("beginSession splits the existing draft at the caret", () => {
  const session = beginSession("hello world", 5);

  assert.equal(session.before, "hello");
  assert.equal(session.after, " world");
  assert.equal(session.status, "starting");
  assert.equal(session.final, "");
  assert.equal(session.interim, "");
});

test("beginSession clamps a caret the textarea never set", () => {
  assert.deepEqual(
    { before: beginSession("abc", -5).before, after: beginSession("abc", -5).after },
    { before: "", after: "abc" },
  );
  assert.deepEqual(
    { before: beginSession("abc", 99).before, after: beginSession("abc", 99).after },
    { before: "abc", after: "" },
  );
});

// The Web Speech API rewrites its hypothesis on every event. The interim span
// has to be replaced wholesale, not appended to, or the composer accumulates
// every draft of the same sentence.
test("an interim hypothesis is replaced, never appended", () => {
  let session = beginSession("", 0, "listening");

  session = applyRecognition(session, { interim: "افتح" });
  assert.equal(composeText(session).text, "افتح");

  session = applyRecognition(session, { interim: "افتح الملف" });
  assert.equal(composeText(session).text, "افتح الملف");

  session = applyRecognition(session, { interim: "افتح الملف الرئيسي" });
  assert.equal(composeText(session).text, "افتح الملف الرئيسي");
  assert.equal(session.final, "", "nothing has firmed up yet");
});

test("a firmed-up chunk moves out of the interim span and stays", () => {
  let session = beginSession("", 0, "listening");

  session = applyRecognition(session, { interim: "open the" });
  session = applyRecognition(session, { finals: [{ index: 0, transcript: "open the file" }] });
  assert.equal(session.final, "open the file");
  assert.equal(session.interim, "");

  // The next sentence starts its own hypothesis on top of the settled text.
  session = applyRecognition(session, { interim: "and run" });
  assert.equal(composeText(session).text, "open the file and run");

  session = applyRecognition(session, { finals: [{ index: 1, transcript: "and run the tests" }] });
  assert.equal(composeText(session).text, "open the file and run the tests");
});

test("dictation lands at the caret and leaves the rest of the draft alone", () => {
  let session = beginSession("Please  then commit.", 7);

  session = markRunning(session, "listening");
  session = applyRecognition(session, { finals: [{ index: 0, transcript: "run the linter" }] });

  const composed = composeText(session);
  assert.equal(composed.text, "Please run the linter then commit.");
  // The caret sits at the logical end of what was just dictated, which is
  // direction-agnostic: RTL text puts the same offset on the other side of
  // the glyph run.
  assert.equal(composed.caret, "Please run the linter".length);
  assert.equal(composed.text.slice(composed.caret), " then commit.");
});

test("dictating into an empty composer produces no stray whitespace", () => {
  const session = applyRecognition(beginSession("", 0, "listening"), {
    finals: [{ index: 0, transcript: "مرحبا" }],
  });

  assert.deepEqual(composeText(session), { text: "مرحبا", caret: "مرحبا".length });
});

test("a draft that already ends in whitespace does not gain a second space", () => {
  const session = applyRecognition(beginSession("note: ", 6, "listening"), {
    finals: [{ index: 0, transcript: "ship it" }],
  });

  assert.equal(composeText(session).text, "note: ship it");
});

test("punctuation the recognizer emits separately hugs the word before it", () => {
  const session = applyRecognition(beginSession("", 0, "listening"), {
    finals: [{ index: 0, transcript: "done" }],
  });

  assert.equal(composeText(applyRecognition(session, { interim: "." })).text, "done.");
  assert.equal(joinSpeech("انتهى", "،"), "انتهى،");
  assert.equal(joinSpeech("انتهى", "الآن"), "انتهى الآن");
});

// A hypothesis the user watched appear should not vanish when they press stop.
test("stopping keeps a hypothesis that never firmed up", () => {
  let session = beginSession("", 0, "listening");
  session = applyRecognition(session, {
    finals: [{ index: 0, transcript: "open the" }],
    interim: "file",
  });

  session = finishSession(session);

  assert.equal(session.status, "idle");
  assert.equal(session.interim, "");
  assert.equal(composeText(session).text, "open the file");
});

test("a failure halfway through keeps what was already dictated", () => {
  let session = beginSession("prefix ", 7, "listening");
  session = applyRecognition(session, { finals: [{ index: 0, transcript: "half a sentence" }] });

  session = failSession(session, "network error");

  assert.equal(session.status, "error");
  assert.equal(session.error, "network error");
  assert.equal(composeText(session).text, "prefix half a sentence");
});

test("failSession always carries a message the user can read", () => {
  assert.equal(failSession(beginSession("", 0), "   ").error, "Voice input failed.");
});

test("dismissing an error resets to idle without touching anything else", () => {
  const failed = failSession(beginSession("draft", 5, "listening"), "no microphone");

  assert.deepEqual(dismissError(failed), IDLE_VOICE_SESSION);
  const listening = beginSession("draft", 5, "listening");
  assert.deepEqual(dismissError(listening), listening, "a healthy session is left alone");
});

// The server path has no interim step: one transcript arrives and the session
// is over.
test("a server transcript lands whole and ends the session", () => {
  let session = beginSession("Task: ", 6, "recording");
  session = withElapsed(session, 4200);
  session = markTranscribing(session);
  assert.equal(session.status, "transcribing");
  assert.equal(session.level, 0);

  session = applyTranscript(session, "افتح المشروع وشغّل الاختبارات");

  assert.equal(session.status, "idle");
  assert.equal(composeText(session).text, "Task: افتح المشروع وشغّل الاختبارات");
});

test("events after the session ended are ignored", () => {
  const idle = IDLE_VOICE_SESSION;

  assert.deepEqual(
    applyRecognition(idle, { finals: [{ index: 0, transcript: "late" }], interim: "x" }),
    idle,
  );
  assert.deepEqual(withLevel(idle, 0.8), idle);
  assert.deepEqual(withElapsed(idle, 5000), idle);
  assert.deepEqual(markRunning(idle, "listening"), idle);
});

test("the level meter is clamped to a usable range", () => {
  const session = beginSession("", 0, "listening");

  assert.equal(withLevel(session, 0.42).level, 0.42);
  assert.equal(withLevel(session, 5).level, 1);
  assert.equal(withLevel(session, -1).level, 0);
  assert.equal(withLevel(session, Number.NaN).level, 0);
});

test("the mic button hides only when neither engine exists", () => {
  assert.equal(voiceInputAvailable(true, false), true, "Chrome without a server key");
  assert.equal(voiceInputAvailable(false, true), true, "Firefox with server transcription");
  assert.equal(voiceInputAvailable(true, true), true);
  assert.equal(voiceInputAvailable(false, false), false, "Firefox with nothing configured");
});

test("resolveEngine honours the preference but never leaves a dead button", () => {
  assert.equal(resolveEngine("server", true, true), "server");
  assert.equal(resolveEngine("browser", true, true), "browser");
  // An admin turned the server off under a user who had selected it.
  assert.equal(resolveEngine("server", true, false), "browser");
  // Firefox has no Web Speech API, so the server is the only engine there.
  assert.equal(resolveEngine("browser", false, true), "server");
  assert.equal(resolveEngine("browser", false, false), null);
});

test("formatElapsed renders the recording timer", () => {
  assert.equal(formatElapsed(0), "0:00");
  assert.equal(formatElapsed(9_400), "0:09");
  assert.equal(formatElapsed(65_000), "1:05");
  assert.equal(formatElapsed(300_000), "5:00");
  assert.equal(formatElapsed(-1), "0:00");
});

// Neither default is "auto". `auto` hands the recognizer whatever
// navigator.language happens to be, which is invisible to the user and is a
// tag the vendor's speech service may not support at all.
test("an Arabic browser starts on Egyptian Arabic, everyone else on US English", () => {
  assert.equal(defaultVoiceLanguage("ar"), "ar-EG");
  assert.equal(defaultVoiceLanguage("ar-SA"), "ar-EG");
  assert.equal(defaultVoiceLanguage("AR-eg"), "ar-EG");
  assert.equal(defaultVoiceLanguage("en-US"), "en-US");
  assert.equal(defaultVoiceLanguage("fr-FR"), "en-US");
  assert.equal(defaultVoiceLanguage(undefined), "en-US");
});

test("auto resolves to the browser language, an explicit tag to itself", () => {
  assert.equal(resolveVoiceLanguage("auto", "fr-FR"), "fr-FR");
  assert.equal(resolveVoiceLanguage("auto", undefined), "");
  assert.equal(resolveVoiceLanguage("ar-EG", "en-US"), "ar-EG");
});

function memoryStorage() {
  const values = new Map<string, string>();
  return {
    getItem: (key: string) => values.get(key) ?? null,
    setItem: (key: string, value: string) => void values.set(key, value),
  };
}

test("the language choice survives a reload", () => {
  const storage = memoryStorage();

  const first = new VoicePreferenceStore(storage, "en-US");
  assert.equal(first.language(), "en-US", "an English browser starts on US English");
  first.setLanguage("ar-SA");

  assert.equal(new VoicePreferenceStore(storage, "en-US").language(), "ar-SA");
});

test("a corrupt stored preference falls back to the browser default", () => {
  const storage = memoryStorage();
  storage.setItem("remote.futrx.voiceLanguage.v1", "kl-KL");
  storage.setItem("remote.futrx.voiceEngine.v1", "quantum");

  const store = new VoicePreferenceStore(storage, "ar-EG");

  assert.equal(store.language(), "ar-EG");
  assert.equal(store.engine(), "browser");
});

test("the engine choice survives a reload and defaults to the browser", () => {
  const storage = memoryStorage();

  assert.equal(new VoicePreferenceStore(storage, "en-US").engine(), "browser");
  new VoicePreferenceStore(storage, "en-US").setEngine("server");
  assert.equal(new VoicePreferenceStore(storage, "en-US").engine(), "server");
});

test("preferences degrade to in-memory when storage is unavailable", () => {
  const store = new VoicePreferenceStore(null, "ar-EG");

  store.setLanguage("en-GB");

  assert.equal(store.language(), "ar-EG", "nothing persisted, so the default stands");
});

// The upload for a server dictation resolves long after the user may have
// pressed stop. A transcript that lands then must be dropped, not pasted over
// whatever they typed in the meantime.
test("a transcript arriving after the session ended is discarded", () => {
  let session = beginSession("keep this", 9, "recording");
  session = markTranscribing(session);
  session = finishSession(session);
  assert.equal(session.status, "idle");

  const late = applyTranscript(session, "words nobody is waiting for");

  assert.deepEqual(late, session, "the ended session is returned untouched");
  assert.equal(composeText(late).text, "keep this");
});

test("markTranscribing cannot revive a session that already ended", () => {
  const ended = finishSession(beginSession("draft", 5, "recording"));

  assert.deepEqual(markTranscribing(ended), ended);
  assert.equal(markTranscribing(ended).status, "idle");
});

// Escape means "stop dictating" and "cancel the run". The cancel shortcut
// consults this claim so a single Escape does the local thing first.
test("a dictation claim is visible while held and released exactly once", () => {
  assert.equal(isDictating(), false);

  const release = beginDictationClaim();
  assert.equal(isDictating(), true);

  release();
  assert.equal(isDictating(), false);

  // A cleanup that runs twice must not push the counter below zero, or the
  // next real claim would read as "nobody is dictating".
  release();
  assert.equal(isDictating(), false);
  const second = beginDictationClaim();
  assert.equal(isDictating(), true);
  second();
  assert.equal(isDictating(), false);
});

test("two composers dictating both have to let go before the run shortcut returns", () => {
  const first = beginDictationClaim();
  const second = beginDictationClaim();
  assert.equal(isDictating(), true);

  first();
  assert.equal(isDictating(), true, "one microphone is still live");

  second();
  assert.equal(isDictating(), false);
});

/* --------------------------------------------------------------------- *
 * Diagnostics
 *
 * The feature failed silently: a session ran, ended, and told the user
 * nothing. These pin the vocabulary that replaced the silence.
 * --------------------------------------------------------------------- */

test("every Web Speech error code produces a specific, actionable sentence", () => {
  const messages = new Map<string, string>();
  for (const code of [
    "not-allowed",
    "service-not-allowed",
    "audio-capture",
    "network",
    "language-not-supported",
    "no-speech",
    "aborted",
    "bad-grammar",
  ]) {
    const message = recognitionErrorMessage(code);
    assert.ok(message.length > 20, `${code} needs more than a shrug`);
    assert.ok(
      !message.startsWith("Voice input failed ("),
      `${code} should be explained, not echoed back as a code`,
    );
    messages.set(code, message);
  }
  assert.equal(new Set(messages.values()).size, messages.size, "no two codes share a message");

  // The two that used to be swallowed entirely are the most informative ones.
  assert.match(recognitionErrorMessage("no-speech"), /silence/i);
  assert.match(recognitionErrorMessage("not-allowed"), /padlock|allow/i);
  // Chrome uploads the audio to Google, so a blocked network is a real cause.
  assert.match(recognitionErrorMessage("network"), /network|service/i);
  // An unknown code still names itself rather than vanishing.
  assert.equal(recognitionErrorMessage("weird-new-code"), "Voice input failed (weird-new-code).");
  assert.equal(recognitionErrorMessage(""), "Voice input failed.");
});

test("only the codes that cannot be retried end the session", () => {
  for (const code of [
    "not-allowed",
    "service-not-allowed",
    "audio-capture",
    "network",
    "language-not-supported",
    "bad-grammar",
  ]) {
    assert.equal(isFatalRecognitionError(code), true, code);
  }
  // These are how a continuous recognizer stops between phrases.
  assert.equal(isFatalRecognitionError("no-speech"), false);
  assert.equal(isFatalRecognitionError("aborted"), false);
  assert.equal(isFatalRecognitionError(""), false);
});

test("a refused getUserMedia is reported by its cause, not as a generic failure", () => {
  assert.match(microphoneErrorMessage({ name: "NotAllowedError" }), /blocked/i);
  assert.match(microphoneErrorMessage({ name: "NotFoundError" }), /No microphone/i);
  assert.match(microphoneErrorMessage({ name: "NotReadableError" }), /another application/i);
  assert.match(microphoneErrorMessage({ name: "OverconstrainedError" }), /matched/i);
  assert.equal(microphoneErrorMessage(null), "The microphone could not be opened.");
});

test("Chrome's already-started throw is recognised by name or by message", () => {
  assert.equal(isAlreadyStartedError({ name: "InvalidStateError" }), true);
  assert.equal(isAlreadyStartedError(new Error("recognition has already started")), true);
  assert.equal(isAlreadyStartedError(new Error("something else")), false);
  assert.equal(isAlreadyStartedError(null), false);
});

/* --------------------------------------------------------------------- *
 * What to do when the recognizer ends by itself
 * --------------------------------------------------------------------- */

function endInput(overrides: Partial<RecognitionEndInput> = {}): RecognitionEndInput {
  return {
    stopRequested: false,
    errorCode: "",
    restarts: 0,
    maxRestarts: 20,
    deadStarts: 0,
    ranForMs: 60_000,
    heard: true,
    ...overrides,
  };
}

test("a user-requested stop always finishes, whatever the recognizer said", () => {
  assert.deepEqual(planAfterRecognitionEnd(endInput({ stopRequested: true })), {
    action: "finish",
  });
  // Even a fatal-looking code: the user asked to stop, and a banner about a
  // session they deliberately ended is noise.
  assert.deepEqual(
    planAfterRecognitionEnd(endInput({ stopRequested: true, errorCode: "aborted" })),
    { action: "finish" },
  );
});

test("Chrome's silence cut-off restarts instead of dropping the microphone", () => {
  assert.deepEqual(planAfterRecognitionEnd(endInput()), { action: "restart", deadStart: false });
  assert.deepEqual(planAfterRecognitionEnd(endInput({ errorCode: "no-speech" })), {
    action: "restart",
    deadStart: false,
  });
});

test("restarting is bounded, and the words survive the last one", () => {
  assert.deepEqual(planAfterRecognitionEnd(endInput({ restarts: 20, maxRestarts: 20 })), {
    action: "finish",
  });
});

test("a run that ended at once having heard nothing gets one retry, then an explanation", () => {
  const dead = endInput({ ranForMs: 0, heard: false });

  assert.deepEqual(planAfterRecognitionEnd(dead), { action: "restart", deadStart: true });
  assert.deepEqual(planAfterRecognitionEnd({ ...dead, deadStarts: 1 }), {
    action: "fail",
    message: NO_AUDIO_MESSAGE,
  });
  // A short run that *did* hear something is a normal short phrase.
  assert.deepEqual(planAfterRecognitionEnd({ ...dead, heard: true, deadStarts: 1 }), {
    action: "restart",
    deadStart: false,
  });
});

test("a fatal code fails immediately, ahead of any restart budget", () => {
  assert.deepEqual(planAfterRecognitionEnd(endInput({ errorCode: "not-allowed" })), {
    action: "fail",
    message: recognitionErrorMessage("not-allowed"),
  });
  assert.deepEqual(
    planAfterRecognitionEnd(endInput({ errorCode: "network", restarts: 0, ranForMs: 0 })),
    { action: "fail", message: recognitionErrorMessage("network") },
  );
});

/* --------------------------------------------------------------------- *
 * Microphone self-test
 * --------------------------------------------------------------------- */

test("the microphone test reports which of the two failures the user has", () => {
  assert.match(microphoneTestVerdict(0.8), /works/i);
  assert.match(microphoneTestVerdict(0.3), /works/i);
  assert.match(microphoneTestVerdict(0.12), /quiet/i);
  assert.match(microphoneTestVerdict(0.05), /quiet/i);
  assert.match(microphoneTestVerdict(0.01), /No sound/i);
  assert.match(microphoneTestVerdict(0), /No sound/i);
  // The percentage is what the user reads back to whoever is helping them.
  assert.match(microphoneTestVerdict(0.62), /62%/);
});

test("levels from an analyser are clamped before anything renders them", () => {
  assert.equal(clampLevel(0.42), 0.42);
  assert.equal(clampLevel(5), 1);
  assert.equal(clampLevel(-1), 0);
  assert.equal(clampLevel(Number.NaN), 0);
});

test("the tooltip label always names the language actually being recognised", () => {
  assert.equal(describeVoiceLanguage("ar-EG", "en-US"), "العربية (مصر)");
  assert.equal(describeVoiceLanguage("en-GB", "ar-EG"), "English (UK)");
  // "Auto" on its own tells the user nothing, so the resolved tag comes with it.
  assert.equal(describeVoiceLanguage("auto", "fr-FR"), "Auto (browser language): fr-FR");
  assert.equal(describeVoiceLanguage("auto", undefined), "Auto (browser language)");
});

/* ------------------------------------------------------------------------ *
 * Duplication
 *
 * The settled text is a *pure function* of the recognizer's result list. That
 * is the property these tests protect, because the alternative — an
 * accumulator that appends whatever the last event carried — turns every
 * re-delivery of an already-firmed result into a second copy of the sentence,
 * which is what the operator saw in Chrome.
 * ------------------------------------------------------------------------ */

test("a final re-delivered at the same index replaces it instead of adding a copy", () => {
  let session = beginSession("", 0, "listening");

  session = applyRecognition(session, { finals: [{ index: 0, transcript: "ابدأ تشغيل الموقع" }] });
  assert.equal(composeText(session).text, "ابدأ تشغيل الموقع");

  // Chrome re-dispatches results it has already firmed up.
  session = applyRecognition(session, { finals: [{ index: 0, transcript: "ابدأ تشغيل الموقع" }] });
  session = applyRecognition(session, { finals: [{ index: 0, transcript: "ابدأ تشغيل الموقع" }] });

  assert.equal(composeText(session).text, "ابدأ تشغيل الموقع");
  assert.deepEqual([...session.finals], ["ابدأ تشغيل الموقع"]);
});

test("a corrected re-delivery wins over the reading it replaces", () => {
  let session = beginSession("", 0, "listening");

  session = applyRecognition(session, { finals: [{ index: 0, transcript: "ابدا تشغيل الموقع" }] });
  session = applyRecognition(session, { finals: [{ index: 0, transcript: "ابدأ تشغيل الموقع" }] });

  assert.equal(session.final, "ابدأ تشغيل الموقع");
});

test("the settled text is the same whatever order the results are replayed in", () => {
  const events = [
    { finals: [{ index: 0, transcript: "ابدأ تشغيل الموقع" }] },
    { finals: [{ index: 1, transcript: "الموقع شغال دلوقتي" }] },
    { finals: [{ index: 0, transcript: "ابدأ تشغيل الموقع" }] },
  ];
  const settle = (order: typeof events) =>
    order.reduce(
      (session, event) => applyRecognition(session, event),
      beginSession("", 0, "listening"),
    ).final;

  assert.equal(settle(events), "ابدأ تشغيل الموقع الموقع شغال دلوقتي");
  assert.equal(settle([...events].reverse()), "ابدأ تشغيل الموقع الموقع شغال دلوقتي");
  assert.equal(settle([...events, ...events]), "ابدأ تشغيل الموقع الموقع شغال دلوقتي");
});

test("commitRecognizer freezes a run so the next one may restart at index 0", () => {
  let session = beginSession("", 0, "listening");
  session = applyRecognition(session, { finals: [{ index: 0, transcript: "ابدأ تشغيل الموقع" }] });

  // Chrome ended the recognizer; a fresh one takes over and numbers from zero.
  session = commitRecognizer(session);
  assert.equal(session.committed, "ابدأ تشغيل الموقع");
  assert.deepEqual([...session.finals], [], "the index map belongs to one instance only");
  assert.equal(session.final, "ابدأ تشغيل الموقع", "and nothing moved in the composer");

  session = applyRecognition(session, { finals: [{ index: 0, transcript: "الموقع شغال دلوقتي" }] });
  assert.equal(composeText(session).text, "ابدأ تشغيل الموقع الموقع شغال دلوقتي");

  // The second instance re-delivering its own index 0 is still one copy.
  session = applyRecognition(session, { finals: [{ index: 0, transcript: "الموقع شغال دلوقتي" }] });
  assert.equal(composeText(session).text, "ابدأ تشغيل الموقع الموقع شغال دلوقتي");
});

test("a gap in the result indices does not shuffle the settled text", () => {
  let session = beginSession("", 0, "listening");

  session = applyRecognition(session, { finals: [{ index: 2, transcript: "third" }] });
  session = applyRecognition(session, { finals: [{ index: 0, transcript: "first" }] });
  session = applyRecognition(session, { finals: [{ index: 1, transcript: "second" }] });

  assert.equal(session.final, "first second third");
});

test("finishing does not settle a hypothesis the text already ends with", () => {
  let session = beginSession("", 0, "listening");
  session = applyRecognition(session, {
    finals: [{ index: 0, transcript: "الموقع شغال دلوقتي" }],
    // The recognizer's guess at what comes next is the tail it just firmed up.
    interim: "دلوقتي",
  });

  session = finishSession(session);

  assert.equal(composeText(session).text, "الموقع شغال دلوقتي");
});

test("finishing is idempotent, so a watchdog and a real end cannot both write", () => {
  let session = beginSession("", 0, "listening");
  session = applyRecognition(session, {
    finals: [{ index: 0, transcript: "افتح المشروع" }],
    interim: "وشغّل الاختبارات",
  });

  const once = finishSession(session);
  const twice = finishSession(once);

  assert.equal(composeText(once).text, "افتح المشروع وشغّل الاختبارات");
  assert.deepEqual(twice, once);
});

test("a server transcript that repeats what is already there lands once", () => {
  let session = beginSession("", 0, "recording");
  session = applyRecognition(session, { finals: [{ index: 0, transcript: "الموقع شغال دلوقتي" }] });

  session = applyTranscript(markTranscribing(session), "الموقع شغال دلوقتي");

  assert.equal(composeText(session).text, "الموقع شغال دلوقتي");
});

test("joinSpeechOnce drops a repeated run but never a genuine continuation", () => {
  assert.equal(joinSpeechOnce("الموقع شغال", "الموقع شغال"), "الموقع شغال");
  assert.equal(joinSpeechOnce("ابدأ تشغيل الموقع", "الموقع"), "ابدأ تشغيل الموقع");
  assert.equal(joinSpeechOnce("الموقع شغال", "دلوقتي"), "الموقع شغال دلوقتي");
  // Whitespace is normalised the same way joinSpeech normalises it.
  assert.equal(joinSpeechOnce("open  the file", "the   file"), "open  the file");
  // A partial word is not a repeat: "شغال" does not end "الموقع شغالة".
  assert.equal(joinSpeechOnce("الموقع شغالة", "شغال"), "الموقع شغالة شغال");
  assert.equal(joinSpeechOnce("", "الموقع شغال"), "الموقع شغال");
  assert.equal(joinSpeechOnce("الموقع شغال", ""), "الموقع شغال");
  // Punctuation still hugs the word before it.
  assert.equal(joinSpeechOnce("انتهى", "،"), "انتهى،");
});
