import assert from "node:assert/strict";
import test from "node:test";
import { defaultVoiceLanguage, resolveVoiceLanguage } from "../../config/voice.ts";
import {
  IDLE_VOICE_SESSION,
  VoicePreferenceStore,
  applyRecognition,
  applyTranscript,
  beginSession,
  composeText,
  dismissError,
  failSession,
  finishSession,
  formatElapsed,
  joinSpeech,
  markRunning,
  markTranscribing,
  resolveEngine,
  voiceInputAvailable,
  withElapsed,
  withLevel,
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
  session = applyRecognition(session, { finalChunk: "open the file", interim: "" });
  assert.equal(session.final, "open the file");
  assert.equal(session.interim, "");

  // The next sentence starts its own hypothesis on top of the settled text.
  session = applyRecognition(session, { interim: "and run" });
  assert.equal(composeText(session).text, "open the file and run");

  session = applyRecognition(session, { finalChunk: "and run the tests", interim: "" });
  assert.equal(composeText(session).text, "open the file and run the tests");
});

test("dictation lands at the caret and leaves the rest of the draft alone", () => {
  let session = beginSession("Please  then commit.", 7);

  session = markRunning(session, "listening");
  session = applyRecognition(session, { finalChunk: "run the linter", interim: "" });

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
    finalChunk: "مرحبا",
    interim: "",
  });

  assert.deepEqual(composeText(session), { text: "مرحبا", caret: "مرحبا".length });
});

test("a draft that already ends in whitespace does not gain a second space", () => {
  const session = applyRecognition(beginSession("note: ", 6, "listening"), {
    finalChunk: "ship it",
    interim: "",
  });

  assert.equal(composeText(session).text, "note: ship it");
});

test("punctuation the recognizer emits separately hugs the word before it", () => {
  const session = applyRecognition(beginSession("", 0, "listening"), {
    finalChunk: "done",
    interim: "",
  });

  assert.equal(composeText(applyRecognition(session, { interim: "." })).text, "done.");
  assert.equal(joinSpeech("انتهى", "،"), "انتهى،");
  assert.equal(joinSpeech("انتهى", "الآن"), "انتهى الآن");
});

// A hypothesis the user watched appear should not vanish when they press stop.
test("stopping keeps a hypothesis that never firmed up", () => {
  let session = beginSession("", 0, "listening");
  session = applyRecognition(session, { finalChunk: "open the", interim: "file" });

  session = finishSession(session);

  assert.equal(session.status, "idle");
  assert.equal(session.interim, "");
  assert.equal(composeText(session).text, "open the file");
});

test("a failure halfway through keeps what was already dictated", () => {
  let session = beginSession("prefix ", 7, "listening");
  session = applyRecognition(session, { finalChunk: "half a sentence", interim: "" });

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

  assert.deepEqual(applyRecognition(idle, { finalChunk: "late", interim: "x" }), idle);
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

test("an Arabic browser starts on Egyptian Arabic, everyone else on auto", () => {
  assert.equal(defaultVoiceLanguage("ar"), "ar-EG");
  assert.equal(defaultVoiceLanguage("ar-SA"), "ar-EG");
  assert.equal(defaultVoiceLanguage("AR-eg"), "ar-EG");
  assert.equal(defaultVoiceLanguage("en-US"), "auto");
  assert.equal(defaultVoiceLanguage(undefined), "auto");
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
  assert.equal(first.language(), "auto", "an English browser starts on auto");
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
