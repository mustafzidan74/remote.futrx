import type { RefObject } from "preact";
import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";
import { transcriptionApi } from "../../../api/transcriptionApi";
import {
  currentBrowserLocale,
  resolveVoiceLanguage,
  type VoiceEngine,
  type VoiceLanguage,
} from "../../../config/voice";
import type { TranscriptionClientConfig } from "../../../models/transcription";
import {
  mediaRecorderSupported,
  speechRecognitionConstructor,
  speechRecognitionSupported,
  type SpeechRecognitionErrorEvent,
  type SpeechRecognitionEvent,
  type SpeechRecognitionLike,
} from "../../../types/speech";
import {
  IDLE_VOICE_SESSION,
  applyRecognition,
  applyTranscript,
  beginSession,
  composeText,
  dismissError,
  failSession,
  finishSession,
  markRunning,
  markTranscribing,
  resolveEngine,
  voiceInputAvailable,
  voicePreferenceStore,
  withElapsed,
  withLevel,
  type VoiceSession,
} from "../../chat/voiceInputState";

/** How often the level meter and the recording timer are refreshed. */
const METER_INTERVAL_MS = 100;

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
  language: VoiceLanguage;
  /** BCP-47 tag for the textarea's `lang`, or "" when the choice is "auto". */
  languageTag: string;
  active: boolean;
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

  const browserAvailable = useMemo(() => speechRecognitionSupported(), []);
  const serverAvailable = !!serverConfig?.enabled && mediaRecorderSupported();
  const engine = resolveEngine(preferredEngine, browserAvailable, serverAvailable);

  // Live handles for the running session. They are refs because the recognizer
  // callbacks outlive the render that installed them.
  const recognition = useRef<SpeechRecognitionLike | null>(null);
  const recorder = useRef<MediaRecorder | null>(null);
  const stream = useRef<MediaStream | null>(null);
  const audioContext = useRef<AudioContext | null>(null);
  const meterTimer = useRef<number | null>(null);
  const startedAt = useRef(0);
  // The composer text as it was when the session opened. Reading it from a ref
  // keeps the recognizer callbacks off the render loop.
  const sessionRef = useRef<VoiceSession>(IDLE_VOICE_SESSION);
  const changeText = useRef(onTextChange);
  changeText.current = onTextChange;

  /** Applies a transition and mirrors the resulting text into the composer. */
  const commit = useCallback(
    (next: (current: VoiceSession) => VoiceSession, { writeText = true } = {}) => {
      const updated = next(sessionRef.current);
      sessionRef.current = updated;
      setSession(updated);
      if (!writeText || updated.status === "starting") return;
      const composed = composeText(updated);
      changeText.current(composed.text);
      // The caret is a logical offset, so this is correct for Arabic too: the
      // textarea keeps `dir="auto"` and places the caret after the same
      // character regardless of which way the glyphs run.
      const textarea = textareaRef.current;
      if (textarea) {
        requestAnimationFrame(() => {
          textarea.setSelectionRange(composed.caret, composed.caret);
        });
      }
    },
    [textareaRef],
  );

  /** Tears down every live handle. Safe to call more than once. */
  const releaseHardware = useCallback(() => {
    if (meterTimer.current !== null) {
      clearInterval(meterTimer.current);
      meterTimer.current = null;
    }
    recognition.current?.abort?.();
    recognition.current = null;
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

  useEffect(() => releaseHardware, [releaseHardware]);

  /**
   * Starts the level meter and the timer. The meter is best effort: a browser
   * that refuses an AudioContext still gets a working recorder, just without
   * the bar.
   */
  const startMeter = useCallback(
    (source: MediaStream) => {
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
        const elapsed = Date.now() - startedAt.current;
        let level = 0;
        if (analyser && samples) {
          analyser.getByteTimeDomainData(samples);
          // Peak deviation from the 128 midpoint, scaled into 0–1.
          let peak = 0;
          for (const sample of samples) peak = Math.max(peak, Math.abs(sample - 128));
          level = Math.min(1, peak / 96);
        }
        commit((current) => withLevel(withElapsed(current, elapsed), level), {
          writeText: false,
        });
      }, METER_INTERVAL_MS);
    },
    [commit],
  );

  const stop = useCallback(() => {
    const current = sessionRef.current;
    if (current.status === "idle" || current.status === "error") return;
    // The server engine finishes in the recorder's stop handler, which needs
    // the stream alive long enough to flush its last chunk.
    if (current.status === "recording" && recorder.current?.state === "recording") {
      recorder.current.stop();
      return;
    }
    releaseHardware();
    commit(finishSession);
  }, [commit, releaseHardware]);

  const startBrowser = useCallback(() => {
    const Recognition = speechRecognitionConstructor();
    if (!Recognition) {
      commit((current) => failSession(current, "This browser has no speech recognition."));
      return;
    }
    const engineInstance = new Recognition();
    engineInstance.continuous = true;
    engineInstance.interimResults = true;
    engineInstance.maxAlternatives = 1;
    const tag = resolveVoiceLanguage(language, currentBrowserLocale());
    if (tag) engineInstance.lang = tag;

    engineInstance.onstart = () => commit((current) => markRunning(current, "listening"));
    engineInstance.onresult = (event: SpeechRecognitionEvent) => {
      // Only the results from resultIndex onward are new; everything before it
      // has already been folded in.
      let finalChunk = "";
      let interim = "";
      for (let index = event.resultIndex; index < event.results.length; index += 1) {
        const result = event.results[index];
        const transcript = result[0]?.transcript ?? "";
        if (result.isFinal) finalChunk += transcript;
        else interim += transcript;
      }
      commit((current) => applyRecognition(current, { finalChunk, interim }));
    };
    engineInstance.onerror = (event: SpeechRecognitionErrorEvent) => {
      // "no-speech" and "aborted" are how a silent or user-stopped session
      // ends, not failures worth a banner.
      if (event.error === "aborted" || event.error === "no-speech") return;
      releaseHardware();
      commit((current) => failSession(current, recognitionErrorMessage(event.error)));
    };
    engineInstance.onend = () => {
      recognition.current = null;
      if (sessionRef.current.status === "listening") {
        releaseHardware();
        commit(finishSession);
      }
    };

    recognition.current = engineInstance;
    try {
      engineInstance.start();
    } catch {
      commit((current) => failSession(current, "Voice input could not start."));
    }
  }, [commit, language, releaseHardware]);

  const startServer = useCallback(async () => {
    const maxSeconds = serverConfig?.maxSeconds ?? 300;
    let media: MediaStream;
    try {
      media = await navigator.mediaDevices.getUserMedia({ audio: true });
    } catch (cause) {
      commit((current) => failSession(current, microphoneErrorMessage(cause)));
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
      releaseHardware();
      const blob = new Blob(chunks, { type: instance.mimeType || "audio/webm" });
      if (blob.size === 0) {
        commit(finishSession);
        return;
      }
      commit(markTranscribing, { writeText: false });
      transcriptionApi
        .transcribe({ audio: blob, language, durationMs, chatId })
        .then((result) => commit((current) => applyTranscript(current, result.text)))
        .catch((cause) => commit((current) => failSession(current, (cause as Error).message)));
    };

    recorder.current = instance;
    instance.start();
    startMeter(media);
    commit((current) => markRunning(current, "recording"));

    // The server refuses anything past its ceiling, so stop before the upload
    // is wasted rather than after.
    window.setTimeout(() => {
      if (recorder.current === instance && instance.state === "recording") instance.stop();
    }, maxSeconds * 1000);
  }, [chatId, commit, language, releaseHardware, serverConfig?.maxSeconds, startMeter]);

  const toggle = useCallback(() => {
    if (disabled) return;
    if (sessionRef.current.status !== "idle" && sessionRef.current.status !== "error") {
      stop();
      return;
    }
    if (!engine) return;
    const textarea = textareaRef.current;
    const caret = textarea?.selectionStart ?? text.length;
    commit(() => beginSession(text, caret), { writeText: false });
    if (engine === "browser") startBrowser();
    else void startServer();
  }, [commit, disabled, engine, startBrowser, startServer, stop, text, textareaRef]);

  // Escape stops dictation, matching how every other transient composer state
  // is dismissed.
  //
  // The chat's own Escape shortcut cancels a running agent turn, and both
  // listeners sit on `window`. Dictating a queued prompt while the agent works
  // is an ordinary thing to do, and killing that run because the user wanted
  // the microphone off would be a nasty surprise — so this claims Escape in
  // the capture phase and stops it there. Bubble-phase order between two
  // independent hooks is not something to bet on. A second Escape, with the
  // microphone now idle, reaches the cancel shortcut as usual.
  useEffect(() => {
    if (session.status === "idle" || session.status === "error") return;
    function onKeyDown(event: KeyboardEvent) {
      if (event.key !== "Escape") return;
      event.preventDefault();
      event.stopImmediatePropagation();
      stop();
    }
    window.addEventListener("keydown", onKeyDown, true);
    return () => window.removeEventListener("keydown", onKeyDown, true);
  }, [session.status, stop]);

  const setLanguage = useCallback((next: VoiceLanguage) => {
    voicePreferenceStore.setLanguage(next);
    setLanguageState(next);
  }, []);

  const setPreferredEngine = useCallback((next: VoiceEngine) => {
    voicePreferenceStore.setEngine(next);
    setPreferredEngineState(next);
  }, []);

  return {
    available: voiceInputAvailable(browserAvailable, serverAvailable),
    session,
    engine,
    preferredEngine,
    serverAvailable,
    browserAvailable,
    language,
    languageTag: resolveVoiceLanguage(language, currentBrowserLocale()),
    active: session.status !== "idle" && session.status !== "error",
    toggle,
    stop,
    setLanguage,
    setPreferredEngine,
    dismiss: () => commit(dismissError, { writeText: false }),
  };
}

/** The first container this browser will actually record; opus preferred. */
function preferredRecorderMimeType(): string {
  const candidates = ["audio/webm;codecs=opus", "audio/webm", "audio/ogg;codecs=opus", "audio/mp4"];
  return candidates.find((type) => MediaRecorder.isTypeSupported?.(type)) ?? "";
}

function recognitionErrorMessage(code: string): string {
  switch (code) {
    case "not-allowed":
    case "service-not-allowed":
      return "Microphone access was denied. Allow it in your browser's site settings.";
    case "audio-capture":
      return "No microphone was found.";
    case "network":
      return "Speech recognition could not reach the browser's speech service.";
    case "language-not-supported":
      return "This browser cannot recognise the selected language.";
    default:
      return `Voice input failed (${code}).`;
  }
}

function microphoneErrorMessage(cause: unknown): string {
  const name = (cause as { name?: string } | null)?.name ?? "";
  if (name === "NotAllowedError" || name === "SecurityError") {
    return "Microphone access was denied. Allow it in your browser's site settings.";
  }
  if (name === "NotFoundError" || name === "DevicesNotFoundError") {
    return "No microphone was found.";
  }
  return "The microphone could not be opened.";
}
