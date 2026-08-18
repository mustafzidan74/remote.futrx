import type { ProjectPortal, UpdateProjectPortalInput } from "../../models/project";

/**
 * The client-portal form as the UI holds it. It is deliberately separate from
 * `ProjectPortal`: the server's view carries bookkeeping (timestamps, the
 * one-time URL) that a form must never round-trip back.
 */
export interface PortalFormState {
  enabled: boolean;
  showPreview: boolean;
  showChangelog: boolean;
  showUsage: boolean;
  brandTitle: string;
  note: string;
}

/** What a project with no stored portal starts from. */
export const DEFAULT_PORTAL_FORM: PortalFormState = {
  enabled: false,
  showPreview: true,
  showChangelog: true,
  showUsage: false,
  brandTitle: "",
  note: "",
};

/** Adopts a server response into form state, filling in the defaults. */
export function portalFormFrom(portal: ProjectPortal | null | undefined): PortalFormState {
  if (!portal) return { ...DEFAULT_PORTAL_FORM };
  return {
    enabled: portal.enabled,
    showPreview: portal.showPreview,
    showChangelog: portal.showChangelog,
    showUsage: portal.showUsage,
    brandTitle: portal.brandTitle ?? "",
    note: portal.note ?? "",
  };
}

/**
 * Builds the write payload. `overrides` is how the Enable, Rotate, and Disable
 * buttons express their intent without first mutating the form the operator is
 * still editing.
 */
export function portalUpdateInput(
  form: PortalFormState,
  overrides: { enabled?: boolean; rotate?: boolean } = {},
): UpdateProjectPortalInput {
  const enabled = overrides.enabled ?? form.enabled;
  return {
    enabled,
    // Rotating a portal that is off would mint a link nobody can use, so the
    // flag only survives while the portal stays enabled.
    rotate: enabled ? overrides.rotate === true : false,
    showPreview: form.showPreview,
    showChangelog: form.showChangelog,
    showUsage: form.showUsage,
    brandTitle: form.brandTitle.trim(),
    note: form.note,
  };
}

/** One-line summary for the section header. */
export function describePortal(portal: ProjectPortal | null | undefined, loading: boolean): string {
  if (loading && !portal) return "Loading the client portal…";
  if (!portal || !portal.enabled) return "Off — no one outside this project can see anything.";
  const sections = [
    portal.showPreview ? "preview links" : null,
    portal.showChangelog ? "recent changes" : null,
    portal.showUsage ? "activity" : null,
  ].filter((section): section is string => section !== null);
  if (sections.length === 0) return "On — showing the project name and status only.";
  return `On — showing ${sections.join(", ")}.`;
}
