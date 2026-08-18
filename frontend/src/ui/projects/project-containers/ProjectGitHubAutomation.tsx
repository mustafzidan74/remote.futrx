import { useEffect, useState } from "preact/hooks";
import type { GitHubDelivery, GitHubSettings } from "../../../models/github";
import {
  deliveryTone,
  describeDelivery,
  showsUntrustedInputWarning,
} from "../../../state/projects/githubPanelState";
import { AlertCircle, Check, Copy, ExternalLink, Loader, X } from "../../primitives/icons";
import { ChecksBadge } from "./ProjectGitHubSection";
import { formatRelativeTime } from "./projectContainerFormat";

/**
 * The inbound half of the GitHub panel: the webhook endpoint, what may trigger
 * a run, and what has arrived.
 *
 * The warning above the automatic-runs switch is not boilerplate. Arming it
 * means the body of any issue somebody with write access labels — text this
 * platform did not write and cannot vet — becomes the prompt of an agent
 * running as root inside the container. The switch is therefore
 * administrator-only, defaults off, and says exactly that.
 */
export function ProjectGitHubAutomation({
  settings,
  issuedSecret,
  busy,
  linked,
  isAdmin,
  onDismissIssuedSecret,
  onSave,
}: {
  settings: GitHubSettings | undefined;
  issuedSecret: string | null;
  busy: boolean;
  linked: boolean;
  isAdmin: boolean;
  onDismissIssuedSecret: () => void;
  onSave: (input: {
    label?: string;
    autoRun?: boolean;
    commentBack?: boolean;
    rotate?: boolean;
    disable?: boolean;
  }) => Promise<GitHubSettings>;
}) {
  const [label, setLabel] = useState(settings?.label ?? "");
  const [error, setError] = useState<string | null>(null);
  const [action, setAction] = useState<string | null>(null);

  // Re-seed the label field when the server's copy changes identity, so a
  // reload or a project switch is adopted without discarding an in-progress
  // edit on every render.
  useEffect(() => {
    setLabel(settings?.label ?? "");
  }, [settings?.updatedAt, settings?.label]);

  if (!linked) {
    return (
      <p class="text-[12.5px] text-ink-300 leading-relaxed">
        Link a repository first. The webhook endpoint is per project, and it can only accept
        deliveries for the repository this project is linked to.
      </p>
    );
  }

  const run = async (
    name: string,
    input: Parameters<typeof onSave>[0],
  ) => {
    setAction(name);
    setError(null);
    try {
      await onSave(input);
    } catch (cause) {
      setError((cause as Error).message);
    } finally {
      setAction(null);
    }
  };

  const configured = settings?.webhookConfigured === true;

  return (
    <>
      {error && (
        <div class="flex items-start gap-2.5 rounded-lg border border-accent-red/30 bg-accent-red/[0.08] px-3 py-2.5 text-[13px]">
          <AlertCircle class="w-4 h-4 mt-0.5 flex-none text-accent-red" />
          <div class="text-accent-red break-words">{error}</div>
        </div>
      )}

      {issuedSecret && (
        <IssuedSecret secret={issuedSecret} onDismiss={onDismissIssuedSecret} />
      )}

      {configured && settings?.webhookUrl && (
        <CopyableValue label="Payload URL" value={settings.webhookUrl} />
      )}

      <label class="block space-y-1.5">
        <span class="text-xs text-ink-300">Trigger label</span>
        <input
          type="text"
          value={label}
          onInput={(event) => setLabel((event.currentTarget as HTMLInputElement).value)}
          placeholder="remote-agent"
          maxLength={64}
          autocomplete="off"
          spellcheck={false}
          class="w-full h-10 rounded-md bg-black/30 border border-white/10 px-3 text-sm text-ink-100
                 placeholder:text-ink-400 focus:outline-none focus:border-accent-blue font-mono"
        />
        <span class="block text-[11.5px] text-ink-400 leading-relaxed">
          An issue carrying this label — at creation or when it is added later — is a request for
          work. GitHub ignores labels set by accounts without write access, which is what makes the
          label an authorization and not just a marker.
        </span>
      </label>

      <label class="flex items-start gap-2.5 rounded-md border border-white/10 bg-white/[0.03] p-2.5 cursor-pointer">
        <input
          type="checkbox"
          checked={settings?.commentBack === true}
          disabled={busy || !configured}
          onChange={(event) =>
            void run("commentBack", {
              commentBack: (event.currentTarget as HTMLInputElement).checked,
            })
          }
          class="mt-0.5 h-4 w-4 flex-none accent-accent-blue"
        />
        <span class="min-w-0">
          <span class="block text-[13px] text-ink-100">Comment back on the issue</span>
          <span class="block text-[12px] text-ink-300 leading-relaxed">
            When a triggered run finishes, post a comment on the issue with a link to the chat.
            Needs a token with write access to issues.
          </span>
        </span>
      </label>

      {showsUntrustedInputWarning(settings) && <UntrustedInputWarning />}

      <label
        class={`flex items-start gap-2.5 rounded-md border p-2.5 ${
          isAdmin
            ? "border-white/10 bg-white/[0.03] cursor-pointer"
            : "border-white/[0.06] bg-white/[0.02] opacity-70"
        }`}
      >
        <input
          type="checkbox"
          checked={settings?.autoRun === true}
          disabled={busy || !isAdmin}
          onChange={(event) =>
            void run("autoRun", { autoRun: (event.currentTarget as HTMLInputElement).checked })
          }
          class="mt-0.5 h-4 w-4 flex-none accent-accent-red"
        />
        <span class="min-w-0">
          <span class="block text-[13px] text-ink-100">
            Start agent runs automatically {isAdmin ? "" : "(administrators only)"}
          </span>
          <span class="block text-[12px] text-ink-300 leading-relaxed">
            Off by default. While it is off, a matching event still creates the chat and sends a
            notification — it just does not run anything until a human presses send.
          </span>
        </span>
      </label>

      <div class="flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={() => void run("save", { label: label.trim() })}
          disabled={busy}
          class="h-10 px-3 rounded-md border border-white/10 text-ink-100 hover:bg-white/[0.06] text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
        >
          {action === "save" && <Loader class="w-3.5 h-3.5 animate-spin" />}
          Save label
        </button>
        <button
          type="button"
          onClick={() => {
            if (configured && !confirm("Generate a new secret? The old one stops verifying immediately.")) {
              return;
            }
            void run("rotate", { rotate: true });
          }}
          disabled={busy}
          class="h-10 px-3 rounded-md bg-accent-blue text-ink-900 hover:bg-accent-blue/85 text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
        >
          {action === "rotate" && <Loader class="w-3.5 h-3.5 animate-spin" />}
          {configured ? "Rotate secret" : "Generate webhook secret"}
        </button>
        {configured && (
          <button
            type="button"
            onClick={() => {
              if (!confirm("Disable the webhook? Every delivery is refused from that moment on."))
                return;
              void run("disable", { disable: true });
            }}
            disabled={busy}
            class="h-10 px-3 rounded-md border border-white/10 text-ink-300 hover:text-accent-red hover:bg-white/[0.06] text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
          >
            {action === "disable" && <Loader class="w-3.5 h-3.5 animate-spin" />}
            Disable webhook
          </button>
        )}
      </div>

      <DeliveryLog deliveries={settings?.deliveries ?? []} />
    </>
  );
}

function UntrustedInputWarning() {
  return (
    <div class="flex items-start gap-2.5 rounded-lg border border-accent-red/30 bg-accent-red/[0.08] px-3 py-2.5 text-[12.5px]">
      <AlertCircle class="w-4 h-4 mt-0.5 flex-none text-accent-red" />
      <div class="text-accent-red leading-relaxed">
        <strong>Automatic runs are armed.</strong> An issue body, a{" "}
        <code class="font-mono">/remote</code> comment, or a review written by anyone with write
        access to the repository becomes the prompt of an agent running as root in this project's
        container. The prompt fences that text as untrusted, but no fence is perfect: only arm this
        for repositories whose collaborators you trust with this container.
      </div>
    </div>
  );
}

function IssuedSecret({ secret, onDismiss }: { secret: string; onDismiss: () => void }) {
  return (
    <div class="rounded-lg border border-accent-green/30 bg-accent-green/[0.08] px-3 py-2.5 space-y-2">
      <div class="flex items-start gap-2">
        <Check class="w-4 h-4 mt-0.5 flex-none text-accent-green" />
        <div class="flex-1 text-[12.5px] text-accent-green leading-relaxed">
          Paste this into the repository's webhook <strong>Secret</strong> field. It is shown once
          and never again — the server keeps it so it can verify signatures, but this panel will
          not repeat it.
        </div>
        <button
          type="button"
          onClick={onDismiss}
          class="h-7 w-7 rounded-md text-ink-300 hover:text-ink-50 hover:bg-white/[0.08] grid place-items-center flex-none"
          aria-label="Dismiss"
        >
          <X class="w-3.5 h-3.5" />
        </button>
      </div>
      <CopyableValue label="Secret" value={secret} />
      <div class="text-[11.5px] text-ink-300 leading-relaxed">
        In GitHub: <em>Settings → Webhooks → Add webhook</em>. Content type{" "}
        <code class="font-mono">application/json</code>; send the <em>Issues</em>,{" "}
        <em>Issue comments</em> and <em>Pull request reviews</em> events.
      </div>
    </div>
  );
}

function CopyableValue({ label, value }: { label: string; value: string }) {
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(value);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      setCopied(false);
    }
  };

  return (
    <div class="rounded-md border border-white/10 bg-black/30 px-3 py-2">
      <div class="text-[11px] text-ink-400">{label}</div>
      <div class="mt-0.5 flex items-center gap-2 min-w-0">
        <code class="flex-1 min-w-0 truncate font-mono text-[12.5px] text-ink-100" title={value}>
          {value}
        </code>
        <button
          type="button"
          onClick={() => void copy()}
          class="h-7 px-2 rounded-md border border-white/10 text-ink-300 hover:text-ink-50 hover:bg-white/[0.08] text-[11.5px] inline-flex items-center gap-1.5 flex-none"
        >
          {copied ? <Check class="w-3 h-3" /> : <Copy class="w-3 h-3" />}
          {copied ? "Copied" : "Copy"}
        </button>
      </div>
    </div>
  );
}

function DeliveryLog({ deliveries }: { deliveries: GitHubDelivery[] }) {
  if (deliveries.length === 0) {
    return (
      <p class="text-[11.5px] text-ink-400 leading-relaxed">
        No deliveries recorded yet. Every accepted delivery is listed here — including the ones no
        rule matched, with the reason — and every one is written to the audit log.
      </p>
    );
  }
  return (
    <div class="space-y-1.5">
      <div class="text-[11.5px] font-semibold uppercase tracking-wide text-ink-300">
        Recent deliveries
      </div>
      {deliveries.map((delivery) => (
        <DeliveryRow key={delivery.id + String(delivery.at)} delivery={delivery} />
      ))}
    </div>
  );
}

function DeliveryRow({ delivery }: { delivery: GitHubDelivery }) {
  return (
    <div class="rounded-md border border-white/[0.08] bg-white/[0.03] px-3 py-2">
      <div class="flex items-center gap-2 flex-wrap min-w-0">
        <ChecksBadge tone={deliveryTone(delivery)} text={delivery.outcome} />
        <span class="text-[11.5px] font-mono text-ink-400">{delivery.event}</span>
        {delivery.sender && (
          <span class="text-[11.5px] text-ink-400">@{delivery.sender}</span>
        )}
        <span class="flex-1" />
        <span class="text-[11px] text-ink-400">{formatRelativeTime(delivery.at)}</span>
      </div>
      <div class="mt-0.5 text-[12.5px] text-ink-200 break-words">{describeDelivery(delivery)}</div>
      {delivery.url && (
        <a
          href={delivery.url}
          target="_blank"
          rel="noopener noreferrer"
          class="mt-0.5 inline-flex items-center gap-1 text-[11.5px] text-ink-400 hover:text-accent-blue"
        >
          Open on GitHub
          <ExternalLink class="w-3 h-3" />
        </a>
      )}
    </div>
  );
}
