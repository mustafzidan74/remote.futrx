import type { JSX } from "preact";
import { useState } from "preact/hooks";
import type {
  ProviderKind,
  ProviderTestResult,
  ProviderView,
  UsageMeter,
} from "../../models/aiProviders";
import {
  useAiProviders,
  type AiProvidersEditor,
} from "../../state/hooks/settings/useAiProviders";
import {
  PROVIDER_KIND_LABELS,
  PROVIDER_LIMIT_FIELDS,
  PROVIDER_STATUS_LABELS,
  PROVIDER_STATUS_TONE,
  emptyProviderForm,
  formProviderFrom,
  formatLatency,
  meterLabel,
  meterPercent,
  meterSourceLabel,
  meterTone,
  modelsSummary,
  providerFormProblem,
  type ProviderForm,
  type ProviderLimitsForm,
} from "../../state/settings/aiProvidersState";
import type { StatusTone } from "../../state/home/dashboardState";
import { EmptyState, ErrorBanner } from "../primitives/Feedback";
import {
  AlertCircle,
  ArrowDown,
  ArrowUp,
  Check,
  Key,
  Loader,
  Network,
  Plus,
  Trash,
  Zap,
} from "../primitives/icons";

const inputClass =
  "h-9 w-full rounded-md border border-white/10 bg-black/30 px-2.5 text-[13px] text-ink-50 " +
  "placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none disabled:opacity-60";

/** The dot beside a provider's name, in the board's shared tones. */
const TONE_DOT: Record<StatusTone, string> = {
  green: "bg-accent-green",
  amber: "bg-accent-yellow",
  red: "bg-accent-red",
  grey: "bg-ink-400",
};

/** The bar's fill, painted through `currentColor` so one class does both. */
const TONE_TEXT: Record<StatusTone, string> = {
  green: "text-accent-blue",
  amber: "text-accent-orange",
  red: "text-accent-red",
  grey: "text-ink-400",
};

/**
 * The free-tier provider pool.
 *
 * The panel makes one promise and one disclaimer. The promise: connect several
 * free tiers, and the platform's own small jobs move to the next one when a
 * quota runs out, without anybody watching. The disclaimer, which is the whole
 * reason the meters are here: the limits shown are what the vendor documented
 * when the seed was written, they change without notice, and a number a
 * provider reports about itself always beats one we counted. Every bar
 * therefore says where it came from, and a window with no documented cap draws
 * an empty track and a raw count rather than a percentage nobody published.
 */
export function AiProvidersSettings() {
  const editor = useAiProviders(true);
  const [draft, setDraft] = useState<ProviderForm | null>(null);
  const { view } = editor;
  const providers = view?.providers ?? [];

  return (
    <div class="space-y-4">
      <section class="rounded-lg border border-white/10 bg-[#101318] p-4">
        <div class="flex items-start gap-2">
          <Network class="mt-0.5 h-4 w-4 flex-none text-accent-blue" aria-hidden="true" />
          <div class="min-w-0 text-[12.5px] leading-relaxed text-ink-300">
            A pool of third-party model APIs — Gemini, Groq, Cerebras, OpenRouter, GLM, Mistral,
            GitHub Models — connected side by side. The platform's own text chores and its bulk
            lane walk this list in priority order and move to the next provider when one runs out,
            so a free tier ending mid-afternoon is not a feature going dark.
            <div class="mt-1.5">
              <span class="text-ink-200">This never runs a coding agent.</span> Claude, Codex and
              Kimi still answer every prompt you send with their own credentials and their own
              models. Nothing here becomes load bearing: a job that cannot get a provider falls
              back to the local auxiliary model, and then to what the platform did before.
            </div>
            <div class="mt-1.5">
              <span class="text-ink-200">The limits below are advisory.</span>{" "}
              {view?.seedLimitsNote ??
                "As documented at seeding time — verify against the vendor's current published limits."}{" "}
              Every one of them is editable, a blank field means the vendor documents no cap for
              that window, and what a provider reports about itself in its own rate-limit headers
              always wins over what we counted.
            </div>
          </div>
        </div>
      </section>

      {editor.error && (
        <ErrorBanner
          message={editor.error}
          onRetry={() => void editor.refresh()}
          retrying={editor.loading}
        />
      )}

      {view && <PoolPolicy editor={editor} />}

      <div class="flex items-center justify-between gap-2">
        <div class="text-[12.5px] text-ink-300">
          {view
            ? `${providers.length} provider${providers.length === 1 ? "" : "s"}${
                view.available ? "" : " · nothing can take a request right now"
              }`
            : ""}
        </div>
        <button
          type="button"
          onClick={() => setDraft(emptyProviderForm())}
          class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent-blue px-3 text-[13px] font-medium text-ink-900 hover:bg-accent-blue/85"
        >
          <Plus class="h-4 w-4" /> Add provider
        </button>
      </div>

      {editor.loading && !view ? (
        <div class="rounded-lg border border-white/10 bg-[#101318] px-4 py-6 text-[13px] text-ink-300">
          Loading the provider pool…
        </div>
      ) : providers.length === 0 ? (
        <EmptyState
          Icon={Network}
          title="No AI providers connected yet."
          hint="Add one free tier and the platform's own text jobs can use it. Add a second and they keep working when the first runs out."
        />
      ) : (
        <div class="overflow-x-auto rounded-lg border border-white/10 bg-[#101318]">
          <table class="w-full min-w-[62rem] text-left text-[12.5px]">
            <thead class="border-b border-white/[0.08] text-[11px] uppercase tracking-wide text-ink-400">
              <tr>
                <th class="px-3 py-2 font-medium">Provider</th>
                <th class="px-3 py-2 font-medium">Models</th>
                <th class="px-3 py-2 font-medium">Free tier used</th>
                <th class="px-3 py-2 font-medium">Priority</th>
                <th class="px-3 py-2 font-medium">On</th>
                <th class="px-3 py-2" />
              </tr>
            </thead>
            <tbody>
              {providers.map((provider, index) => (
                <ProviderRow
                  key={provider.id}
                  provider={provider}
                  index={index}
                  total={providers.length}
                  month={view?.month ?? ""}
                  busy={editor.saving}
                  testing={editor.testing === provider.id}
                  testResult={
                    editor.testResult?.providerId === provider.id ? editor.testResult : null
                  }
                  onEdit={() => setDraft(formProviderFrom(provider))}
                  onMove={(offset) => void editor.reorder(provider.id, offset)}
                  onToggle={(enabled) =>
                    void editor.saveProvider({ ...formProviderFrom(provider), enabled })
                  }
                  onDelete={() => void editor.deleteProvider(provider.id)}
                  onTest={() => void editor.runTest(provider.id)}
                />
              ))}
            </tbody>
          </table>
        </div>
      )}

      {draft && (
        <ProviderDialog
          draft={draft}
          onDraftChange={setDraft}
          kinds={view?.kinds ?? ["openai", "gemini", "anthropic"]}
          saving={editor.saving}
          onCancel={() => setDraft(null)}
          onSubmit={async (next) => {
            if (await editor.saveProvider(next)) setDraft(null);
          }}
        />
      )}
    </div>
  );
}

/**
 * The pool's own policy. Auto-switch is the whole point of a pool, so it leads;
 * turning it off is a deliberate "use exactly this one", which is why the
 * preferred picker only appears then.
 */
function PoolPolicy({ editor }: { editor: AiProvidersEditor }) {
  const settings = editor.view?.settings;
  // Local overrides win until the save lands, so a click is never swallowed by
  // the payload the last request happened to return.
  const [autoSwitch, setAutoSwitch] = useState<boolean | null>(null);
  const [preferred, setPreferred] = useState<string | null>(null);
  const effectiveAuto = autoSwitch ?? settings?.autoSwitch ?? true;
  const effectivePreferred = preferred ?? settings?.preferredProviderId ?? "";
  const dirty =
    effectiveAuto !== (settings?.autoSwitch ?? true) ||
    effectivePreferred !== (settings?.preferredProviderId ?? "");

  return (
    <Panel
      Icon={Zap}
      title="When a quota runs out"
      description="The pool is walked in the priority order below. This decides whether it may walk past a provider that cannot take the request."
    >
      <Toggle
        checked={effectiveAuto}
        onChange={setAutoSwitch}
        label="Auto-switch on quota exhaustion"
        hint="On means skip whatever is cooling down, out of quota, keyless or off, and try the next provider. Off means use exactly the one named below and fail if it cannot answer."
      />

      {!effectiveAuto && (
        <label class="block space-y-1.5">
          <span class="text-[12px] text-ink-300">Preferred provider</span>
          <select
            value={effectivePreferred}
            onChange={(event) => setPreferred(event.currentTarget.value)}
            class={inputClass}
          >
            <option value="">Highest priority that is ready</option>
            {(editor.view?.providers ?? []).map((provider) => (
              <option key={provider.id} value={provider.id}>
                {provider.label} — {PROVIDER_STATUS_LABELS[provider.status]}
              </option>
            ))}
          </select>
          <span class="block text-[11px] leading-relaxed text-ink-400">
            With auto-switch off, this provider answers every pooled job. When it cannot, the job
            falls back to the local auxiliary model rather than to another free tier.
          </span>
        </label>
      )}

      <div class="flex flex-wrap items-center gap-3">
        <button
          type="button"
          onClick={async () => {
            await editor.saveSettings(effectiveAuto, effectivePreferred);
            setAutoSwitch(null);
            setPreferred(null);
          }}
          disabled={editor.saving || !dirty}
          class="h-9 rounded-md bg-accent-blue px-4 text-[13px] font-semibold text-ink-900 disabled:opacity-50"
        >
          {editor.saving ? "Saving…" : "Save policy"}
        </button>
        <span class="text-[12.5px] text-ink-300">{dirty ? "Unsaved changes." : "Saved."}</span>
      </div>
    </Panel>
  );
}

function ProviderRow({
  provider,
  index,
  total,
  month,
  busy,
  testing,
  testResult,
  onEdit,
  onMove,
  onToggle,
  onDelete,
  onTest,
}: {
  provider: ProviderView;
  index: number;
  total: number;
  month: string;
  busy: boolean;
  testing: boolean;
  testResult: ProviderTestResult | null;
  onEdit: () => void;
  onMove: (offset: number) => void;
  onToggle: (enabled: boolean) => void;
  onDelete: () => void;
  onTest: () => void;
}) {
  const tone = PROVIDER_STATUS_TONE[provider.status];
  const statusLabel = PROVIDER_STATUS_LABELS[provider.status];
  const lastError = provider.usage.lastError;

  function remove() {
    if (
      !confirm(
        `Remove ${provider.label} from the pool? Its usage counters go with it, and any job set to the pool falls through to the next provider.`,
      )
    ) {
      return;
    }
    onDelete();
  }

  return (
    <>
      <tr class="border-b border-white/[0.05] align-top last:border-b-0">
        <td class="px-3 py-2.5">
          <div class="flex items-start gap-2">
            <span
              class={`mt-1.5 h-2 w-2 flex-none rounded-full ${TONE_DOT[tone]}`}
              title={statusLabel}
              aria-hidden="true"
            />
            <div class="min-w-0">
              <div class="text-ink-50">{provider.label}</div>
              <div class="mt-0.5 text-[11px] text-ink-400">
                {statusLabel} · {PROVIDER_KIND_LABELS[provider.kind] ?? provider.kind}
              </div>
              <div class="mt-0.5 font-mono text-[11px] text-ink-400">{provider.id}</div>
              {provider.keyConfigured ? (
                <div class="mt-0.5 inline-flex items-center gap-1 text-[11px] text-ink-400">
                  <Key class="h-3 w-3" aria-hidden="true" />
                  {provider.keySource === "vault"
                    ? `vault: ${provider.apiKeyRef}`
                    : (provider.apiKeyMasked ?? "key stored")}
                </div>
              ) : (
                <div class="mt-0.5 text-[11px] text-accent-yellow">no credential</div>
              )}
            </div>
          </div>
        </td>

        <td class="max-w-[14rem] px-3 py-2.5 font-mono text-[11.5px] text-ink-300">
          <span title={provider.models.map((model) => model.id).join("\n")}>
            {modelsSummary(provider.models)}
          </span>
        </td>

        <td class="px-3 py-2.5">
          <div class="grid gap-2 sm:grid-cols-3">
            <UsageBar
              label="Requests today"
              meter={provider.usage.requestsToday}
              capNote="requests / day"
            />
            <UsageBar
              label="Tokens today"
              meter={provider.usage.tokensToday}
              capNote="tokens / day"
            />
            <UsageBar
              label={`Tokens ${month || "month"}`}
              meter={provider.usage.tokensMonth}
              capNote="tokens / month"
            />
          </div>
          {provider.limitsNote && (
            <div class="mt-1.5 text-[11px] text-ink-400">{provider.limitsNote}</div>
          )}
        </td>

        <td class="px-3 py-2.5">
          <div class="flex items-center gap-1">
            <span class="grid h-6 w-6 flex-none place-items-center rounded bg-white/[0.07] text-[11px] tabular-nums text-ink-300">
              {index + 1}
            </span>
            <IconButton
              Icon={ArrowUp}
              label={`Try ${provider.label} earlier`}
              disabled={index === 0 || busy}
              onClick={() => onMove(-1)}
            />
            <IconButton
              Icon={ArrowDown}
              label={`Try ${provider.label} later`}
              disabled={index === total - 1 || busy}
              onClick={() => onMove(1)}
            />
          </div>
        </td>

        <td class="px-3 py-2.5">
          <input
            type="checkbox"
            checked={provider.enabled}
            disabled={busy}
            onChange={(event) => onToggle(event.currentTarget.checked)}
            aria-label={`Use ${provider.label}`}
            class="h-4 w-4 accent-accent-blue"
          />
        </td>

        <td class="px-3 py-2.5">
          <div class="flex items-center justify-end gap-1">
            <button
              type="button"
              onClick={onTest}
              disabled={testing}
              title="Ask this provider for one short sentence and report how long it took"
              class="inline-flex h-8 items-center gap-1 rounded px-2 text-[11px] text-ink-300 hover:bg-white/[0.08] hover:text-ink-100 disabled:opacity-50"
            >
              {testing ? <Loader class="h-3 w-3 animate-spin" /> : <Zap class="h-3 w-3" />}
              {testing ? "testing…" : "test"}
            </button>
            <button
              type="button"
              onClick={onEdit}
              class="h-8 rounded px-2 text-[11px] text-ink-300 hover:bg-white/[0.08] hover:text-ink-100"
            >
              edit
            </button>
            <button
              type="button"
              onClick={remove}
              disabled={busy}
              aria-label={`Remove ${provider.label}`}
              class="grid h-8 w-8 place-items-center rounded text-ink-300 hover:bg-white/[0.08] hover:text-accent-red disabled:opacity-50"
            >
              <Trash class="h-3.5 w-3.5" />
            </button>
          </div>
        </td>
      </tr>

      {(testResult || lastError || provider.notes) && (
        <tr class="border-b border-white/[0.05] last:border-b-0">
          <td colSpan={6} class="px-3 pb-2.5 text-[11.5px]">
            {provider.notes && (
              <div class="text-ink-400" dir="auto">
                {provider.notes}
              </div>
            )}
            {lastError && (
              <div class="mt-1 flex items-start gap-1.5 text-accent-yellow">
                <AlertCircle class="mt-0.5 h-3.5 w-3.5 flex-none" aria-hidden="true" />
                <span class="min-w-0 break-words">
                  Last error{provider.usage.errors > 1 ? ` (${provider.usage.errors} total)` : ""}:{" "}
                  {lastError}
                </span>
              </div>
            )}
            {testResult && (
              <div
                role="status"
                class={`mt-1.5 rounded-md border px-2.5 py-2 leading-relaxed ${
                  testResult.ok
                    ? "border-accent-green/30 bg-accent-green/[0.08] text-ink-100"
                    : "border-accent-red/30 bg-accent-red/[0.08] text-ink-100"
                }`}
              >
                {testResult.ok ? (
                  <>
                    <div>
                      <span class="text-ink-50">{testResult.model}</span> answered in{" "}
                      <span class="tabular-nums text-ink-50">
                        {formatLatency(testResult.durationMs)}
                      </span>
                      .
                    </div>
                    {testResult.answer && (
                      <div dir="auto" class="bidi-auto mt-1 italic text-ink-200">
                        “{testResult.answer}”
                      </div>
                    )}
                  </>
                ) : (
                  <>
                    Test failed after {formatLatency(testResult.durationMs)}:{" "}
                    {testResult.error || "no answer"}
                  </>
                )}
              </div>
            )}
          </td>
        </tr>
      )}
    </>
  );
}

/**
 * One usage window as an inline SVG bar. No charting library: three bars per
 * row across a table of providers is exactly the case a dependency would make
 * slow, and a track plus a rect is the whole drawing.
 *
 * A meter with no documented cap draws an empty track and prints the count.
 * Filling it to some invented fraction would be the one lie this panel exists
 * to avoid.
 */
function UsageBar({
  label,
  meter,
  capNote,
}: {
  label: string;
  meter: UsageMeter;
  capNote: string;
}) {
  const percent = meterPercent(meter);
  const value = meterLabel(meter);
  const source = meterSourceLabel(meter);
  const title =
    percent == null
      ? `${label}: ${value} — no documented ${capNote} cap · ${source}`
      : `${label}: ${value} (${percent}%) · ${source}`;

  return (
    <div class="min-w-0">
      <div class="flex items-baseline justify-between gap-2">
        <span class="truncate text-[11px] text-ink-400">{label}</span>
        <span class="flex-none font-mono text-[11px] tabular-nums text-ink-200">{value}</span>
      </div>
      <svg
        viewBox="0 0 100 6"
        preserveAspectRatio="none"
        class="mt-1 h-1.5 w-full"
        role="img"
        aria-label={title}
      >
        <title>{title}</title>
        <rect x="0" y="0" width="100" height="6" rx="3" fill="currentColor" class="text-white/10" />
        {percent != null && percent > 0 && (
          <rect
            x="0"
            y="0"
            width={Math.max(percent, 1.5)}
            height="6"
            rx="3"
            fill="currentColor"
            class={TONE_TEXT[meterTone(percent)]}
          />
        )}
      </svg>
      <span
        class="mt-1 inline-block rounded border border-white/10 px-1 text-[10px] leading-[1.5] text-ink-400"
        title={source}
      >
        {meter.source === "reported" ? "reported" : "counted"}
      </span>
    </div>
  );
}

/**
 * The add/edit form. Nothing is written until Save, and the credential input
 * starts empty even when a key is stored: the placeholder shows the mask, and
 * an untouched field means "keep what the server has".
 */
function ProviderDialog({
  draft,
  onDraftChange,
  kinds,
  saving,
  onCancel,
  onSubmit,
}: {
  draft: ProviderForm;
  onDraftChange: (draft: ProviderForm) => void;
  kinds: ProviderKind[];
  saving: boolean;
  onCancel: () => void;
  onSubmit: (draft: ProviderForm) => Promise<void>;
}) {
  const [problem, setProblem] = useState<string | null>(null);

  function patch(changes: Partial<ProviderForm>) {
    onDraftChange({ ...draft, ...changes });
  }

  function patchLimit(key: keyof ProviderLimitsForm, value: string) {
    onDraftChange({ ...draft, limits: { ...draft.limits, [key]: value } });
  }

  async function submit(event: Event) {
    event.preventDefault();
    const found = providerFormProblem(draft);
    setProblem(found);
    if (found) return;
    await onSubmit(draft);
  }

  return (
    <form
      onSubmit={submit}
      class="space-y-3 rounded-lg border border-white/10 bg-[#101318] p-4"
      aria-label={draft.existing ? `Edit ${draft.label || draft.id}` : "Add provider"}
    >
      <div class="text-[14.5px] font-semibold text-ink-50">
        {draft.existing ? `Edit ${draft.label || draft.id}` : "Add provider"}
      </div>

      <div class="grid gap-3 sm:grid-cols-3">
        <Field label="Id" hint="Lower-case, 2–40 characters. It keys the usage counters.">
          <input
            value={draft.id}
            disabled={draft.existing}
            onInput={(event) => patch({ id: event.currentTarget.value })}
            placeholder="groq"
            spellcheck={false}
            dir="ltr"
            class={`${inputClass} bidi-ltr font-mono`}
          />
        </Field>
        <Field label="Label" hint="What the table and the dashboard card call it.">
          <input
            value={draft.label}
            onInput={(event) => patch({ label: event.currentTarget.value })}
            placeholder="Groq"
            class={inputClass}
          />
        </Field>
        <Field label="Wire shape" hint="Almost everything speaks the OpenAI-compatible one.">
          <select
            value={draft.kind}
            onChange={(event) => patch({ kind: event.currentTarget.value as ProviderKind })}
            class={inputClass}
          >
            {kinds.map((kind) => (
              <option key={kind} value={kind}>
                {PROVIDER_KIND_LABELS[kind] ?? kind}
              </option>
            ))}
          </select>
        </Field>
      </div>

      <Field
        label="Base URL"
        hint="Without a trailing slash. The wire shape decides what is appended — /chat/completions, /models/{model}:generateContent, or /messages."
      >
        <input
          value={draft.baseUrl}
          onInput={(event) => patch({ baseUrl: event.currentTarget.value })}
          placeholder="https://api.groq.com/openai/v1"
          spellcheck={false}
          dir="ltr"
          class={`${inputClass} bidi-ltr font-mono`}
        />
      </Field>

      <div class="grid gap-3 rounded-md border border-white/10 bg-white/[0.02] p-3 sm:grid-cols-2">
        <Field label="API key">
          <input
            type="password"
            value={draft.apiKey}
            disabled={draft.clearApiKey}
            onInput={(event) => patch({ apiKey: event.currentTarget.value })}
            placeholder={draft.keyConfigured ? draft.apiKeyMasked || "••••" : "gsk_…"}
            autocomplete="off"
            spellcheck={false}
            class={inputClass}
          />
          <span class="mt-1 flex flex-wrap items-center gap-2 text-[11px] text-ink-400">
            {draft.clearApiKey ? (
              <>
                <span class="text-accent-yellow">The stored key is removed on save.</span>
                <button
                  type="button"
                  onClick={() => patch({ clearApiKey: false })}
                  class="rounded border border-white/10 px-1.5 py-0.5 text-ink-200 hover:bg-white/[0.07]"
                >
                  Keep it
                </button>
              </>
            ) : draft.keyConfigured ? (
              <>
                <span>A key is stored. Leave this blank to keep it.</span>
                <button
                  type="button"
                  onClick={() => patch({ apiKey: "", clearApiKey: true })}
                  class="rounded border border-white/10 px-1.5 py-0.5 text-ink-200 hover:bg-white/[0.07]"
                >
                  Remove key
                </button>
              </>
            ) : (
              <span>Stored encrypted and never shown again — only its last four characters.</span>
            )}
          </span>
        </Field>
        <Field
          label="Secrets-vault key name"
          hint="Optional, and preferred: the value is read from the vault at call time, so it is never copied into this entry."
        >
          <input
            value={draft.apiKeyRef}
            onInput={(event) => patch({ apiKeyRef: event.currentTarget.value })}
            placeholder="GROQ_API_KEY"
            spellcheck={false}
            dir="ltr"
            class={`${inputClass} bidi-ltr font-mono`}
          />
        </Field>
      </div>

      <Field
        label="Models"
        hint="One per line: id | label | context tokens | text,code,bulk — everything after the id optional."
      >
        <textarea
          value={draft.modelsText}
          onInput={(event) => patch({ modelsText: event.currentTarget.value })}
          rows={4}
          spellcheck={false}
          dir="ltr"
          placeholder={"llama-3.3-70b-versatile | Llama 3.3 70B | 131072 | text,code"}
          class="bidi-ltr w-full resize-y rounded-md border border-white/10 bg-black/30 px-2.5 py-1.5 font-mono text-[12.5px] leading-[1.45] text-ink-50 placeholder:text-ink-400 focus:border-accent-blue/50 focus:outline-none"
        />
      </Field>

      <fieldset class="space-y-2 rounded-md border border-white/10 bg-white/[0.02] p-3">
        <legend class="text-[11.5px] font-medium text-ink-300">Documented free-tier limits</legend>
        <p class="text-[11px] leading-relaxed text-ink-400">
          Leave a field blank when the vendor documents no cap for that window — blank means “not
          documented”, and the panel then shows a count instead of a percentage. These numbers only
          decide when the pool skips ahead; they are never treated as facts about the world.
        </p>
        <div class="grid gap-3 sm:grid-cols-3">
          {PROVIDER_LIMIT_FIELDS.map((field) => (
            <Field key={field.key} label={field.label} hint={field.hint}>
              <input
                type="number"
                min={1}
                value={draft.limits[field.key]}
                onInput={(event) => patchLimit(field.key, event.currentTarget.value)}
                placeholder="not documented"
                class={`${inputClass} tabular-nums`}
              />
            </Field>
          ))}
        </div>
      </fieldset>

      <Field label="Notes" hint="Whatever the next operator needs to know about this account.">
        <input
          value={draft.notes}
          onInput={(event) => patch({ notes: event.currentTarget.value })}
          placeholder="Personal Google account — free tier only"
          class={inputClass}
        />
      </Field>

      <label class="flex cursor-pointer items-start gap-2.5 rounded-md border border-white/10 bg-white/[0.03] p-2.5">
        <input
          type="checkbox"
          checked={draft.enabled}
          onChange={(event) => patch({ enabled: event.currentTarget.checked })}
          class="mt-0.5 h-4 w-4 flex-none accent-accent-blue"
        />
        <span class="min-w-0">
          <span class="block text-[13px] text-ink-100">Use this provider</span>
          <span class="mt-0.5 block text-[11.5px] leading-snug text-ink-300">
            Off keeps the entry and its counters but takes it out of the rotation. A provider
            cannot be switched on without a key or a vault key name.
          </span>
        </span>
      </label>

      {problem && (
        <div class="flex items-start gap-1.5 text-[12px] text-accent-red" role="alert">
          <AlertCircle class="mt-0.5 h-3.5 w-3.5 flex-none" aria-hidden="true" />
          <span>{problem}</span>
        </div>
      )}

      <div class="flex items-center gap-2">
        <button
          type="submit"
          disabled={saving}
          class="inline-flex h-9 items-center gap-1.5 rounded-md bg-accent-blue px-3 text-[13px] font-medium text-ink-900 hover:bg-accent-blue/85 disabled:opacity-50"
        >
          {saving ? <Loader class="h-4 w-4 animate-spin" /> : <Check class="h-4 w-4" />}
          {draft.existing ? "Save" : "Add"}
        </button>
        <button
          type="button"
          onClick={onCancel}
          class="h-9 rounded-md px-3 text-[13px] text-ink-300 hover:bg-white/[0.08] hover:text-ink-100"
        >
          Cancel
        </button>
      </div>
    </form>
  );
}

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: preact.ComponentChildren;
}) {
  return (
    <label class="block space-y-1">
      <span class="block text-[11.5px] font-medium text-ink-300">{label}</span>
      {children}
      {hint && <span class="block text-[11px] leading-snug text-ink-400">{hint}</span>}
    </label>
  );
}

function Toggle({
  checked,
  onChange,
  label,
  hint,
}: {
  checked: boolean;
  onChange: (checked: boolean) => void;
  label: string;
  hint: string;
}) {
  return (
    <label class="flex cursor-pointer items-start gap-2.5 rounded-md border border-white/10 bg-white/[0.03] p-2.5">
      <input
        type="checkbox"
        checked={checked}
        onChange={(event) => onChange(event.currentTarget.checked)}
        class="mt-0.5 h-4 w-4 flex-none accent-accent-blue"
      />
      <span class="min-w-0">
        <span class="block text-[13px] text-ink-100">{label}</span>
        <span class="mt-0.5 block text-[11.5px] leading-snug text-ink-300">{hint}</span>
      </span>
    </label>
  );
}

function IconButton({
  Icon,
  label,
  onClick,
  disabled = false,
}: {
  Icon: (props: { class?: string }) => JSX.Element;
  label: string;
  onClick: () => void;
  disabled?: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled}
      aria-label={label}
      title={label}
      class="grid h-8 w-8 flex-none place-items-center rounded-md text-ink-300 hover:bg-white/[0.08] hover:text-ink-100 disabled:opacity-30"
    >
      <Icon class="h-3.5 w-3.5" />
    </button>
  );
}

function Panel({
  Icon,
  title,
  description,
  children,
}: {
  Icon: (props: { class?: string }) => JSX.Element;
  title: string;
  description: string;
  children: preact.ComponentChildren;
}) {
  return (
    <section class="overflow-hidden rounded-lg border border-white/10 bg-[#101318]">
      <header class="flex items-start gap-3 border-b border-white/[0.06] px-4 py-3">
        <div class="mt-0.5 grid h-9 w-9 flex-none place-items-center rounded-md border border-white/10 bg-white/[0.06]">
          <Icon class="h-4 w-4 text-ink-200" />
        </div>
        <div class="min-w-0 flex-1">
          <div class="text-[14.5px] font-semibold text-ink-50">{title}</div>
          <div class="mt-0.5 text-[12.5px] leading-snug text-ink-300">{description}</div>
        </div>
      </header>
      <div class="space-y-3 p-3">{children}</div>
    </section>
  );
}
