import { useEffect, useState } from "preact/hooks";
import type { ProjectPortal } from "../../../models/project";
import type { PortalRecord } from "../../../state/projects/projectContainerRecords";
import {
  type PortalFormState,
  portalFormFrom,
} from "../../../state/projects/projectPortalState";
import { AlertCircle, Check, ExternalLink, Loader, X } from "../../primitives/icons";
import { Loading } from "./ProjectContainerPrimitives";

const TOGGLES: Array<{ key: keyof PortalFormState; label: string; hint: string }> = [
  {
    key: "showPreview",
    label: "Show preview links",
    hint: "Only ports that already have a live public preview link are listed — never the sign-in-gated preview.",
  },
  {
    key: "showChangelog",
    label: "Show recent changes",
    hint: "The last 15 commits in /workspace, grouped by day, without author emails.",
  },
  {
    key: "showUsage",
    label: "Show activity",
    hint: "How many agent runs happened in the last 7 days. Costs are never shown.",
  },
];

export function ProjectClientPortalSection({
  record,
  issuedUrl,
  onDismissIssuedUrl,
  onSave,
}: {
  record: PortalRecord;
  issuedUrl: string | null;
  onDismissIssuedUrl: () => void;
  onSave: (
    form: PortalFormState,
    overrides?: { enabled?: boolean; rotate?: boolean },
  ) => Promise<ProjectPortal>;
}) {
  const stored = record.data;
  const [form, setForm] = useState<PortalFormState>(() => portalFormFrom(stored));
  const [busy, setBusy] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  // The form is seeded from whatever the server last returned. Re-seeding on
  // identity change (not on every render) keeps the operator's in-progress
  // edits while still adopting a reload or a project switch.
  useEffect(() => {
    setForm(portalFormFrom(stored));
    setSaved(false);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [stored?.updatedAt, stored?.enabled]);

  const run = async (
    action: string,
    overrides: { enabled?: boolean; rotate?: boolean } = {},
  ) => {
    setBusy(action);
    setError(null);
    setSaved(false);
    try {
      await onSave(form, overrides);
      setSaved(true);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setBusy(null);
    }
  };

  if (record.loading && !stored) return <Loading text="Loading the client portal…" />;

  const enabled = stored?.enabled === true;

  return (
    <>
      {(record.error || error) && (
        <div class="flex items-start gap-2.5 rounded-lg border border-accent-red/30 bg-accent-red/[0.08] px-3 py-2.5 text-[13px]">
          <AlertCircle class="w-4 h-4 mt-0.5 flex-none text-accent-red" />
          <div class="text-accent-red break-words">{record.error ?? error}</div>
        </div>
      )}

      {issuedUrl && <IssuedPortalLink url={issuedUrl} onDismiss={onDismissIssuedUrl} />}

      <div class="space-y-2">
        {TOGGLES.map((toggle) => (
          <label
            key={toggle.key}
            class="flex items-start gap-2.5 rounded-md border border-white/10 bg-white/[0.03] p-2.5 cursor-pointer"
          >
            <input
              type="checkbox"
              checked={form[toggle.key] === true}
              onChange={(event) =>
                setForm((current) => ({
                  ...current,
                  [toggle.key]: (event.currentTarget as HTMLInputElement).checked,
                }))
              }
              class="mt-0.5 h-4 w-4 flex-none accent-accent-blue"
            />
            <span class="min-w-0">
              <span class="block text-[13px] text-ink-100">{toggle.label}</span>
              <span class="block text-[12px] text-ink-300 leading-relaxed">{toggle.hint}</span>
            </span>
          </label>
        ))}
      </div>

      <label class="block space-y-1.5">
        <span class="text-xs text-ink-300">Brand title (optional)</span>
        <input
          type="text"
          value={form.brandTitle}
          onInput={(event) =>
            setForm((current) => ({
              ...current,
              brandTitle: (event.currentTarget as HTMLInputElement).value,
            }))
          }
          placeholder="Shown instead of the project name"
          maxLength={80}
          autocomplete="off"
          class="w-full h-10 rounded-md bg-black/30 border border-white/10 px-3 text-sm text-ink-100
                 placeholder:text-ink-400 focus:outline-none focus:border-accent-blue"
        />
      </label>

      <label class="block space-y-1.5">
        <span class="text-xs text-ink-300">Note to the client (optional)</span>
        <textarea
          value={form.note}
          onInput={(event) =>
            setForm((current) => ({
              ...current,
              note: (event.currentTarget as HTMLTextAreaElement).value,
            }))
          }
          rows={4}
          maxLength={2000}
          placeholder="Plain text. Line breaks are kept; everything else is escaped."
          class="w-full rounded-md bg-black/30 border border-white/10 px-3 py-2 text-sm text-ink-100
                 placeholder:text-ink-400 focus:outline-none focus:border-accent-blue resize-y"
        />
      </label>

      {saved && !error && (
        <div class="text-xs text-accent-green">Client portal settings saved.</div>
      )}

      <div class="flex flex-wrap items-center gap-2">
        {enabled ? (
          <>
            <button
              type="button"
              onClick={() => void run("save")}
              disabled={busy !== null}
              class="h-10 px-3 rounded-md bg-accent-blue text-ink-900 hover:bg-accent-blue/85 text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
            >
              {busy === "save" && <Loader class="w-3.5 h-3.5 animate-spin" />}
              Save portal settings
            </button>
            <button
              type="button"
              onClick={() => {
                if (!confirm("Rotate the link? The one the client has stops working.")) return;
                void run("rotate", { enabled: true, rotate: true });
              }}
              disabled={busy !== null}
              class="h-10 px-3 rounded-md border border-white/10 text-ink-100 hover:bg-white/[0.06] text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
            >
              {busy === "rotate" && <Loader class="w-3.5 h-3.5 animate-spin" />}
              Rotate link
            </button>
            <button
              type="button"
              onClick={() => {
                if (!confirm("Disable the client portal? The link stops working immediately."))
                  return;
                void run("disable", { enabled: false });
              }}
              disabled={busy !== null}
              class="h-10 px-3 rounded-md border border-white/10 text-ink-300 hover:text-accent-red hover:bg-white/[0.06] text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
            >
              {busy === "disable" && <Loader class="w-3.5 h-3.5 animate-spin" />}
              Disable
            </button>
          </>
        ) : (
          <button
            type="button"
            onClick={() => void run("enable", { enabled: true })}
            disabled={busy !== null}
            class="h-10 px-3 rounded-md bg-accent-blue text-ink-900 hover:bg-accent-blue/85 text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
          >
            {busy === "enable" && <Loader class="w-3.5 h-3.5 animate-spin" />}
            Enable client portal
          </button>
        )}
      </div>

      <p class="text-[11.5px] text-ink-400 leading-relaxed">
        The portal is a read-only summary page on this server: project name, status, the preview
        ports that already have a live public link, recent commit subjects, and your note. It never
        exposes the workspace, the IDE, the agent browser, chats, or secrets. Anyone holding the
        link can open it, so treat it like a password — the link is shown once and the server keeps
        only a hash of it.
      </p>
    </>
  );
}

function IssuedPortalLink({ url, onDismiss }: { url: string; onDismiss: () => void }) {
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(url);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      setCopied(false);
    }
  };

  return (
    <div class="rounded-md border border-accent-blue/30 bg-accent-blue/[0.08] px-3 py-2.5 space-y-2">
      <div class="flex items-center gap-2">
        <ExternalLink class="w-4 h-4 flex-none text-accent-blue" />
        <div class="text-[12.5px] font-semibold text-ink-50">
          Client portal link — copy it now
        </div>
        <button
          type="button"
          onClick={onDismiss}
          class="h-7 w-7 ml-auto rounded text-ink-300 hover:text-ink-50 hover:bg-white/[0.08] grid place-items-center"
          aria-label="Hide link"
          title="Hide link"
        >
          <X class="w-3.5 h-3.5" />
        </button>
      </div>
      <div class="flex items-start gap-2">
        <code class="flex-1 min-w-0 text-[12px] font-mono text-ink-100 break-all">{url}</code>
        <button
          type="button"
          onClick={copy}
          class="h-8 px-2.5 rounded-md bg-accent-blue text-ink-900 hover:bg-accent-blue/85 text-[12px] font-medium inline-flex items-center gap-1.5 flex-none"
        >
          {copied ? <Check class="w-3.5 h-3.5" /> : null}
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
      <div class="text-[11.5px] text-ink-300">
        This is the only time the link is shown. Closing this leaves the portal open — rotate or
        disable it below if the link was not copied.
      </div>
    </div>
  );
}
