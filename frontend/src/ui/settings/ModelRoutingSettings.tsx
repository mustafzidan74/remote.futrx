import type { JSX } from "preact";
import { useState } from "preact/hooks";
import type { ChatProvider } from "../../models/chat";
import type {
  RoutingConditionKind,
  RoutingModelRef,
  RoutingPolicy,
  RoutingProviderModels,
  RoutingRule,
} from "../../models/modelRouting";
import {
  ROUTING_CONDITION_KINDS,
  ROUTING_SYNTHETIC_VALUES,
} from "../../models/modelRouting";
import type { ModelRoutingEditor } from "../../state/hooks/settings/useModelRouting";
import {
  addRule,
  changeConditionKind,
  isRefAvailable,
  modelsForProvider,
  moveRule,
  refLabel,
  removeRule,
  ruleTitle,
  setRefProvider,
  updateRule,
} from "../../state/settings/modelRoutingState";
import { ErrorBanner } from "../primitives/Feedback";
import { AlertCircle, ArrowDown, ArrowUp, Loader, Trash, Zap } from "../primitives/icons";

const MODE_SUGGESTIONS = ["chat", "plan", "code", "review", "debug", "full-auto"];

/**
 * The automatic model routing policy.
 *
 * The rule list is ordered and the server stops at the first match, so the
 * editor puts the order in front of the operator rather than hiding it behind
 * a priority field: what you read top-to-bottom is what the server evaluates
 * top-to-bottom. Nothing here saves on its own — routing decides which model
 * answers every unattended run, so the change is explicit.
 */
export function ModelRoutingSettings({ editor }: { editor: ModelRoutingEditor }) {
  const { draft, catalog, providers } = editor;

  if (editor.loading && !draft) {
    return (
      <div class="flex items-center justify-center gap-2 rounded-lg border border-white/10 bg-[#101318] px-4 py-12 text-[13px] text-ink-300">
        <Loader class="h-4 w-4 animate-spin" /> Loading routing policy…
      </div>
    );
  }
  if (!draft) {
    return (
      <ErrorBanner
        message={editor.error ?? "The routing policy could not be loaded."}
        onRetry={() => void editor.refresh()}
        retrying={editor.loading}
      />
    );
  }

  const patch = (next: Partial<RoutingPolicy>) => editor.setDraft({ ...draft, ...next });

  return (
    <div class="space-y-4">
      {editor.error && (
        <ErrorBanner
          message={editor.error}
          onRetry={() => void editor.refresh()}
          retrying={editor.loading}
        />
      )}

      <Panel
        Icon={Zap}
        title="Automatic routing"
        description="Send routine turns to a cheap model and hard ones to the expensive one. A chat only follows this policy once its model pill is switched to Auto."
      >
        <Toggle
          checked={draft.enabled}
          onChange={(enabled) => patch({ enabled })}
          label="Route turns automatically"
          hint="Off means every chat runs the model it names, exactly as before."
        />
        <Toggle
          checked={draft.autoHeuristics}
          onChange={(autoHeuristics) => patch({ autoHeuristics })}
          label="Use the built-in heuristics"
          hint="After the rules: long or refactor/architecture/debug/migration prompts go expensive; chat mode, platform checks, and short prompts with no code block go cheap."
        />

        <div class="grid gap-3 sm:grid-cols-3">
          <ModelPicker
            label="Default model"
            hint="Used when no rule and no heuristic claims a turn. Also the baseline the savings report prices against."
            catalog={catalog}
            providers={providers}
            value={draft.default}
            onChange={(next) => patch({ default: next })}
          />
          <ModelPicker
            label="Cheap model"
            hint="The cheap pole the heuristics and the seeded rules point at."
            catalog={catalog}
            providers={providers}
            value={draft.cheapModel}
            onChange={(next) => patch({ cheapModel: next })}
          />
          <ModelPicker
            label="Expensive model"
            hint="The expensive pole, for work the policy decides is hard."
            catalog={catalog}
            providers={providers}
            value={draft.expensiveModel}
            onChange={(next) => patch({ expensiveModel: next })}
          />
        </div>

        {providers.length > 0 && (
          <p class="text-[11.5px] text-ink-300">
            Connected agents: <span class="text-ink-100">{providers.join(", ")}</span>. A rule
            pointing anywhere else falls back to the default at run time, and the transcript says
            so.
          </p>
        )}
      </Panel>

      <Panel
        Icon={Zap}
        title="Rules"
        description="Evaluated top to bottom; the first match wins. A chat pinned to its own model always wins over every rule."
      >
        {draft.rules.length === 0 && (
          <p class="text-[12.5px] text-ink-300">
            No rules. With none, every routed turn uses the heuristics if they are on, and the
            default model otherwise.
          </p>
        )}
        {draft.rules.map((rule, index) => (
          <RuleRow
            key={rule.id}
            rule={rule}
            index={index}
            total={draft.rules.length}
            catalog={catalog}
            providers={providers}
            onChange={(patchRule) => editor.setDraft(updateRule(draft, rule.id, patchRule))}
            onKindChange={(kind) => editor.setDraft(changeConditionKind(draft, rule.id, kind))}
            onMove={(offset) => editor.setDraft(moveRule(draft, rule.id, offset))}
            onRemove={() => editor.setDraft(removeRule(draft, rule.id))}
          />
        ))}
        <button
          type="button"
          onClick={() => editor.setDraft(addRule(draft))}
          class="h-9 rounded-md bg-white/[0.08] px-3 text-[13px] text-ink-100 hover:bg-white/[0.12]"
        >
          Add rule
        </button>
      </Panel>

      <PolicyTester editor={editor} />

      <div class="sticky bottom-0 flex flex-wrap items-center gap-3 rounded-lg border border-white/10 bg-[#101318] px-4 py-3">
        <button
          type="button"
          onClick={() => void editor.save().catch(() => {})}
          disabled={editor.saving || !editor.dirty || editor.problem !== null}
          class="h-9 rounded-md bg-accent-blue px-4 text-[13px] font-semibold text-ink-900 disabled:opacity-50"
        >
          {editor.saving ? "Saving…" : "Save policy"}
        </button>
        {editor.dirty && (
          <button
            type="button"
            onClick={editor.revert}
            disabled={editor.saving}
            class="h-9 rounded-md px-3 text-[13px] text-ink-300 hover:bg-white/[0.07] hover:text-ink-100"
          >
            Discard changes
          </button>
        )}
        {editor.problem ? (
          <span class="inline-flex items-center gap-1.5 text-[12.5px] text-accent-red">
            <AlertCircle class="h-3.5 w-3.5" aria-hidden="true" />
            {editor.problem}
          </span>
        ) : (
          <span class="text-[12.5px] text-ink-300">
            {editor.dirty ? "Unsaved changes." : "Saved."}
          </span>
        )}
      </div>
    </div>
  );
}

function RuleRow({
  rule,
  index,
  total,
  catalog,
  providers,
  onChange,
  onKindChange,
  onMove,
  onRemove,
}: {
  rule: RoutingRule;
  index: number;
  total: number;
  catalog: RoutingProviderModels[];
  providers: string[];
  onChange: (patch: Partial<RoutingRule>) => void;
  onKindChange: (kind: RoutingConditionKind) => void;
  onMove: (offset: number) => void;
  onRemove: () => void;
}) {
  const kind = ROUTING_CONDITION_KINDS.find((entry) => entry.kind === rule.when.kind);
  const unavailable = !isRefAvailable(rule.use, providers);

  return (
    <div class="rounded-md border border-white/10 bg-white/[0.03] p-3 space-y-3">
      <div class="flex flex-wrap items-center gap-2">
        <span class="grid h-6 w-6 flex-none place-items-center rounded bg-white/[0.07] text-[11px] tabular-nums text-ink-300">
          {index + 1}
        </span>
        <input
          type="text"
          value={rule.note ?? ""}
          placeholder="Name this rule"
          onInput={(event) => onChange({ note: event.currentTarget.value })}
          aria-label={`Name for rule ${index + 1}`}
          class="h-9 min-w-0 flex-1 rounded-md border border-white/10 bg-[#0c0f13] px-3 text-[13px] text-ink-50 outline-none placeholder:text-ink-400 focus:border-accent-blue/60"
        />
        <label class="inline-flex flex-none items-center gap-1.5 text-[12.5px] text-ink-200">
          <input
            type="checkbox"
            checked={rule.enabled}
            onChange={(event) => onChange({ enabled: event.currentTarget.checked })}
            class="h-4 w-4 accent-accent-blue"
          />
          Armed
        </label>
        <IconButton
          Icon={ArrowUp}
          label={`Move rule ${index + 1} earlier`}
          disabled={index === 0}
          onClick={() => onMove(-1)}
        />
        <IconButton
          Icon={ArrowDown}
          label={`Move rule ${index + 1} later`}
          disabled={index === total - 1}
          onClick={() => onMove(1)}
        />
        <IconButton Icon={Trash} label={`Delete rule ${ruleTitle(rule)}`} onClick={onRemove} />
      </div>

      <div class="grid gap-3 sm:grid-cols-2">
        <label class="block min-w-0">
          <span class="block text-[12px] text-ink-300">When</span>
          <select
            value={rule.when.kind}
            onChange={(event) => onKindChange(event.currentTarget.value as RoutingConditionKind)}
            class="mt-1 h-9 w-full rounded-md border border-white/10 bg-[#0c0f13] px-2 text-[13px] text-ink-50 outline-none focus:border-accent-blue/60"
          >
            {ROUTING_CONDITION_KINDS.map((entry) => (
              <option key={entry.kind} value={entry.kind}>
                {entry.label}
              </option>
            ))}
          </select>
        </label>
        <label class="block min-w-0">
          <span class="block text-[12px] text-ink-300">{kind?.valueLabel ?? "Value"}</span>
          <input
            type="text"
            value={rule.when.value ?? ""}
            placeholder={kind?.placeholder}
            list={valueSuggestionsId(rule.when.kind)}
            spellcheck={false}
            onInput={(event) =>
              onChange({ when: { kind: rule.when.kind, value: event.currentTarget.value } })
            }
            class="mt-1 h-9 w-full rounded-md border border-white/10 bg-[#0c0f13] px-3 font-mono text-[12.5px] text-ink-50 outline-none placeholder:text-ink-400 focus:border-accent-blue/60"
          />
        </label>
      </div>

      <ModelPicker
        label="Use"
        hint={
          unavailable
            ? "This agent is not connected on this host. The rule will fall back to the default model."
            : `Routes to ${refLabel(catalog, rule.use)}.`
        }
        tone={unavailable ? "warn" : "normal"}
        catalog={catalog}
        providers={providers}
        value={rule.use}
        onChange={(use) => onChange({ use })}
      />

      <datalist id="routing-synthetic-values">
        {ROUTING_SYNTHETIC_VALUES.map((value) => (
          <option key={value} value={value} />
        ))}
      </datalist>
      <datalist id="routing-mode-values">
        {MODE_SUGGESTIONS.map((value) => (
          <option key={value} value={value} />
        ))}
      </datalist>
    </div>
  );
}

function PolicyTester({ editor }: { editor: ModelRoutingEditor }) {
  const [prompt, setPrompt] = useState("");
  const [mode, setMode] = useState("code");
  const [synthetic, setSynthetic] = useState("");
  const decision = editor.tested;

  return (
    <Panel
      Icon={Zap}
      title="Test this policy"
      description="Paste a prompt and see which rule claims it. The saved policy is what answers, so save before testing an edit."
    >
      <textarea
        value={prompt}
        onInput={(event) => setPrompt(event.currentTarget.value)}
        rows={4}
        placeholder="Paste a prompt…"
        aria-label="Prompt to test"
        class="w-full rounded-md border border-white/10 bg-[#0c0f13] p-3 text-[13px] text-ink-50 outline-none placeholder:text-ink-400 focus:border-accent-blue/60"
      />
      <div class="flex flex-wrap items-end gap-3">
        <label class="block">
          <span class="block text-[12px] text-ink-300">Mode</span>
          <select
            value={mode}
            onChange={(event) => setMode(event.currentTarget.value)}
            class="mt-1 h-9 rounded-md border border-white/10 bg-[#0c0f13] px-2 text-[13px] text-ink-50"
          >
            {MODE_SUGGESTIONS.map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </select>
        </label>
        <label class="block">
          <span class="block text-[12px] text-ink-300">Platform label</span>
          <select
            value={synthetic}
            onChange={(event) => setSynthetic(event.currentTarget.value)}
            class="mt-1 h-9 rounded-md border border-white/10 bg-[#0c0f13] px-2 text-[13px] text-ink-50"
          >
            <option value="">a person typed it</option>
            {ROUTING_SYNTHETIC_VALUES.filter((value) => value !== "any").map((value) => (
              <option key={value} value={value}>
                {value}
              </option>
            ))}
          </select>
        </label>
        <button
          type="button"
          onClick={() => void editor.test({ prompt, mode, synthetic: synthetic || undefined })}
          disabled={editor.testing}
          class="h-9 rounded-md bg-white/[0.08] px-3 text-[13px] text-ink-100 hover:bg-white/[0.12] disabled:opacity-60"
        >
          {editor.testing ? "Testing…" : "Test"}
        </button>
      </div>

      {editor.testError && <ErrorBanner message={editor.testError} />}

      {decision && (
        <div class="rounded-md border border-white/10 bg-white/[0.03] p-3">
          <div class="text-[13px] text-ink-50">
            {decision.routed ? "Routed to " : "Kept the chat's own model: "}
            <span class="font-semibold">
              {refLabel(editor.catalog, {
                provider: decision.provider as ChatProvider,
                model: decision.model,
              })}
            </span>
          </div>
          <div class="mt-1 text-[12.5px] text-ink-300">{decision.reason}</div>
          {decision.ruleId && (
            <div class="mt-1 font-mono text-[11.5px] text-ink-400">rule: {decision.ruleId}</div>
          )}
        </div>
      )}
    </Panel>
  );
}

function ModelPicker({
  label,
  hint,
  catalog,
  providers,
  value,
  onChange,
  tone = "normal",
}: {
  label: string;
  hint: string;
  catalog: RoutingProviderModels[];
  providers: string[];
  value: RoutingModelRef;
  onChange: (next: RoutingModelRef) => void;
  tone?: "normal" | "warn";
}) {
  const models = modelsForProvider(catalog, value.provider);
  return (
    <div class="min-w-0">
      <span class="block text-[12.5px] font-medium text-ink-100">{label}</span>
      <div class="mt-1.5 flex gap-2">
        <select
          value={value.provider}
          onChange={(event) =>
            onChange(
              setRefProvider(catalog, value, event.currentTarget.value as ChatProvider | ""),
            )
          }
          aria-label={`${label} agent`}
          class="h-9 min-w-0 flex-1 rounded-md border border-white/10 bg-[#0c0f13] px-2 text-[13px] text-ink-50 outline-none focus:border-accent-blue/60"
        >
          <option value="">Choose an agent…</option>
          {catalog.map((entry) => (
            <option key={entry.provider} value={entry.provider}>
              {entry.label}
              {providers.length > 0 && !providers.includes(entry.provider)
                ? " (not connected)"
                : ""}
            </option>
          ))}
        </select>
        <select
          value={value.model ?? ""}
          onChange={(event) => onChange({ ...value, model: event.currentTarget.value })}
          disabled={!value.provider}
          aria-label={`${label} model`}
          class="h-9 min-w-0 flex-1 rounded-md border border-white/10 bg-[#0c0f13] px-2 text-[13px] text-ink-50 outline-none focus:border-accent-blue/60 disabled:opacity-50"
        >
          {models.map((model) => (
            <option key={model.value || "auto"} value={model.value}>
              {model.label}
            </option>
          ))}
        </select>
      </div>
      <span
        class={`mt-1 block text-[11px] ${tone === "warn" ? "text-accent-orange" : "text-ink-300"}`}
      >
        {hint}
      </span>
    </div>
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

function valueSuggestionsId(kind: RoutingConditionKind): string | undefined {
  if (kind === "synthetic") return "routing-synthetic-values";
  if (kind === "modeIs") return "routing-mode-values";
  return undefined;
}
