import { useEffect, useRef } from "preact/hooks";
import type { ProjectTemplate, TemplateInput } from "../../models/template";
import {
  newProjectState,
  type NewProjectState,
} from "../../state/projects/newProjectState";
import { AlertCircle, Check, Loader, X } from "../primitives/icons";
import { templateIcon } from "./templateIcons";

export function NewProjectDialog({
  state,
  onNameChange,
  onSelectTemplate,
  onInputChange,
  onSubmit,
  onClose,
}: {
  state: NewProjectState;
  onNameChange: (name: string) => void;
  onSelectTemplate: (template: string) => void;
  onInputChange: (key: string, value: string) => void;
  onSubmit: () => void;
  onClose: () => void;
}) {
  const nameInput = useRef<HTMLInputElement>(null);
  const panel = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (state.open) nameInput.current?.focus();
  }, [state.open]);

  // Escape closes, and Tab stays inside the panel: a modal that leaks focus to
  // the sidebar behind it is unusable with a keyboard or a screen reader.
  useEffect(() => {
    if (!state.open) return;
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        onClose();
        return;
      }
      if (event.key !== "Tab" || !panel.current) return;
      const focusable = Array.from(
        panel.current.querySelectorAll<HTMLElement>(
          'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])'
        )
      ).filter((element) => element.offsetParent !== null);
      if (focusable.length === 0) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      const current = document.activeElement as HTMLElement | null;
      if (event.shiftKey && (current === first || !panel.current.contains(current))) {
        event.preventDefault();
        last.focus();
      } else if (!event.shiftKey && current === last) {
        event.preventDefault();
        first.focus();
      }
    }
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [state.open, onClose]);

  if (!state.open) return null;

  const canSubmit = newProjectState.canSubmit(state);
  const notice = newProjectState.firstStartNotice(state);

  return (
    <div
      class="fixed inset-0 z-50 flex items-end md:items-center justify-center bg-black/60 p-0 md:p-4"
      onClick={onClose}
    >
      <div
        ref={panel}
        role="dialog"
        aria-modal="true"
        aria-labelledby="new-project-title"
        class="dialog-panel w-full md:max-w-2xl flex flex-col rounded-t-xl md:rounded-xl
               border border-white/10 bg-[#101318] shadow-2xl overflow-hidden"
        onClick={(event) => event.stopPropagation()}
      >
        <header class="flex-none flex items-start gap-3 px-4 py-3 border-b border-white/[0.08]">
          <div class="flex-1 min-w-0">
            <h2 id="new-project-title" class="text-[15px] font-semibold text-ink-50">
              New project
            </h2>
            <p class="text-[12.5px] text-ink-300 mt-0.5 leading-snug">
              Each project gets its own container. The template decides what is
              installed inside it and cannot be changed later.
            </p>
          </div>
          <button
            type="button"
            onClick={onClose}
            class="h-8 w-8 rounded-md text-ink-300 hover:text-ink-50 hover:bg-white/[0.08] grid place-items-center"
            aria-label="Close"
          >
            <X class="w-4 h-4" />
          </button>
        </header>

        <form
          class="flex-1 min-h-0 flex flex-col"
          onSubmit={(event) => {
            event.preventDefault();
            if (canSubmit) onSubmit();
          }}
        >
          <div class="flex-1 min-h-0 overflow-y-auto touch-scroll px-4 py-4 space-y-4">
            <label class="block">
              <span class="text-[12.5px] font-medium text-ink-200">Project name</span>
              <input
                ref={nameInput}
                type="text"
                value={state.name}
                dir="auto"
                onInput={(event) => onNameChange((event.target as HTMLInputElement).value)}
                placeholder="My project"
                class="mt-1.5 w-full h-10 px-3 rounded-md bg-white/[0.04] border border-white/10
                       text-[14px] text-ink-50 placeholder:text-ink-400
                       focus:outline-none focus:border-accent-blue/60 focus:ring-1 focus:ring-accent-blue/30"
              />
            </label>

            <div>
              <div class="text-[12.5px] font-medium text-ink-200">Template</div>
              <TemplatePicker
                templates={state.templates}
                loading={state.templatesLoading}
                error={state.templatesError}
                selected={state.template}
                onSelect={onSelectTemplate}
              />
              {notice && (
                <p dir="auto" class="bidi-auto mt-2 text-[12px] text-ink-300 leading-snug">{notice}</p>
              )}
            </div>

            <TemplateInputs state={state} onChange={onInputChange} />

            {state.error && (
              <div class="flex items-start gap-2.5 rounded-md border border-accent-red/30
                          bg-accent-red/[0.08] px-3 py-2 text-[12.5px]">
                <AlertCircle class="w-4 h-4 mt-0.5 flex-none text-accent-red" />
                <span dir="auto" class="bidi-auto min-w-0 text-ink-100 break-words">{state.error}</span>
              </div>
            )}
          </div>

          <footer class="flex-none flex justify-end gap-2 px-4 py-3 border-t border-white/[0.08]">
            <button
              type="button"
              onClick={onClose}
              class="h-10 px-3 rounded-md text-[13.5px] text-ink-200 hover:text-ink-50 hover:bg-white/[0.08]"
            >
              Cancel
            </button>
            <button
              type="submit"
              disabled={!canSubmit}
              class="h-10 px-4 rounded-md text-[13.5px] font-medium text-white
                     bg-accent-blue hover:bg-accent-blue/85 disabled:opacity-50
                     disabled:cursor-not-allowed inline-flex items-center gap-2"
            >
              {state.submitting && <Loader class="w-4 h-4 animate-spin" />}
              Create project
            </button>
          </footer>
        </form>
      </div>
    </div>
  );
}

/**
 * The selected template's own form. Most templates declare no inputs and this
 * renders nothing, which is why it is not a titled panel of its own.
 */
function TemplateInputs({
  state,
  onChange,
}: {
  state: NewProjectState;
  onChange: (key: string, value: string) => void;
}) {
  const inputs = newProjectState.inputs(state);
  if (inputs.length === 0) return null;
  const template = newProjectState.selectedTemplate(state);

  return (
    <fieldset class="rounded-lg border border-white/10 bg-white/[0.02] p-3 space-y-3">
      <legend class="px-1 text-[12.5px] font-medium text-ink-200">
        {template?.title ?? "Template"} setup
      </legend>
      {inputs.map((input) => (
        <TemplateInputField
          key={input.key}
          input={input}
          value={newProjectState.inputValue(state, input)}
          error={newProjectState.visibleInputError(state, input)}
          onChange={(value) => onChange(input.key, value)}
        />
      ))}
    </fieldset>
  );
}

function TemplateInputField({
  input,
  value,
  error,
  onChange,
}: {
  input: TemplateInput;
  value: string;
  error: string;
  onChange: (value: string) => void;
}) {
  const fieldId = `template-input-${input.key}`;
  const describedBy = error ? `${fieldId}-error` : input.help ? `${fieldId}-help` : undefined;
  const control =
    "w-full h-10 px-3 rounded-md bg-white/[0.04] border text-[14px] text-ink-50 " +
    "placeholder:text-ink-400 focus:outline-none focus:ring-1 focus:ring-accent-blue/30 " +
    (error ? "border-accent-red/60" : "border-white/10 focus:border-accent-blue/60");

  if (input.type === "checkbox") {
    return (
      <div>
        <label class="flex items-start gap-2.5 cursor-pointer">
          <input
            id={fieldId}
            type="checkbox"
            checked={value === "true"}
            onChange={(event) =>
              onChange((event.target as HTMLInputElement).checked ? "true" : "false")
            }
            class="mt-0.5 h-4 w-4 flex-none rounded border-white/20 bg-white/[0.04] accent-[#3b82f6]"
          />
          <span dir="auto" class="bidi-auto text-[13px] text-ink-100 leading-snug">{input.label}</span>
        </label>
        {input.help && (
          <p id={`${fieldId}-help`} class="mt-1 ml-[26px] text-[11.5px] text-ink-400 leading-snug">
            {input.help}
          </p>
        )}
      </div>
    );
  }

  return (
    <div>
      <label class="block" for={fieldId}>
        <span class="text-[12.5px] font-medium text-ink-200">
          {input.label}
          {!input.required && (
            <span class="ml-1.5 text-[11px] font-normal text-ink-400">optional</span>
          )}
        </span>
      </label>
      {input.type === "select" ? (
        <select
          id={fieldId}
          value={value}
          aria-describedby={describedBy}
          onChange={(event) => onChange((event.target as HTMLSelectElement).value)}
          class={`mt-1.5 ${control}`}
        >
          {input.options?.map((option) => (
            <option key={option.value} value={option.value} class="bg-[#101318]">
              {option.label}
            </option>
          ))}
        </select>
      ) : (
        <input
          id={fieldId}
          type={input.type === "password" ? "password" : input.type === "email" ? "email" : "text"}
          value={value}
          dir={input.type === "password" ? "ltr" : "auto"}
          autocomplete={input.type === "password" ? "new-password" : "off"}
          placeholder={placeholderFor(input)}
          aria-describedby={describedBy}
          aria-invalid={error ? "true" : undefined}
          onInput={(event) => onChange((event.target as HTMLInputElement).value)}
          class={`mt-1.5 ${control}`}
        />
      )}
      {error ? (
        <p id={`${fieldId}-error`} class="mt-1 text-[11.5px] text-accent-red leading-snug">
          {error}
        </p>
      ) : (
        input.help && (
          <p id={`${fieldId}-help`} class="mt-1 text-[11.5px] text-ink-400 leading-snug">
            {input.help}
          </p>
        )
      )}
    </div>
  );
}

/** Says what happens when the field is left empty, rather than repeating the label. */
function placeholderFor(input: TemplateInput): string {
  if (input.generate) return "generated automatically";
  if (input.defaultFrom === "userEmail") return "your account email";
  return "";
}

function TemplatePicker({
  templates,
  loading,
  error,
  selected,
  onSelect,
}: {
  templates: ProjectTemplate[];
  loading: boolean;
  error: string;
  selected: string;
  onSelect: (template: string) => void;
}) {
  if (loading && templates.length === 0) {
    return (
      <div class="mt-1.5 flex items-center gap-2 text-[12.5px] text-ink-300">
        <Loader class="w-4 h-4 animate-spin" /> Loading templates…
      </div>
    );
  }
  if (error && templates.length === 0) {
    return (
      <div class="mt-1.5 text-[12.5px] text-ink-300">
        Templates could not be loaded ({error}). The project will be created
        from the blank template.
      </div>
    );
  }

  return (
    <div class="mt-1.5 grid grid-cols-1 sm:grid-cols-2 gap-2" role="radiogroup" aria-label="Template">
      {templates.map((template) => {
        const active = template.name === selected;
        const Icon = templateIcon(template.icon);
        return (
          <button
            key={template.name}
            type="button"
            role="radio"
            aria-checked={active}
            onClick={() => onSelect(template.name)}
            class={`text-left rounded-lg border p-3 transition-colors ${
              active
                ? "border-accent-blue/60 bg-accent-blue/[0.08]"
                : "border-white/10 bg-white/[0.03] hover:bg-white/[0.06]"
            }`}
          >
            <div class="flex items-center gap-2">
              <Icon class={`w-4 h-4 flex-none ${active ? "text-accent-blue" : "text-ink-300"}`} />
              <span class="text-[13.5px] font-medium text-ink-50 truncate">{template.title}</span>
              {active && <Check class="w-4 h-4 ml-auto flex-none text-accent-blue" />}
            </div>
            <p class="mt-1 text-[12px] text-ink-300 leading-snug">{template.description}</p>
            <div class="mt-1.5 flex flex-wrap gap-1">
              {template.default && <TemplateTag label="default" />}
              {template.prebuiltImageAvailable && <TemplateTag label="prebuilt image" />}
              {template.defaultPorts?.map((port) => (
                <TemplateTag key={port} label={`:${port}`} mono />
              ))}
            </div>
          </button>
        );
      })}
    </div>
  );
}

function TemplateTag({ label, mono = false }: { label: string; mono?: boolean }) {
  return (
    <span
      class={`inline-flex items-center h-5 px-1.5 rounded bg-white/[0.06] text-[10.5px] text-ink-300 ${
        mono ? "font-mono" : ""
      }`}
    >
      {label}
    </span>
  );
}
