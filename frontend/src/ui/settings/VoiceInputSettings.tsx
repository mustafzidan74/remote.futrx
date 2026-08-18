import {
  TRANSCRIPTION_DEFAULT_LANGUAGE_OPTIONS,
  useTranscriptionSettings,
} from "../../state/hooks/settings/useTranscriptionSettings";
import { AlertCircle, Check, ExternalLink, Loader, Mic, X } from "../primitives/icons";

const inputClass =
  "w-full h-10 rounded-md bg-black/30 border border-white/10 px-3 text-sm text-ink-100 " +
  "placeholder:text-ink-400 focus:outline-none focus:border-accent-blue";

/**
 * Server-side speech-to-text: the optional half of voice input. The browser
 * path needs nothing configured here — this panel exists for Firefox, which
 * has no Web Speech API, and for users who want better Arabic than the
 * browser's own recognizer gives.
 */
export function VoiceInputSettings() {
  const editor = useTranscriptionSettings(true);
  const { settings, testResult } = editor;

  return (
    <section class="rounded-lg border border-white/10 bg-[#101318] overflow-hidden">
      <header class="px-4 py-3 flex items-start gap-3 border-b border-white/[0.06]">
        <div class="h-9 w-9 rounded-md bg-white/[0.06] border border-white/10 grid place-items-center flex-none">
          <Mic class="w-4 h-4 text-ink-200" />
        </div>
        <div class="flex-1 min-w-0">
          <div class="flex items-center gap-2">
            <div class="text-[14.5px] font-semibold text-ink-50">Server transcription</div>
            {editor.loading ? (
              <Loader class="w-3.5 h-3.5 text-ink-300 animate-spin" />
            ) : settings?.enabled ? (
              <span class="inline-flex items-center gap-1 text-[11px] text-accent-green">
                <Check class="w-3.5 h-3.5" /> on
              </span>
            ) : (
              <span class="text-[11px] text-ink-400">off</span>
            )}
          </div>
          <div class="text-[12px] text-ink-300 mt-1 leading-relaxed">
            The mic button in the chat composer works without any of this: Chrome, Edge, and Safari
            transcribe in the browser for free. Configure a provider here to give Firefox users
            dictation, and to offer everyone a "better Arabic" option that records the clip and
            transcribes it server-side.
          </div>
        </div>
      </header>

      <form onSubmit={editor.save} class="p-3 space-y-3">
        <label class="flex items-start gap-2.5 rounded-md border border-white/10 bg-white/[0.03] p-2.5 cursor-pointer">
          <input
            type="checkbox"
            checked={editor.enabled}
            onChange={(event) => editor.setEnabled((event.currentTarget as HTMLInputElement).checked)}
            class="mt-0.5 h-4 w-4 flex-none accent-accent-blue"
          />
          <span class="min-w-0">
            <span class="block text-[13px] text-ink-100">Enable server transcription</span>
            <span class="block text-[12px] text-ink-300 leading-relaxed">
              Add an API key below before turning this on. Recordings are limited to 5 minutes and
              25 MB, and are streamed straight to the provider — never stored on this server.
            </span>
          </span>
        </label>

        <fieldset class="space-y-3">
          <legend class="text-xs font-semibold text-ink-200 uppercase tracking-wide">Provider</legend>

          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">Provider</span>
            <select
              value={settings?.provider || "openai"}
              disabled
              class={`${inputClass} disabled:opacity-70`}
            >
              <option value="openai">OpenAI</option>
            </select>
            <span class="block text-[11.5px] text-ink-400 leading-relaxed">
              OpenAI is the only provider implemented today. The field is stored so another can be
              added without re-entering these settings.
            </span>
          </label>

          <div class="rounded-md border border-white/10 bg-white/[0.03] p-2.5 text-[12px] text-ink-300 leading-relaxed">
            Create a key at{" "}
            <code class="rounded bg-black/30 border border-white/10 px-1 py-0.5 text-[11.5px] break-all">
              platform.openai.com/api-keys
            </code>
            . It is stored at{" "}
            <code class="rounded bg-black/30 border border-white/10 px-1 py-0.5 text-[11.5px]">
              DATA_DIR/transcription.json
            </code>{" "}
            with mode 0600 and is never returned to this page.
            <a
              href="https://platform.openai.com/docs/api-reference/audio/createTranscription"
              target="_blank"
              rel="noreferrer"
              class="inline-flex items-center gap-1 mt-2 text-accent-blue hover:underline"
            >
              Transcription API documentation <ExternalLink class="w-3.5 h-3.5" />
            </a>
          </div>

          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">API key</span>
            <input
              type="password"
              value={editor.apiKey}
              onInput={(event) => editor.setApiKey((event.currentTarget as HTMLInputElement).value)}
              placeholder={settings?.configured ? settings.apiKeyMasked : "sk-proj-…"}
              autocomplete="off"
              spellcheck={false}
              class={inputClass}
            />
            <span class="flex flex-wrap items-center gap-2 text-[11.5px] text-ink-400">
              {settings?.configured ? (
                <>
                  <span>A key is stored. Leave this blank to keep it.</span>
                  <button
                    type="button"
                    onClick={editor.clearApiKey}
                    disabled={editor.saving}
                    class="inline-flex items-center gap-1 rounded border border-white/10 px-1.5 py-0.5
                           text-ink-200 hover:bg-white/[0.07] disabled:opacity-50"
                  >
                    <X class="w-3 h-3" /> Remove stored key
                  </button>
                </>
              ) : (
                <span>No key stored yet.</span>
              )}
            </span>
          </label>

          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">Model</span>
            <select
              value={editor.model}
              onChange={(event) => editor.setModel((event.currentTarget as HTMLSelectElement).value)}
              class={inputClass}
            >
              {(editor.models.length ? editor.models : [editor.model]).filter(Boolean).map((name) => (
                <option key={name} value={name}>
                  {name}
                </option>
              ))}
            </select>
            <span class="block text-[11.5px] text-ink-400 leading-relaxed">
              <code class="text-ink-300">gpt-4o-mini-transcribe</code> is the cheapest and fastest.
              <code class="text-ink-300"> whisper-1</code> is the original endpoint.
            </span>
          </label>

          <label class="block space-y-1.5">
            <span class="text-xs text-ink-300">Default language</span>
            <select
              value={editor.defaultLanguage}
              onChange={(event) =>
                editor.setDefaultLanguage((event.currentTarget as HTMLSelectElement).value)
              }
              class={inputClass}
            >
              {TRANSCRIPTION_DEFAULT_LANGUAGE_OPTIONS.map((option) => (
                <option key={option.value || "auto"} value={option.value}>
                  {option.label}
                </option>
              ))}
            </select>
            <span class="block text-[11.5px] text-ink-400 leading-relaxed">
              A hint for clips whose speaker did not pick a language themselves. Users override it
              from the mic button in any composer.
            </span>
          </label>
        </fieldset>

        {editor.error && (
          <div
            role="alert"
            class="flex items-start gap-2 rounded-md border border-accent-red/30 bg-accent-red/[0.08] px-2.5 py-2"
          >
            <AlertCircle class="mt-0.5 h-3.5 w-3.5 flex-none text-accent-red" aria-hidden="true" />
            <span class="min-w-0 flex-1 break-words text-[12px] leading-relaxed text-ink-100">
              {editor.error}
            </span>
          </div>
        )}

        {testResult && (
          <div
            class={`rounded-md border px-2.5 py-2 text-[12px] leading-relaxed ${
              testResult.ok
                ? "border-accent-green/30 bg-accent-green/[0.08] text-ink-100"
                : "border-accent-red/30 bg-accent-red/[0.08] text-ink-100"
            }`}
            role="status"
          >
            {testResult.ok ? (
              <>
                Round trip to <span class="text-ink-50">{testResult.model}</span> succeeded in{" "}
                <span class="text-ink-50">{testResult.durationMs} ms</span>. The sample is one
                second of silence, so an empty transcript is the expected answer.
              </>
            ) : (
              <>Test failed: {testResult.error}</>
            )}
          </div>
        )}

        <div class="flex flex-wrap items-center gap-2 pt-1">
          <button
            type="submit"
            disabled={editor.saving || editor.loading}
            class="h-9 rounded-md bg-accent-blue px-3 text-[13px] font-semibold text-ink-900
                   hover:bg-accent-blue/85 disabled:opacity-50 disabled:cursor-not-allowed transition"
          >
            {editor.saving ? "Saving…" : "Save"}
          </button>
          <button
            type="button"
            onClick={editor.runTest}
            disabled={editor.testing || !settings?.configured}
            title={
              settings?.configured
                ? "Transcribe a one-second silent sample and report the round trip"
                : "Save an API key first"
            }
            class="inline-flex h-9 items-center gap-1.5 rounded-md border border-white/10 px-3 text-[13px]
                   font-medium text-ink-100 hover:bg-white/[0.07] disabled:opacity-50 disabled:cursor-not-allowed transition"
          >
            {editor.testing ? <Loader class="h-3.5 w-3.5 animate-spin" /> : <Mic class="h-3.5 w-3.5" />}
            {editor.testing ? "Testing…" : "Test"}
          </button>
          {editor.saved && (
            <span class="inline-flex items-center gap-1 text-[12px] text-accent-green">
              <Check class="h-3.5 w-3.5" /> Saved
            </span>
          )}
        </div>
      </form>
    </section>
  );
}
