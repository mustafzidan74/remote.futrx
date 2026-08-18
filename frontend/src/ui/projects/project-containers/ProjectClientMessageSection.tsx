import { useState } from "preact/hooks";
import type { ProjectMeta } from "../../../models/project";
import { useClientMessage } from "../../../state/hooks/projects/useClientMessage";
import { AlertCircle, Check, Copy, Loader, Send } from "../../primitives/icons";

/**
 * "Message client": pick one of your own client templates, choose Arabic or
 * English, and take the resolved text wherever the client actually is.
 *
 * The panel never sends anything to the client by itself. It copies, it opens
 * a mail draft, it hands the text to the notification sinks you already set
 * up, and it can publish the same words on the project's portal page — every
 * one of them an existing channel, and every one of them explicit.
 */
export function ProjectClientMessageSection({
  project,
  portalUrl,
  previewUrl,
  portalEnabled,
  onPublishNote,
}: {
  project: ProjectMeta;
  /** The portal link, known only in the session that minted it. */
  portalUrl?: string | null;
  previewUrl?: string | null;
  portalEnabled: boolean;
  onPublishNote: (note: string) => Promise<unknown>;
}) {
  const message = useClientMessage({ project, portalUrl, previewUrl, onPublishNote });
  const [copied, setCopied] = useState(false);

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(message.text);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      setCopied(false);
    }
  };

  const hasText = message.text.trim().length > 0;

  return (
    <div class="space-y-3">
      {message.error && (
        <div class="flex items-start gap-2.5 rounded-lg border border-accent-red/30 bg-accent-red/[0.08] px-3 py-2.5 text-[13px]">
          <AlertCircle class="w-4 h-4 mt-0.5 flex-none text-accent-red" />
          <div class="text-accent-red break-words">{message.error}</div>
        </div>
      )}

      <div class="flex flex-wrap items-end gap-2">
        <label class="min-w-[200px] flex-1 space-y-1.5">
          <span class="text-xs text-ink-300">Template</span>
          <select
            value={message.selectedId ?? ""}
            onChange={(event) => message.select((event.currentTarget as HTMLSelectElement).value)}
            disabled={message.loading || message.templates.length === 0}
            class="w-full h-10 rounded-md bg-black/30 border border-white/10 px-2.5 text-sm text-ink-100
                   focus:outline-none focus:border-accent-blue disabled:opacity-50"
          >
            <option value="">
              {message.loading
                ? "Loading your templates…"
                : message.templates.length === 0
                  ? "No client templates yet"
                  : "Pick a template"}
            </option>
            {message.templates.map((template) => (
              <option key={template.id} value={template.id}>
                {template.title}
              </option>
            ))}
          </select>
        </label>

        <div class="flex-none space-y-1.5">
          <span class="block text-xs text-ink-300">Language</span>
          <div class="inline-flex h-10 overflow-hidden rounded-md border border-white/10">
            {(["en", "ar"] as const).map((code) => (
              <button
                key={code}
                type="button"
                onClick={() => message.setLanguage(code)}
                class={`h-full px-3 text-[13px] ${
                  message.language === code
                    ? "bg-accent-blue text-ink-900 font-medium"
                    : "text-ink-200 hover:bg-white/[0.06]"
                }`}
              >
                {code === "en" ? "English" : "العربية"}
              </button>
            ))}
          </div>
        </div>
      </div>

      <label class="block space-y-1.5">
        <span class="text-xs text-ink-300">Message</span>
        <textarea
          value={message.text}
          onInput={(event) => message.setText((event.currentTarget as HTMLTextAreaElement).value)}
          rows={8}
          dir="auto"
          placeholder="Pick a template, or write the message here."
          class="w-full rounded-md bg-black/30 border border-white/10 px-3 py-2 text-sm text-ink-100
                 placeholder:text-ink-400 focus:outline-none focus:border-accent-blue resize-y"
        />
      </label>

      {message.unresolved.length > 0 && (
        <div class="text-[12px] text-amber-300">
          Still to fill in: {message.unresolved.join(", ")}
        </div>
      )}

      <div class="flex flex-wrap items-center gap-2">
        <button
          type="button"
          onClick={() => void copy()}
          disabled={!hasText}
          class="h-10 px-3 rounded-md border border-white/10 text-ink-100 hover:bg-white/[0.06]
                 text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
        >
          {copied ? <Check class="w-3.5 h-3.5" /> : <Copy class="w-3.5 h-3.5" />}
          {copied ? "Copied" : "Copy"}
        </button>

        <a
          href={hasText ? message.mailtoHref() : undefined}
          class={`h-10 px-3 rounded-md border border-white/10 text-[13px] font-medium inline-flex items-center gap-2 ${
            hasText ? "text-ink-100 hover:bg-white/[0.06]" : "pointer-events-none opacity-50 text-ink-300"
          }`}
        >
          Open in email
        </a>

        <button
          type="button"
          onClick={() => void message.send()}
          disabled={!hasText || !message.canSend || message.sending}
          class="h-10 px-3 rounded-md bg-accent-blue text-ink-900 hover:bg-accent-blue/85
                 text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
          title={
            message.canSend
              ? "Send through the WhatsApp or Telegram sink configured in Settings → Notifications"
              : "No notification sink is configured on this server"
          }
        >
          {message.sending ? (
            <Loader class="w-3.5 h-3.5 animate-spin" />
          ) : (
            <Send class="w-3.5 h-3.5" />
          )}
          Send to my sink
        </button>

        <button
          type="button"
          onClick={() => void message.publishToPortal()}
          disabled={!hasText || message.publishing}
          class="h-10 px-3 rounded-md border border-white/10 text-ink-100 hover:bg-white/[0.06]
                 text-[13px] font-medium disabled:opacity-50 inline-flex items-center gap-2"
          title="Show this message on the client portal page"
        >
          {message.publishing && <Loader class="w-3.5 h-3.5 animate-spin" />}
          Show on the portal
        </button>
      </div>

      {message.published && (
        <div class="text-xs text-accent-green">
          {portalEnabled
            ? "The portal page now shows this message."
            : "Saved. It appears once you enable the client portal above."}
        </div>
      )}

      {message.delivered && (
        <div class="rounded-md border border-white/10 bg-white/[0.03] px-3 py-2 text-[12px] text-ink-200">
          {message.delivered.length === 0 ? (
            <span>Nothing was configured to receive it.</span>
          ) : (
            message.delivered.map((row) => (
              <div key={row.sink}>
                {row.sink}: {row.delivered ? "sent" : row.error || "failed"}
              </div>
            ))
          )}
        </div>
      )}

      <p class="text-[11.5px] text-ink-400 leading-relaxed">
        Templates come from your own snippet library — the same one the composer's Snippets menu
        edits — so anything you save there is available here. Placeholders are filled in from this
        project; anything left visible needs you. "Send to my sink" delivers the text to the
        Telegram or WhatsApp destination configured under Settings → Notifications, which is your
        own channel, not the client's.
      </p>
    </div>
  );
}
