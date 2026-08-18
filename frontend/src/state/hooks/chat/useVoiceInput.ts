import type { RefObject } from "preact";
import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";
import { transcriptionApi } from "../../../api/transcriptionApi";
import {
  currentBrowserLocale,
  describeVoiceLanguage,
  resolveVoiceLanguage,
  type VoiceEngine,
  type VoiceLanguage,
} from "../../../config/voice";
import type { TranscriptionClientConfig } from "../../../models/transcription";
import {
  mediaRecorderSupported,
  secureContextAvailable,
  speechRecognitionConstructor,
  speechRecognitionSupported,
} from "../../../types/speech";
import { BrowserDictation } from "../../chat/voiceDictationController";
import {
  IDLE_MICROPHONE_TEST,
  IDLE_VOICE_SESSION,
  INSECURE_CONTEXT_MESSAGE,
  MIC_TEST_DURATION_MS,
  applyTranscript,
  beginDictationClaim,
  beginSession,
  clampLevel,
  composeText,
  dismissError,
  failSession,
  finishSession,
  markRunning,
  markTranscribing,
  microphoneErrorMessage,
  microphoneTestVerdict,
  resolveEngine,
  voiceInputAvailable,
  voicePreferenceStore,
  withElapsed,
  withLevel,
  type MicrophoneTest,
  type VoiceSession,
} from "../../chat/voiceInputState";

/** How often the level meter and the recording timer are refreshed. */
const METER_INTERVAL_MS = 100;

/** How many diagnostic lines the mic menu keeps. Enough to explain one session. */
const TRACE_LIMIT = 10;

export interface VoiceInput {
  /** False hides the mic button entirely: no engine is available here. */
  available: boolean;
  session: VoiceSession;
  /** The engine a click would actually start, or null when none can run. */
  engine: VoiceEngine | null;
  /** The engine the user asked for, which may differ from `engine`. */
  preferredEngine: VoiceEngine;
  serverAvailable: boolean;
  browserAvailable: boolean;
  /** False on plain HTTP, where no microphone can be opened at all. */
  secureContext: boolean;
  language: VoiceLanguage;
  /** The human name of the selected language, for the tooltip. */
  languageLabel: string;
  /** BCP-47 tag for the textarea's `lang`, or "" when the choice is "auto". */
  languageTag: string;
  active: boolean;
  /** The hypothesis still being rewritten, for the live strip. */
  interim: string;
  /** How many times the browser ended the session and it was restarted. */
  restarts: number;
  /** Recent lifecycle lines, newest last, shown in the mic menu. */
  diagnostics: string[];
  microphoneTest: MicrophoneTest;
  runMicrophoneTest: () => void;
  toggle: () => void;
  stop: () => void;
  setLanguage: (language: VoiceLanguage) => void;
  setPreferredEngine: (engine: VoiceEngine) => void;
  dismiss: () => void;
}

/**
 * Drives dictation for one composer.
 *
 * Two engines sit behind the same button. The browser's Web Speech API is the
 * default because it is free and streams words as they are spoken; the server
 * fallback exists for Firefox (no Web Speech API at all) and for users who
 * want the better Arabic a hosted model gives. Neither ever sends anything on
 * its own — the transcript lands in the composer and the user presses Send.
 *
 * The Web Speech lifecycle lives in `BrowserDictation`, a pure controller with
 * its browser objects injected, so it can be tested without a DOM. What is
 * left here is the server engine's recorder, the microphone self-test, and the
 * glue that mirrors both into React state and into the composer's draft.
 *
 * One rule is worth stating because breaking it is what broke this feature:
 * **nothing in this hook opens a second microphone capture while the Web
 * Speech recognizer is running.** Chrome hands its recognizer the same input
 * device, and a concurrent `getUserMedia` restarts that device out from under
 * it — the recognizer then ends with `aborted`/`no-speech` having heard
 * nothing at all. The level meter therefore runs for the *server* engine,
 * which owns its stream anyway, and the "Test microphone" action, which never
 * runs at the same time as recognition.
 */
export function useVoiceInput({
  chatId,
  text,
  onTextChange,
  textareaRef,
  disabled = false,
}: {
  chatId?: string;
  text: string;
  onTextChange: (text: string) => void;
  textareaRef: RefObject<HTMLTextAreaElement>;
  disabled?: boolean;
}): VoiceInput {
  const [session, setSession] = useState<VoiceSession>(IDLE_VOICE_SESSION);
  const [language, setLanguageState] = useState<VoiceLanguage>(() =>
    voicePreferenceStore.language(),
  );
  const [preferredEngine, setPreferredEngineState] = useState<VoiceEngine>(() =>
    voicePreferenceStore.engine(),
  );
  const [serverConfig, setServerConfig] = useState<TranscriptionClientConfig | null>(null);
  const [diagnostics, setDiagnostics] = useState<string[]>([]);
  const [microphoneTest, setMicrophoneTest] = useState<MicrophoneTest>(IDLE_MICROPHONE_TEST);

  const secureContext = useMemo(() => secureContextAvailable(), []);
  const browserAvailable = useMemo(() => speechRecognitionSupported(), []);
  const serverAvailable = !!serverConfig?.enabled && mediaRecorderSupported();
  const engine = resolveEngine(preferredEngine, browserAvailable, serverAvailable);

  // Live handles for the running server session. They are refs because the
  // recorder callbacks outlive the render that installed them.
  const recorder = useRef<MediaRecorder | null>(null);
  const stream = useRef<MediaStream | null>(null);
  const audioContext = useRef<AudioContext | null>(null);
  const meterTimer = useRef<number | null>(null);
  const ceilingTimer = useRef<number | null>(null);
  const startedAt = useRef(0);
  const sessionRef = useRef<VoiceSession>(IDLE_VOICE_SESSION);
  // Which engine owns the live session, so stop() knows where to send the ask.
  const runningEngine = useRef<VoiceEngine | null>(null);
  // Every server session gets a number, and every async continuation captures
  // the number it started under. `getUserMedia` behind a permission prompt, a
  // MediaRecorder flush, and an upload round trip all resolve long after the
  // user may have pressed stop, switched chats, or unmounted the composer —
  // and a stale continuation that still owns the microphone is how you get a
  // hot mic and an upload nobody asked for.
  const generation = useRef(0);
  const changeText = useRef(onTextChange);
  changeText.current = onTextChange;
  const languageRef = useRef(language);
  languageRef.current = language;
  const micTestHandles = useRef<MicTestHandles>({ stream: null, context: null, timer: null });

  const trace = useCallback((line: string) => {
    const stamp = new Date().toLocaleTimeString();
    setDiagnostics((lines) => [...lines, `${stamp} · ${line}`].slice(-TRACE_LIMIT));
  }, []);

  /** Mirrors a session into React state and into the ref the callbacks read. */
  const publish = useCallback((next: VoiceSession) => {
    sessionRef.current = next;
    setSession(next);
  }, []);

  /**
   * The only path from a session to the composer's draft.
   *
   * `onTextChange` is the controller's own `setText`, which writes the chat's
   * per-chat draft store as well as React state — so dictated text survives a
   * chat switch exactly as typed text does.
   */
  const writeDraft = useCallback(
    (value: string, caret: number) => {
      changeText.current(value);
      // The caret is a logical offset, so this is correct for Arabic too: the
      // textarea keeps `dir="auto"` and places the caret after the same
      // character regardless of which way the glyphs run.
      const textarea = textareaRef.current;
      if (!textarea) return;
      requestAnimationFrame(() => {
        textarea.setSelectionRange(caret, caret);
      });
    },
    [textareaRef],
  );

  /** Applies a transition for the server engine and mirrors the text out. */
  const commit = useCallback(
    (next: (current: VoiceSession) => VoiceSession, { writeText = true } = {}) => {
      const updated = next(sessionRef.current);
      publish(updated);
      if (!writeText) return;
      const composed = composeText(updated);
      writeDraft(composed.text, composed.caret);
    },
    [publish, writeDraft],
  );

  /** The Web Speech engine, with its browser objects injected. */
  const dictation = useMemo(
    () =>
      new BrowserDictation({
        create: () => {
          const Recognition = speechRecognitionConstructor();
          return Recognition ? new Recognition() : null;
        },
        secureContext,
        languageTag: () => resolveVoiceLanguage(languageRef.current, currentBrowserLocale()),
        emit: publish,
        write: writeDraft,
        trace,
      }),
    [publish, secureContext, trace, writeDraft],
  );

  const stillOwns = useCallback((token: number) => generation.current === token, []);

  /** Ends the current server session for good, so no late callback revives it. */
  const retireSession = useCallback(() => {
    generation.current += 1;
  }, []);

  /** Tears down every live server handle. Safe to call more than once. */
  const releaseHardware = useCallback(() => {
    if (meterTimer.current !== null) {
      clearInterval(meterTimer.current);
      meterTimer.current = null;
    }
    if (ceilingTimer.current !== null) {
      clearTimeout(ceilingTimer.current);
      ceilingTimer.current = null;
    }
    if (recorder.current && recorder.current.state !== "inactive") {
      recorder.current.stop();
    }
    recorder.current = null;
    stream.current?.getTracks().forEach((track) => track.stop());
    stream.current = null;
    void audioContext.current?.close().catch(() => {});
    audioContext.current = null;
  }, []);

  // Ask the server once whether its fallback is on. A failure here is not an
  // error the user needs to see: it just means the browser engine is all there
  // is, which is the common case.
  useEffect(() => {
    let cancelled = false;
    transcriptionApi
      .clientConfig()
      .then((config) => !cancelled && setServerConfig(config))
      .catch(() => !cancelled && setServerConfig(null));
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(
    () => () => {
      dictation.abandon();
      retireSession();
      releaseHardware();
      stopMicrophoneTest(micTestHandles.current);
    },
    [dictation, releaseHardware, retireSession],
  );

  /**
   * Starts the level meter and the elapsed timer for the server engine, which
   * already owns the stream being recorded. The browser engine deliberately
   * has no meter: opening a second capture beside Chrome's recognizer is what
   * silently killed dictation in the first place.
   */
  const startMeter = useCallback(
    (token: number, source: MediaStream) => {
      startedAt.current = Date.now();
      let analyser: AnalyserNode | null = null;
      let samples: Uint8Array<ArrayBuffer> | null = null;
      try {
        const Context = window.AudioContext ?? window.webkitAudioContext;
        if (Context) {
          const context = new Context();
          audioContext.current = context;
          analyser = context.createAnalyser();
          analyser.fftSize = 512;
          context.createMediaStreamSource(source).connect(analyser);
          samples = new Uint8Array(new ArrayBuffer(analyser.frequencyBinCount));
        }
      } catch {
        analyser = null;
      }

      meterTimer.current = window.setInterval(() => {
        if (!stillOwns(token)) return;
        const elapsed = Date.now() - startedAt.current;
        const level = analyser && samples ? readLevel(analyser, samples) : 0;
        commit((current) => withLevel(withElapsed(current, elapsed), level), {
          writeText: false,
        });
      }, METER_INTERVAL_MS);
    },
    [commit, stillOwns],
  );

  const stop = useCallback(() => {
    if (runningEngine.current === "browser") {
      dictation.stop();
      return;
    }
    const current = sessionRef.current;
    if (current.status === "idle" || current.status === "error") return;
    // The server engine finishes in the recorder's stop handler, which needs
    // the stream alive long enough to flush its last chunk. That handler is
    // the one place allowed to carry the session forward past a stop.
    if (current.status === "recording" && recorder.current?.state === "recording") {
      recorder.current.stop();
      return;
    }
    // Everything else ends here, including a stop pressed while the upload is
    // in flight: the session is retired so the transcript, when it lands, is
    // discarded instead of overwriting whatever the user typed since.
    retireSession();
    releaseHardware();
    commit(finishSession);
  }, [commit, dictation, releaseHardware, retireSession]);

  const startServer = useCallback(
    async (token: number) => {
      const maxSeconds = serverConfig?.maxSeconds ?? 300;
      const maxBytes = serverConfig?.maxBytes ?? 0;
      if (!secureContext) {
        retireSession();
        commit((current) => failSession(current, INSECURE_CONTEXT_MESSAGE));
        return;
      }
      let media: MediaStream;
      try {
        media = await navigator.mediaDevices.getUserMedia({ audio: true });
      } catch (cause) {
        if (!stillOwns(token)) return;
        retireSession();
        trace(`microphone refused: ${(cause as { name?: string } | null)?.name ?? "unknown"}`);
        commit((current) => failSession(current, microphoneErrorMessage(cause)));
        return;
      }
      // The permission prompt can sit open for a long time. If the user gave
      // up on it — pressed stop, hit Escape, or navigated to another chat —
      // the microphone they were granted is handed straight back.
      if (!stillOwns(token)) {
        media.getTracks().forEach((track) => track.stop());
        return;
      }
      stream.current = media;

      const mimeType = preferredRecorderMimeType();
      const instance = new MediaRecorder(media, mimeType ? { mimeType } : undefined);
      const chunks: Blob[] = [];
      instance.ondataavailable = (event) => {
        if (event.data.size > 0) chunks.push(event.data);
      };
      instance.onstop = () => {
        const durationMs = Date.now() - startedAt.current;
        // A second stop press, or an unmount, already retired this session.
        // Releasing the hardware is still right; resurrecting the UI and
        // uploading the clip is not.
        if (!stillOwns(token)) {
          releaseHardware();
          return;
        }
        retireSession();
        releaseHardware();
        const blob = new Blob(chunks, { type: instance.mimeType || "audio/webm" });
        if (blob.size === 0) {
          trace("recorder produced no audio");
          commit(finishSession);
          return;
        }
        if (maxBytes > 0 && blob.size > maxBytes) {
          commit((current) =>
            failSession(current, "That recording is too large to transcribe. Try a shorter one."),
          );
          return;
        }
        // The upload owns the rest of this session, so it gets its own token:
        // stopping during "Transcribing…" retires that one and the transcript
        // is dropped rather than pasted over whatever was typed since.
        generation.current += 1;
        const uploadToken = generation.current;
        commit(markTranscribing, { writeText: false });
        trace(`uploading ${Math.round(blob.size / 1024)} KB`);
        transcriptionApi
          .transcribe({ audio: blob, language: languageRef.current, durationMs, chatId })
          .then((result) => {
            if (!stillOwns(uploadToken)) return;
            retireSession();
            trace(`transcript: ${result.text.length} characters`);
            commit((current) => applyTranscript(current, result.text));
          })
          .catch((cause) => {
            if (!stillOwns(uploadToken)) return;
            retireSession();
            commit((current) => failSession(current, (cause as Error).message));
          });
      };

      recorder.current = instance;
      instance.start();
      startMeter(token, media);
      commit((current) => markRunning(current, "recording"), { writeText: false });

      // The server refuses anything past its ceiling, so stop before the
      // upload is wasted rather than after.
      ceilingTimer.current = window.setTimeout(() => {
        if (recorder.current === instance && instance.state === "recording") instance.stop();
      }, maxSeconds * 1000);
    },
    [
      chatId,
      commit,
      releaseHardware,
      retireSession,
      secureContext,
      serverConfig?.maxBytes,
      serverConfig?.maxSeconds,
      startMeter,
      stillOwns,
      trace,
    ],
  );

  const toggle = useCallback(() => {
    // `disabled` gates starting, never stopping. An attachment upload that
    // begins mid-dictation, or a socket that drops, must not leave the only
    // way off the microphone behind a disabled button.
    if (sessionRef.current.status !== "idle" && sessionRef.current.status !== "error") {
      stop();
      return;
    }
    if (disabled || !engine) return;
    const textarea = textareaRef.current;
    const caret = textarea?.selectionStart ?? text.length;
    setDiagnostics([]);
    runningEngine.current = engine;
    if (engine === "browser") {
      dictation.start(text, caret);
      return;
    }
    generation.current += 1;
    const token = generation.current;
    commit(() => beginSession(text, caret), { writeText: false });
    void startServer(token);
  }, [commit, dictation, disabled, engine, startServer, stop, text, textareaRef]);

  // Escape stops dictation, matching how every other transient composer state
  // is dismissed.
  //
  // This is an ordinary bubble listener that consumes nothing. The chat's
  // cancel shortcut is the one that stands down, by asking the shared claim
  // registered below — swallowing the key here would also rob every modal in
  // the app of its Escape, which is worse than the problem it solves.
  useEffect(() => {
    if (session.status === "idle" || session.status === "error") return;
    const releaseClaim = beginDictationClaim();
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== "Escape") return;
      stop();
    }
    window.addEventListener("keydown", onKeyDown);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      releaseClaim();
    };
  }, [session.status, stop]);

  const setLanguage = useCallback((next: VoiceLanguage) => {
    voicePreferenceStore.setLanguage(next);
    setLanguageState(next);
  }, []);

  const setPreferredEngine = useCallback((next: VoiceEngine) => {
    voicePreferenceStore.setEngine(next);
    setPreferredEngineState(next);
  }, []);

  const dismiss = useCallback(() => {
    if (runningEngine.current === "browser") {
      dictation.dismiss();
      return;
    }
    commit(dismissError, { writeText: false });
  }, [commit, dictation]);

  /**
   * Records two seconds and reports the input level.
   *
   * This is the question the composer cannot answer: did the microphone
   * deliver any audio at all? A working meter here with nothing in the
   * composer points at recognition (permissions for the speech service, the
   * language tag, the network path to the vendor); a flat meter points at the
   * device, the operating system, or a muted tab.
   */
  const startMicrophoneTest = useCallback(async () => {
    if (sessionRef.current.status !== "idle" && sessionRef.current.status !== "error") {
      setMicrophoneTest({
        status: "error",
        level: 0,
        peak: 0,
        message: "Stop dictation before testing the microphone.",
      });
      return;
    }
    stopMicrophoneTest(micTestHandles.current);
    if (!secureContext) {
      setMicrophoneTest({ status: "error", level: 0, peak: 0, message: INSECURE_CONTEXT_MESSAGE });
      return;
    }
    if (!navigator?.mediaDevices?.getUserMedia) {
      setMicrophoneTest({
        status: "error",
        level: 0,
        peak: 0,
        message: "This browser cannot open a microphone.",
      });
      return;
    }
    setMicrophoneTest({
      status: "running",
      level: 0,
      peak: 0,
      message: "Listening for 2 seconds — say something.",
    });
    let media: MediaStream;
    try {
      media = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch (cause) {
      setMicrophoneTest({
        status: "error",
        level: 0,
        peak: 0,
        message: microphoneErrorMessage(cause),
      });
      return;
    }
    micTestHandles.current.stream = media;

    let analyser: AnalyserNode | null = null;
    let samples: Uint8Array<ArrayBuffer> | null = null;
    try {
      const Context = window.AudioContext ?? window.webkitAudioContext;
      if (Context) {
        const context = new Context();
        micTestHandles.current.context = context;
        analyser = context.createAnalyser();
        analyser.fftSize = 512;
        context.createMediaStreamSource(media).connect(analyser);
        samples = new Uint8Array(new ArrayBuffer(analyser.frequencyBinCount));
      }
    } catch {
      analyser = null;
    }

    let peak = 0;
    const started = Date.now();
    micTestHandles.current.timer = window.setInterval(() => {
      const level = analyser && samples ? readLevel(analyser, samples) : 0;
      peak = Math.max(peak, level);
      const elapsed = Date.now() - started;
      if (elapsed < MIC_TEST_DURATION_MS) {
        setMicrophoneTest((current) =>
          current.status === "running" ? { ...current, level, peak } : current,
        );
        return;
      }
      stopMicrophoneTest(micTestHandles.current);
      setMicrophoneTest({
        status: "done",
        level: 0,
        peak,
        message: analyser
          ? microphoneTestVerdict(peak)
          : "The microphone opened, but this browser would not measure its level.",
      });
    }, 60);
  }, [secureContext]);

  return {
    available: voiceInputAvailable(browserAvailable, serverAvailable),
    session,
    engine,
    preferredEngine,
    serverAvailable,
    browserAvailable,
    secureContext,
    language,
    languageLabel: describeVoiceLanguage(language, currentBrowserLocale()),
    languageTag: resolveVoiceLanguage(language, currentBrowserLocale()),
    active: session.status !== "idle" && session.status !== "error",
    interim: session.interim,
    restarts: dictation.restartCount,
    diagnostics,
    microphoneTest,
    runMicrophoneTest: () => void startMicrophoneTest(),
    toggle,
    stop,
    setLanguage,
    setPreferredEngine,
    dismiss,
  };
}

interface MicTestHandles {
  stream: MediaStream | null;
  context: AudioContext | null;
  timer: number | null;
}

function stopMicrophoneTest(handles: MicTestHandles): void {
  if (handles.timer !== null) {
    clearInterval(handles.timer);
    handles.timer = null;
  }
  handles.stream?.getTracks().forEach((track) => track.stop());
  handles.stream = null;
  void handles.context?.close().catch(() => {});
  handles.context = null;
}

/** Peak deviation from the 128 midpoint of the waveform, scaled into 0–1. */
function readLevel(analyser: AnalyserNode, samples: Uint8Array<ArrayBuffer>): number {
  analyser.getByteTimeDomainData(samples);
  let peak = 0;
  for (const sample of samples) peak = Math.max(peak, Math.abs(sample - 128));
  return clampLevel(peak / 96);
}

/** The first container this browser will actually record; opus preferred. */
function preferredRecorderMimeType(): string {
  const candidates = ["audio/webm;codecs=opus", "audio/webm", "audio/ogg;codecs=opus", "audio/mp4"];
  return candidates.find((type) => MediaRecorder.isTypeSupported?.(type)) ?? "";
}
