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
`ar`, and `auto` otherwise. `auto` resolves to the browser's own language,
which is what the Web Speech API falls back to when `lang` is unset.

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
— so the merge behaviour is pinned by tests
(`voiceInputState.test.ts`).

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
| Browser + recorder driver | `frontend/src/state/hooks/chat/useVoiceInput.ts` |
| Web Speech typings | `frontend/src/types/speech.ts` |
| Mic button | `frontend/src/ui/chat/composer/VoiceInputButton.tsx` |
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
