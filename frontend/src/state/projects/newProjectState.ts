import type { ProjectTemplate, TemplateInput } from "../../models/template";

/**
 * State of the new-project dialog. The template is chosen here and nowhere
 * else: a project's stack preset is immutable once its container exists, so
 * this dialog is the whole decision surface.
 *
 * A template may also declare inputs (site title, language, feature toggles).
 * They are rendered under the picker and submitted alongside the name, because
 * they too are only settable at creation: provisioning consumes them once.
 */
export interface NewProjectState {
  open: boolean;
  name: string;
  /** Selected template name; "" until the catalog loads. */
  template: string;
  templates: ProjectTemplate[];
  templatesLoading: boolean;
  templatesError: string;
  /** Form values for the selected template's inputs, keyed by input key. */
  inputs: Record<string, string>;
  /**
   * Keys the operator has edited. A prefilled default follows the project
   * name until it is edited, and an untouched field shows no error.
   */
  touched: Record<string, boolean>;
  submitting: boolean;
  error: string;
}

export type NewProjectAction =
  | { type: "open" }
  | { type: "close" }
  | { type: "set-name"; name: string }
  | { type: "select-template"; template: string }
  | { type: "set-input"; key: string; value: string }
  | { type: "templates-loading" }
  | { type: "templates-loaded"; templates: ProjectTemplate[] }
  | { type: "templates-failed"; error: string }
  | { type: "submit" }
  | { type: "submit-failed"; error: string };

/** The template assumed before the catalog answers, and if it omits a default. */
export const DEFAULT_TEMPLATE = "blank";

/**
 * Deliberately permissive: the server validates the address properly. This
 * only catches the obvious typo before a round trip.
 */
const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;

class NewProjectStateTransitions {
  createInitial(): NewProjectState {
    return {
      open: false,
      name: "",
      template: DEFAULT_TEMPLATE,
      templates: [],
      templatesLoading: false,
      templatesError: "",
      inputs: {},
      touched: {},
      submitting: false,
      error: "",
    };
  }

  readonly reduce = (
    state: NewProjectState,
    action: NewProjectAction
  ): NewProjectState => {
    switch (action.type) {
      case "open": {
        // Reopening starts from a clean form but keeps the already-loaded
        // catalog, so the picker does not flash on every open.
        const template = this.defaultTemplate(state.templates);
        return {
          ...state,
          open: true,
          name: "",
          error: "",
          submitting: false,
          template,
          inputs: this.defaultInputs(state.templates, template, ""),
          touched: {},
        };
      }
      case "close":
        return { ...state, open: false, submitting: false, error: "" };
      case "set-name":
        return {
          ...state,
          name: action.name,
          error: "",
          inputs: this.withNameDefaults(state, action.name),
        };
      case "select-template":
        return {
          ...state,
          template: action.template,
          error: "",
          inputs: this.defaultInputs(state.templates, action.template, state.name),
          touched: {},
        };
      case "set-input":
        return {
          ...state,
          error: "",
          inputs: { ...state.inputs, [action.key]: action.value },
          touched: { ...state.touched, [action.key]: true },
        };
      case "templates-loading":
        return { ...state, templatesLoading: true, templatesError: "" };
      case "templates-loaded": {
        const template = this.selectedOrDefault(state.template, action.templates);
        return {
          ...state,
          templatesLoading: false,
          templatesError: "",
          templates: action.templates,
          template,
          inputs: this.defaultInputs(action.templates, template, state.name),
          touched: {},
        };
      }
      case "templates-failed":
        // A failed catalog must not block project creation: the form falls
        // back to the default template, which is what every project got
        // before templates existed.
        return {
          ...state,
          templatesLoading: false,
          templatesError: action.error,
          templates: [],
          template: DEFAULT_TEMPLATE,
          inputs: {},
          touched: {},
        };
      case "submit":
        return { ...state, submitting: true, error: "" };
      case "submit-failed":
        return { ...state, submitting: false, error: action.error };
    }
  };

  /** The template a freshly opened dialog starts on. */
  defaultTemplate(templates: ProjectTemplate[]): string {
    return templates.find((template) => template.default)?.name ?? DEFAULT_TEMPLATE;
  }

  /** Keeps a still-valid selection, otherwise falls back to the default. */
  selectedOrDefault(selected: string, templates: ProjectTemplate[]): string {
    if (templates.some((template) => template.name === selected)) return selected;
    return this.defaultTemplate(templates);
  }

  /** Trimmed project name, which is what gets submitted. */
  submittedName(state: NewProjectState): string {
    return state.name.trim();
  }

  canSubmit(state: NewProjectState): boolean {
    if (state.submitting || this.submittedName(state).length === 0) return false;
    return this.inputs(state).every((input) => this.inputError(state, input) === "");
  }

  selectedTemplate(state: NewProjectState): ProjectTemplate | undefined {
    return state.templates.find((template) => template.name === state.template);
  }

  /** The selected template's declared inputs, in declaration order. */
  inputs(state: NewProjectState): TemplateInput[] {
    return this.selectedTemplate(state)?.inputs ?? [];
  }

  /** The current form value of one input. */
  inputValue(state: NewProjectState, input: TemplateInput): string {
    return state.inputs[input.key] ?? "";
  }

  /**
   * Why this input is not acceptable, or "" when it is. An empty value is
   * only an error when nothing downstream can fill it: a declared default, a
   * server-side default, or the project name all count as filled.
   */
  inputError(state: NewProjectState, input: TemplateInput): string {
    const value = this.inputValue(state, input).trim();
    if (value === "") {
      if (!input.required || this.hasFallback(input)) return "";
      return `${input.label} is required.`;
    }
    if (input.type === "email" && !EMAIL_PATTERN.test(value)) {
      return `${input.label} must be an email address.`;
    }
    if (input.type === "select" && !input.options?.some((option) => option.value === value)) {
      return `${input.label} must be one of the offered options.`;
    }
    return "";
  }

  /** The error to render under a field: only once the operator has edited it. */
  visibleInputError(state: NewProjectState, input: TemplateInput): string {
    if (!state.touched[input.key]) return "";
    return this.inputError(state, input);
  }

  /**
   * The `templateInputs` body of the create request. Empty values are omitted
   * so the server applies its own default (the creating user's email, or a
   * generated admin password) rather than being handed a blank.
   */
  submittedInputs(state: NewProjectState): Record<string, string> {
    const out: Record<string, string> = {};
    for (const input of this.inputs(state)) {
      const value = this.inputValue(state, input);
      if (input.type === "checkbox") {
        out[input.key] = value === "true" ? "true" : "false";
        continue;
      }
      const normalized = input.type === "password" ? value : value.trim();
      if (normalized !== "") out[input.key] = normalized;
    }
    return out;
  }

  /**
   * One-line warning about how long the first start will take. Only a
   * provisioning template without a published image pays the slow path.
   */
  firstStartNotice(state: NewProjectState): string {
    const template = this.selectedTemplate(state);
    if (!template || !template.provisions) return "";
    if (template.prebuiltImageAvailable) {
      return "This host has a prebuilt image for this template, so the container starts immediately.";
    }
    return "The stack is installed inside the container on first start, which can take several minutes.";
  }

  /** True when something other than the operator can supply a missing value. */
  private hasFallback(input: TemplateInput): boolean {
    return Boolean(input.default) || Boolean(input.defaultFrom) || Boolean(input.generate);
  }

  private defaultInputs(
    templates: ProjectTemplate[],
    templateName: string,
    projectName: string
  ): Record<string, string> {
    const template = templates.find((candidate) => candidate.name === templateName);
    const out: Record<string, string> = {};
    for (const input of template?.inputs ?? []) {
      out[input.key] = this.defaultValue(input, projectName);
    }
    return out;
  }

  /**
   * Re-derives the inputs that mirror the project name. Only untouched ones
   * follow along: once the site title has been edited it is the operator's.
   */
  private withNameDefaults(state: NewProjectState, projectName: string): Record<string, string> {
    const out = { ...state.inputs };
    for (const input of this.inputs(state)) {
      if (input.defaultFrom !== "projectName" || state.touched[input.key]) continue;
      out[input.key] = projectName.trim();
    }
    return out;
  }

  /**
   * The value a field starts on. `userEmail` is deliberately left blank: the
   * browser does not know the session's address, and the server fills it in.
   */
  private defaultValue(input: TemplateInput, projectName: string): string {
    if (input.type === "checkbox") return input.default === "true" ? "true" : "false";
    if (input.default) return input.default;
    if (input.defaultFrom === "projectName") return projectName.trim();
    return "";
  }
}

export const newProjectState = new NewProjectStateTransitions();
