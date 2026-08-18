import type { JSX } from "preact";
import { useState } from "preact/hooks";
import type { ProjectSecret } from "../../../models/project";
import type {
  ProjectTemplateAdmin,
  ProjectTemplateStatus,
} from "../../../models/template";
import type { SecretsRecord } from "../../../state/projects/projectContainerRecords";
import { Copy, ExternalLink, Eye, EyeOff } from "../../primitives/icons";
import { Field, Grid, Panel } from "./ProjectContainerPrimitives";
import { formatEpochMillis } from "./projectContainerFormat";

/**
 * Small badge shown next to the project name. Templates that install nothing
 * ("blank") still get a badge so the field is never mysteriously absent.
 */
export function TemplateBadge({ template }: { template: ProjectTemplateStatus }) {
  return (
    <span
      class="inline-flex items-center h-5 px-1.5 rounded bg-white/[0.06] text-[11px] font-medium text-ink-200"
      title={`Project template: ${template.title || template.name}`}
    >
      {template.title || template.name}
    </span>
  );
}

/**
 * Provisioning progress badge. Only rendered for templates that actually
 * install something — "none" means the container was ready on first boot.
 */
export function TemplateStatusBadge({ status }: { status: ProjectTemplateStatus["status"] }) {
  if (status === "none") return null;
  const tone =
    status === "done"
      ? "text-accent-green bg-accent-green/[0.12]"
      : status === "failed"
      ? "text-accent-red bg-accent-red/[0.12]"
      : status === "running"
      ? "text-accent-blue bg-accent-blue/[0.12]"
      : "text-ink-300 bg-white/[0.06]";
  return (
    <span class={`inline-flex items-center h-5 px-1.5 rounded text-[11px] font-medium ${tone}`}>
      {statusLabel(status)}
    </span>
  );
}

export function statusLabel(status: ProjectTemplateStatus["status"]): string {
  switch (status) {
    case "pending":
      return "setup pending";
    case "running":
      return "setting up";
    case "done":
      return "setup done";
    case "failed":
      return "setup failed";
    default:
      return "";
  }
}

export function TemplatePanel({
  template,
  secrets,
}: {
  template: ProjectTemplateStatus;
  secrets?: SecretsRecord;
}) {
  const fields: JSX.Element[] = [
    <Field key="template" label="Template" value={template.title || template.name} />,
    <Field
      key="provisioning"
      label="Provisioning"
      value={template.status === "none" ? "not required" : statusLabel(template.status)}
      tone={template.status === "failed" ? "warn" : undefined}
    />,
    <Field key="started" label="Started" value={formatEpochMillis(template.startedAt)} mono />,
    <Field key="finished" label="Finished" value={formatEpochMillis(template.finishedAt)} mono />,
  ];
  if (template.logPath) {
    fields.push(
      <Field key="log" label="Log (in container)" value={template.logPath} mono />
    );
  }

  return (
    <Panel title="Template">
      <div>
        <Grid>{fields}</Grid>
        {template.admin && (
          <TemplateAdminAccess admin={template.admin} secrets={secrets} />
        )}
        {template.error && (
          <p class="mt-2 text-[12px] text-accent-red break-words">{template.error}</p>
        )}
        {template.status === "failed" && (
          <p class="mt-2 text-[12px] text-ink-300 leading-snug">
            Restart the project to retry: provisioning re-runs on every container
            convergence until it succeeds.
          </p>
        )}
      </div>
    </Panel>
  );
}

/**
 * Where to sign in to what the template installed. The password is never in
 * the status payload — only the name of the project secret holding it — so
 * revealing it reads the already-loaded secrets record instead.
 */
function TemplateAdminAccess({
  admin,
  secrets,
}: {
  admin: ProjectTemplateAdmin;
  secrets?: SecretsRecord;
}) {
  const [revealed, setRevealed] = useState(false);
  const [copied, setCopied] = useState(false);
  const password = findSecret(secrets?.data, admin.passwordSecret);

  const copyLogin = async () => {
    const lines = [admin.url];
    if (admin.user) lines.push(`user: ${admin.user}`);
    if (password) lines.push(`password: ${password}`);
    try {
      await navigator.clipboard.writeText(lines.join("\n"));
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard access can be denied; the credentials stay readable above.
      setCopied(false);
    }
  };

  return (
    <div class="mt-3 rounded-md border border-white/10 bg-white/[0.03] p-2.5">
      <div class="flex items-center justify-between gap-2">
        <span class="text-[12px] font-medium text-ink-200">{admin.label}</span>
        <button
          type="button"
          onClick={() => void copyLogin()}
          class="inline-flex items-center gap-1.5 h-7 px-2 rounded text-[11.5px] text-ink-200
                 hover:text-ink-50 hover:bg-white/[0.08]"
        >
          <Copy class="w-3.5 h-3.5" />
          {copied ? "Copied" : "Copy login"}
        </button>
      </div>
      <a
        href={admin.url}
        target="_blank"
        rel="noreferrer noopener"
        class="mt-1.5 inline-flex items-center gap-1.5 text-[12.5px] font-mono text-accent-blue
               hover:underline break-all"
      >
        {admin.url}
        <ExternalLink class="w-3.5 h-3.5 flex-none" />
      </a>
      <dl class="mt-2 grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 items-center text-[12px]">
        {admin.user && (
          <>
            <dt class="text-ink-400">user</dt>
            <dd class="font-mono text-ink-100 break-all">{admin.user}</dd>
          </>
        )}
        {admin.passwordSecret && (
          <>
            <dt class="text-ink-400">password</dt>
            <dd class="flex items-center gap-2 min-w-0">
              <span class="font-mono text-ink-100 break-all">
                {passwordText(password, revealed, secrets)}
              </span>
              {password && (
                <button
                  type="button"
                  onClick={() => setRevealed((shown) => !shown)}
                  class="h-6 w-6 flex-none grid place-items-center rounded text-ink-300
                         hover:text-ink-50 hover:bg-white/[0.08]"
                  aria-label={revealed ? "Hide password" : "Reveal password"}
                >
                  {revealed ? <EyeOff class="w-3.5 h-3.5" /> : <Eye class="w-3.5 h-3.5" />}
                </button>
              )}
            </dd>
          </>
        )}
      </dl>
      {admin.passwordSecret && (
        <p class="mt-2 text-[11.5px] text-ink-400 leading-snug">
          Stored as the{" "}
          <span class="font-mono">{admin.passwordSecret}</span> project secret. Change it
          on the Secrets tab and inside {admin.label} — this panel only reads the secret.
        </p>
      )}
    </div>
  );
}

function passwordText(
  password: string | undefined,
  revealed: boolean,
  secrets?: SecretsRecord
): string {
  if (password) return revealed ? password : "••••••••••••";
  if (secrets?.loading) return "loading…";
  return "not set";
}

function findSecret(list: ProjectSecret[] | undefined, key?: string): string | undefined {
  if (!key) return undefined;
  return list?.find((secret) => secret.key === key)?.value;
}
