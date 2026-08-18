/**
 * Project templates ("stack presets"). A template is the shared base image
 * plus a one-time in-container provisioning step, so choosing one at project
 * creation is the only place the stack is decided — it cannot be changed
 * afterwards.
 */
/** One choice of a `select` template input. */
export interface TemplateInputOption {
  value: string;
  label: string;
}

/**
 * One operator-supplied value a template collects in the new-project dialog.
 * The backend derives the provisioning environment variable from `key`
 * (`adminPassword` becomes `TPL_ADMIN_PASSWORD`), so the key is also the name
 * used in the create request's `templateInputs`.
 */
export interface TemplateInput {
  key: string;
  label: string;
  type: "text" | "email" | "password" | "select" | "checkbox";
  required?: boolean;
  default?: string;
  options?: TemplateInputOption[];
  help?: string;
  /**
   * A default only the server knows. The dialog prefills the same value so
   * the form shows what will actually be used.
   */
  defaultFrom?: "projectName" | "userEmail";
  /** True when the value is stored as a project secret, never in metadata. */
  secret?: boolean;
  /** The project-secret key a secret input is stored under. */
  secretName?: string;
  /** True when leaving a secret empty makes the server generate one. */
  generate?: boolean;
}

export interface ProjectTemplate {
  name: string;
  title: string;
  description: string;
  /** Stable key mapped to a glyph by the UI; unknown keys fall back. */
  icon: string;
  defaultPorts?: number[];
  /** True for the template assigned when none is requested. */
  default: boolean;
  /** False when the template installs nothing (the container starts at once). */
  provisions: boolean;
  /** Alias of the dedicated pre-built image, when the template declares one. */
  prebuiltImage?: string;
  /** True when that image is published on this host (fast first start). */
  prebuiltImageAvailable: boolean;
  /** Values the dialog collects for this template. Absent for most. */
  inputs?: TemplateInput[];
}

/** How far a project's one-time template provisioning has got. */
export type TemplateProvisionStatus =
  | "none"
  | "pending"
  | "running"
  | "done"
  | "failed";

export interface ProjectTemplateStatus {
  name: string;
  title?: string;
  status: TemplateProvisionStatus;
  error?: string;
  logPath?: string;
  startedAt?: number;
  finishedAt?: number;
  /** Where to sign in to what the template installed. Only once done. */
  admin?: ProjectTemplateAdmin;
}

/**
 * Admin sign-in for a provisioned template. It names the project secret
 * holding the password; the value is read from the Secrets endpoint, so
 * revealing it stays an explicit, audited action.
 */
export interface ProjectTemplateAdmin {
  label: string;
  url: string;
  user?: string;
  passwordSecret?: string;
}
