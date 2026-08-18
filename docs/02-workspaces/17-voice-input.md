# Voice input

The chat composer has a microphone button. Press it, speak, press it again,
read what landed in the textarea, and press Send. Dictation never sends a
prompt by itself — an agent turn is too consequential to fire on a silence
timeout, so the transcript is always a draft the human reviews.

Arabic is a first-class target: Egyptian Arabic is the default language on any
browser whose locale starts with `ar`, and the optional server fallback exists
largely because hosted models transcribe Arabic considerably better than the
browsers do.

## Two engines behind one button

| | Browser (Web Speech API) | Server (OpenAI transcription) |
| --- | --- | --- |
| Cost to the operator | Free | Per minute of audio, billed by the provider |
| Setup | None | Admin configures a provider and API key |
| Feedback while speaking | Words stream into the composer live | Elapsed timer only; text arrives after you stop |
| Where the audio goes | The browser vendor's speech service | This server, streamed straight through to the provider |
| Arabic quality | Fair | Better |
| Availability | Chrome, Edge, Safari | Any browser with `MediaRecorder` |

The browser engine is the default because it is free and streams. The server
engine is what Firefox users get automatically, and what anyone can opt into
from the mic button's menu when it is configured.

### Browser support matrix

| Browser | Web Speech API | Server transcription | Result |
| --- | --- | --- | --- |
| Chrome (desktop, Android) | Yes | Yes | Both; browser by default |
| Edge | Yes | Yes | Both; browser by default |
| Safari (macOS, iOS) | Yes, `webkit`-prefixed | Yes | Both; browser by default |
| Firefox | **No** | Yes | Server only — the button appears only once an admin configures it |
| Anything else | Detected at runtime | Detected at runtime | See below |

The mic button is **hidden entirely** when neither engine is available: no Web
Speech API *and* no server transcription configured. A disabled button the user
cannot explain is worse than no button.

### Privacy note

Neither engine is local.

- **Web Speech API**: Chrome and Edge stream the microphone audio to Google's
  and Microsoft's speech services respectively; Safari uses Apple's. This
  happens entirely between the browser and its vendor — this server never sees
  the audio and cannot log, block, or audit it. Anyone whose threat model
  excludes those vendors should not use the browser engine.
- **Server transcription**: the clip is uploaded here and streamed straight
  through to the configured provider. It is never written to disk, never
  logged, and never persisted in any form. What is recorded is one audit entry
  with the clip's duration — not the audio, and not the transcript.

Both paths require HTTPS. Browsers refuse microphone access on plain HTTP
outside `localhost`, so voice input only works over the platform's normal
Caddy-terminated origin.

## Language selection

The mic button's menu offers five choices, remembered per device in
`localStorage` (`remote.futrx.voiceLanguage.v1`):

| Value | Label |
| --- | --- |
| `ar-EG` | العربية (مصر) |
| `ar-SA` | العربية (السعودية) |
| `en-US` | English (US) |
| `en-GB` | English (UK) |
| `auto` | Auto (browser language) |

A user who has never chosen gets `ar-EG` when `navigator.language` starts with
`ar`, and `en-US` otherwise. Neither default is `auto`: `auto` hands the
recognizer whatever `navigator.language` happens to be, which the user cannot
see and the vendor's speech service may not support — and a language the
service will not recognise is one of the ways dictation "just does nothing".
`auto` remains selectable for anyone who wants it.

Whichever language is selected is named in the microphone button's tooltip and
in its accessible label, listening or idle. "I spoke and nothing appeared" is
very often "it was listening for the other language", and that is invisible
unless it is written where the user is already looking.

The choice is per device rather than per account on purpose: which language you
speak into a microphone is a property of where you are sitting, not of who you
are, and the same account on a shared machine should not drag the setting
around.

The selected tag is also written to the composer textarea's `lang` attribute,
which tells the browser how to shape and spell-check the inserted text.

### Language and the provider

The Web Speech API takes a full BCP-47 tag (`ar-EG`). The transcription API
takes an ISO-639-1 subtag, so the backend reduces the tag before forwarding it
(`ar-EG` → `ar`). A tag it cannot reduce — including the `auto` sentinel — is
omitted, and the provider detects the language itself.

## Right-to-left behaviour

The composer textarea keeps `dir="auto"`, so its base direction follows its
content: dictate Arabic into an empty composer and the whole field flips to
RTL, exactly as typing Arabic would.

Caret handling is direction-agnostic by construction. The state machine tracks
the caret as a **logical string offset**, not as anything visual, and
`setSelectionRange` takes logical offsets too. A session records the text on
either side of the caret when it opens and only ever rewrites the span between
them, so the caret lands after the same character whichever way the glyphs run.
Mixing scripts (an English draft with Arabic dictated into the middle of it)
keeps the base direction of the first strong character and renders the inserted
run bidirectionally, which is the browser's normal Unicode bidi behaviour.

## The dictation state machine

`frontend/src/state/chat/voiceInputState.ts` is a pure module — no browser APIs
— so the merge behaviour is pinned by tests (`voiceInputState.test.ts`). The
Web Speech *lifecycle* is a second pure module,
`frontend/src/state/chat/voiceDictationController.ts`, whose recognizer
factory, clock, timers, and two output callbacks are all injected; its tests
(`voiceDictationController.test.ts`) play out whole sessions — interim results,
a stop that has to flush, a browser that ends the session by itself — without a
DOM.

```
idle ──click──▶ starting ──▶ listening ──stop/Esc/send──▶ idle
                    │                    (browser)
                    └──▶ recording ──stop──▶ transcribing ──▶ idle
                                             (server)
              any state ──failure──▶ error ──dismiss──▶ idle
```

The interesting part is the interim/final merge. The Web Speech API does not
append; it **rewrites** its hypothesis on every `result` event until a span
firms up. So the session holds two strings:

- `final` — everything that has firmed up this session, accumulated.
- `interim` — the current hypothesis, replaced wholesale on every event.

The composer shows `before + final + interim + after`. Appending interim text
instead of replacing it would accumulate every draft of the same sentence.

### Stopping

A session ends on a second click of the microphone, on **Escape**, or on
**Send**.

Escape means two things in a chat — stop dictating, and cancel the running
agent turn — and both are `window` listeners owned by hooks that cannot see
each other. Dictation does **not** swallow the key (that would also rob every
modal in the app of its Escape); instead it registers a claim, and the cancel
shortcut stands down while any microphone is live. One Escape stops the
microphone, the next cancels the run.

Send is not instantaneous either. The last words reach the composer through a
state update, and the server engine has not even uploaded its clip when Send is
pressed — so pressing Send while dictating stops the session, waits for it to
settle, and only then sends. Both the Send button and the Ctrl/Cmd+Enter
shortcut route through that path; sending straight from the shortcut would post
the draft as it stood one hypothesis ago. A session that ended on an error
sends nothing: the banner is there to be read first.

### Session ownership

`getUserMedia` behind a permission prompt, a `MediaRecorder` flush, and an
upload round trip all resolve long after the user may have pressed stop,
switched chats, or closed the composer. Every session therefore carries a
generation number, and every asynchronous continuation checks that it still
owns the session before touching hardware or text. A continuation that lost
ownership hands the microphone back and drops its result — which is what stops
a cancelled recording from being uploaded minutes later, and stops a late
transcript from overwriting whatever was typed in the meantime.

Two deliberate choices:

- **Stopping keeps an unfirmed hypothesis.** The user watched those words
  appear; making them vanish on stop reads as lost dictation, and the composer
  is a reviewable draft anyway.
- **A failure keeps what was already dictated.** A connection that drops
  halfway through should not also erase the first half.

## When dictation produces nothing

This section exists because it happened: an operator on Chrome/Windows pressed
the microphone, spoke, pressed stop, and got an unchanged composer and no
error at all. Everything below is the machinery that makes that outcome
impossible to reach silently.

### Never open a second capture while the recognizer is running

**This was the bug.** The browser engine's level meter used to call
`navigator.mediaDevices.getUserMedia({ audio: true })` from the recognizer's
`start` handler, purely to drive a bar. Chrome's `SpeechRecognition` captures
from the same input device, and opening a second capture beside it
re-initialises that device underneath the recognizer: it ends within a second
or two, reporting `aborted` (sometimes `no-speech`) having heard nothing. Both
of those codes were swallowed without a banner, and the resulting `end` was
treated as a normal finish — so the composer was rewritten with exactly the
draft it already had.

The browser engine therefore has **no level meter**. The level readout belongs
to the server engine, which owns the stream it is recording anyway, and to the
microphone self-test, which never runs at the same time as recognition (the
menu entry is disabled while a session is live, and the hook refuses it too).

### The live strip

While any microphone is open, a strip above the composer shows the status, the
language being recognised, the restart count, and the current hypothesis. It is
driven by the dictation session rather than by the draft, deliberately: words
in the strip but not in the textarea would mean the composer wiring is broken,
while an empty strip beside a running timer means the microphone is silent.
The two failures look identical when the textarea is the only feedback.

### Every error code has a sentence

No code is swallowed. `no-speech` in particular — the microphone was open and
only silence arrived — is the single most useful thing the API says, and it is
now shown rather than hidden.

| Code | What it means | What to do |
| --- | --- | --- |
| `not-allowed` | The site's microphone permission is denied | Padlock menu → allow the microphone → reload |
| `service-not-allowed` | The browser refused its speech service for this page | Check the site permission and any managed-browser policy |
| `audio-capture` | No usable input device | Select an input in the browser's site settings |
| `network` | The vendor's speech service was unreachable | Chrome uploads the audio to Google; a blocked or offline network stops dictation. Use server transcription where that traffic is not allowed |
| `language-not-supported` | The service does not recognise the selected tag | Pick another language from the menu |
| `no-speech` | Open microphone, silence only | Run the microphone test; check the input device and mute state |
| `aborted` | The session was interrupted | Usually the user, a navigation, or another capture |

Anything not on this list is still reported, with its code in the message.

### The conditions that used to be invisible

| Condition | Behaviour now |
| --- | --- |
| Page is not a secure context | Refused before anything opens, saying so. Browsers deny microphones on plain HTTP outside `localhost` |
| `start()` throws `InvalidStateError` ("already started") | The stuck recognizer is aborted and one retry is scheduled; a second failure asks the user to reload the tab |
| `end` fires immediately having heard nothing (muted tab, no usable input) | One retry, then a banner naming the mute/other-app/OS-privacy causes and pointing at the microphone test |
| Chrome ends continuous recognition after ~60 s of silence | The session restarts underneath the user, up to 20 times, until they press stop. The strip shows the restart count |
| Stop pressed with a hypothesis still in flight | `stop()` is called, **not** `abort()`, and results keep being folded in until `end` arrives. `abort()` discards whatever the recognizer has not yet dispatched. A 1.5 s watchdog ends the session anyway if `end` never comes |

### Test microphone

**Test microphone (2s)** in the microphone menu records two seconds and reports
the peak input level. It answers the one question the composer cannot: did the
operating system hand the browser any audio at all?

- A healthy level with nothing in the composer points at *recognition* — the
  language tag, the site's permission for the speech service, or the network
  path to the vendor.
- A flat level points at the *device* — the wrong input selected, a muted
  microphone, another application holding it, or OS privacy settings.

The menu also keeps the last ten lifecycle lines under **What happened last
time**: which language started, which error code arrived, whether the browser
restarted the session. That is what to ask an operator for when a report says
"the microphone does nothing".

## Server transcription

### Admin configuration

**Settings → Agents & skills → Voice input** (admin only).

| Field | Meaning |
| --- | --- |
| Enable server transcription | Master switch. Refuses to turn on without a key. |
| Provider | `openai` — the only one implemented. Stored so another can be added without re-entering settings. |
| API key | Write-only. Stored server-side, returned only as `••••` plus the last four characters. |
| Model | `gpt-4o-mini-transcribe` (default), `gpt-4o-transcribe`, or `whisper-1`. |
| Default language | Hint for clips whose speaker did not pick one. Users override it per device. |
| Test | Transcribes a one-second silent WAV and reports the round-trip time. |

The **Test** button proves the key, model, and network path work without anyone
speaking. Silence normally comes back as empty text — what is being checked is
the round trip, not the words.

### Storage

| Path | Contents |
| --- | --- |
| `DATA_DIR/transcription.json` | The whole configuration, mode 0600, temp-file + rename |

```json
{
  "enabled": true,
  "provider": "openai",
  "apiKey": "sk-proj-…",
  "model": "gpt-4o-mini-transcribe",
  "defaultLanguage": "ar-EG",
  "updatedAt": 1755500000000
}
```

Like every other secret under `DATA_DIR`, the key is stored in plaintext at
mode 0600 — see the [threat model](../threat-model.md) for what that implies.

### Endpoints

| Method | Path | Who | Purpose |
| --- | --- | --- | --- |
| `POST` | `/api/transcribe` | Any signed-in user | Upload one clip, get its text |
| `GET` | `/api/transcribe/config` | Any signed-in user | Is the fallback available, and what are the limits |
| `GET`/`PUT` | `/api/admin/transcription` | Admin | Read (masked) / write settings |
| `POST` | `/api/admin/transcription/test` | Admin | Silent-sample round trip |

`POST /api/transcribe` is `multipart/form-data`:

| Field | Meaning |
| --- | --- |
| `audio` | The recording. `audio/webm;codecs=opus` from the browser. |
| `language` | BCP-47 tag the user picked, or `auto`. |
| `durationMs` | What the browser measured. Advisory. |
| `chatId` | Optional, for the audit entry's target. |

`GET /api/transcribe/config` deliberately carries no provider identity and no
key material — only `enabled`, `defaultLanguage`, `maxBytes`, and `maxSeconds`.
The composer does not need to know who transcribes, only whether it can offer
the option.

### Limits and guards

| Guard | Value | Enforced by |
| --- | --- | --- |
| Upload size | 25 MB | `http.MaxBytesReader` — the hard defence |
| Clip duration | 5 minutes | Service, from the browser-reported `durationMs`; the browser also stops its own recorder at the ceiling |
| Provider round trip | 60 s | `context.WithTimeout` around the request |
| Rate limit | 30 clips per user per minute | In-process sliding window, keyed by the caller's email |

The byte ceiling is the real defence; the duration is advisory, because the
container format's true length cannot be trusted from an upload without
decoding it. A refused rate-limit attempt is not itself counted, so a client
that backs off recovers as soon as the oldest hit ages out.

Failures map to status codes the composer can explain: `503` not configured,
`429` rate limited, `400` too long or no audio, `413` over the size ceiling,
`502` the provider or network failed.

The first four say plainly what went wrong, because they are the caller's own
doing. A `502` deliberately does not: the provider's own error prose names the
vendor and, on an OpenAI `401`, quotes a fragment of the operator's API key —
neither of which an ordinary member should learn from a failed dictation. Those
are logged for the operator and flattened for the user, who gets the detail
only through the admin **Test** probe.

### Streaming, not buffering

The handler walks the multipart body with `r.MultipartReader()` rather than
`ParseMultipartForm`, because that helper spools any part over its memory
budget into a temp file — a recording of someone's voice must not land on this
server's disk even briefly. The still-uploading audio part is handed to the
service, which builds its own multipart body through an `io.Pipe` into the
provider request, so a 25 MB clip is never held in memory twice and never
reaches disk.

That buys an ordering contract: the text fields have to arrive **before** the
audio part, because once the audio is streaming the reader has passed
everything ahead of it. The client appends them in that order; a request that
does not is still transcribed, just without the hints.

An upload whose declared length is over the cap is refused before a byte is
read. `http.MaxBytesReader` is the backstop for a client that lies or sends no
length, and that mid-copy failure is recognized and reported as `413` too
rather than as a provider outage.

## Audit

Every server transcription writes one `chat.transcribe` entry whose metadata is
the clip duration and nothing else:

```json
{
  "action": "chat.transcribe",
  "target": { "type": "chat", "id": "…" },
  "meta": { "durationMs": 7500 },
  "ok": true
}
```

Saving the settings writes `settings.transcription.configure`, recording the
enabled flag and the model — never the key.

The browser engine produces no audit entries at all, because the server is not
involved in it.

## Where the code lives

| Layer | Path |
| --- | --- |
| State machine (pure) | `frontend/src/state/chat/voiceInputState.ts` |
| Web Speech lifecycle (pure) | `frontend/src/state/chat/voiceDictationController.ts` |
| Recorder + self-test driver | `frontend/src/state/hooks/chat/useVoiceInput.ts` |
| Web Speech typings | `frontend/src/types/speech.ts` |
| Mic button and menu | `frontend/src/ui/chat/composer/VoiceInputButton.tsx` |
| Live strip above the composer | `frontend/src/ui/chat/composer/VoiceLiveStrip.tsx` |
| Admin panel | `frontend/src/ui/settings/VoiceInputSettings.tsx` |
| Service | `backend/internal/service/transcribe/` |
| Store | `backend/internal/stores/filetranscribe/` |
| Handler | `backend/internal/transport/http/handlers/transcribe_handler.go` |

## Known gaps

- **No auto-send after silence.** Deliberately out of scope; the user reviews
  and sends.
- **Duration is client-reported.** The server does not decode the container to
  verify it. The byte cap bounds the real cost.
- **The rate limiter is in-process.** It resets when the backend restarts,
  which is consistent with the platform's single-process design.
- **Only OpenAI.** The `provider` field is stored so a second backend can be
  added without a migration, but nothing else is implemented.
