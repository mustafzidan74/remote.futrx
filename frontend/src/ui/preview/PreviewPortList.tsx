import { useReducer } from "preact/hooks";
import { PUBLIC_HOSTNAME } from "../../config/runtime.ts";
import type { ProjectMeta } from "../../models/project";
import type { ProjectPreviewLinks } from "../../state/hooks/projects/useProjectPreviewLinks.ts";
import { useAgentBrowserOpener } from "../../state/hooks/projects/useAgentBrowserOpener.ts";
import { useProjectScreenshot } from "../../state/hooks/projects/useProjectScreenshot.ts";
import {
  PREVIEW_SHARE_TTL_HOURS,
  canOpenInAgentBrowser,
  isPreviewLinkBusy,
  isPreviewLinkDone,
  issuedShareUrl,
  previewLinkError,
  platformPortLabel,
  previewLinkFeedbackInitial,
  previewLinkFeedbackReduce,
  type PreviewPortRow,
} from "../../state/projects/projectPreviewLinksState.ts";
import { buildProjectPreviewUrl } from "../../shared/projectPreviewUrls.ts";
import { ScreenshotCard } from "./ScreenshotCard";
import {
  AlertCircle,
  Camera,
  Check,
  Copy,
  ExternalLink,
  Link2,
  Loader,
  Monitor,
  RotateCcw,
} from "../primitives/icons";

/**
 * The body shared by the sidebar Preview popover and the chat header chip's
 * dropdown: one row per listening port, with open / copy / share actions.
 *
 * `onAgentBrowserOpened` is what the chat header passes to reveal the Agent
 * Browser pane. The sidebar has no chat to reveal it in, so it omits the
 * callback and the row reports the result in place instead.
 */
export function PreviewPortList({
  project,
  links,
  onAgentBrowserOpened,
}: {
  project: ProjectMeta;
  links: ProjectPreviewLinks;
  onAgentBrowserOpened?: (port: number) => void;
}) {
  const [feedback, dispatch] = useReducer(
    previewLinkFeedbackReduce,
    previewLinkFeedbackInitial,
  );
  const agentBrowser = useAgentBrowserOpener({
    projectId: project.id,
    onOpened: onAgentBrowserOpened,
  });
  const screenshot = useProjectScreenshot(project.id);

  async function copyUrl(port: number, url: string) {
    dispatch({ type: "start", action: "copy", port });
    if (await writeToClipboard(url)) {
      dispatch({ type: "done", action: "copy", port, url, copied: true });
      return;
    }
    dispatch({
      type: "failed",
      action: "copy",
      port,
      error: "Clipboard blocked — select the URL above and copy it manually.",
    });
  }

  async function shareUrl(port: number) {
    dispatch({ type: "start", action: "share", port });
    let url = "";
    try {
      url = await links.createShare(port);
    } catch (caught) {
      dispatch({ type: "failed", action: "share", port, error: (caught as Error).message });
      return;
    }
    // The token is shown exactly once, so the link is surfaced in the panel
    // whether or not the clipboard write was allowed.
    const copied = await writeToClipboard(url);
    dispatch({ type: "done", action: "share", port, url, copied });
  }

  const issued = issuedShareUrl(feedback);

  return (
    <div class="space-y-2">
      {links.error && (
        <div class="flex items-start gap-2 rounded-md border border-accent-red/30 bg-accent-red/[0.08] px-2.5 py-2 text-[11.5px] text-accent-red">
          <AlertCircle class="mt-0.5 h-3.5 w-3.5 flex-none" />
          <span class="min-w-0 break-words">{links.error}</span>
        </div>
      )}

      {links.unavailable ? (
        <PreviewNotice text={unavailableText(links.unavailable)} />
      ) : links.loading && !links.loaded ? (
        <PreviewNotice text="Scanning container ports…" />
      ) : links.rows.length === 0 ? (
        <PreviewNotice
          text="No app is listening yet."
          hint="Ask the agent to start your dev server — new ports appear here within 15 seconds."
        />
      ) : (
        links.rows.map((row) => (
          <PortRow
            key={row.port}
            row={row}
            url={buildProjectPreviewUrl(project.slug, row.port, PUBLIC_HOSTNAME)}
            copying={isPreviewLinkBusy(feedback, "copy", row.port)}
            copied={isPreviewLinkDone(feedback, "copy", row.port)}
            sharing={isPreviewLinkBusy(feedback, "share", row.port)}
            shared={isPreviewLinkDone(feedback, "share", row.port)}
            agentBrowserBusy={agentBrowser.busyPort === row.port}
            agentBrowserOpened={agentBrowser.openedPort === row.port}
            screenshotBusy={screenshot.busyPort === row.port}
            error={
              previewLinkError(feedback, row.port) ??
              (agentBrowser.errorPort === row.port ? agentBrowser.error ?? undefined : undefined) ??
              (screenshot.errorPort === row.port ? screenshot.error ?? undefined : undefined)
            }
            onCopy={copyUrl}
            onShare={shareUrl}
            onOpenInAgentBrowser={(port) => void agentBrowser.open(port)}
            onScreenshot={(port) => void screenshot.capture(port)}
          />
        ))
      )}

      {screenshot.card && (
        <ScreenshotCard
          screenshot={screenshot.card.screenshot}
          delivered={screenshot.card.delivered}
          publicUrl={screenshot.card.publicUrl}
          sending={screenshot.sending}
          canSend={screenshot.card.canSend}
          onSend={screenshot.send}
          onDismiss={screenshot.dismiss}
        />
      )}

      {issued && <IssuedShareLink url={issued} copied={feedback.copied === true} />}

      {agentBrowser.openedPort !== null && !onAgentBrowserOpened && (
        <PreviewNotice
          text={`Loaded :${agentBrowser.openedPort} in the Agent Browser.`}
          hint="Open a chat in this project and switch the Browser pane to Agent Browser to watch it."
        />
      )}

      <div class="flex items-center gap-2 pt-0.5">
        <button
          type="button"
          onClick={() => void links.refresh()}
          disabled={links.loading || links.unavailable !== null}
          class="inline-flex h-8 items-center gap-1.5 rounded px-2 text-[11px] font-medium text-ink-300
                 transition hover:bg-white/[0.07] hover:text-ink-100 disabled:opacity-40"
        >
          {links.loading ? (
            <Loader class="h-3 w-3 animate-spin" />
          ) : (
            <RotateCcw class="h-3 w-3" />
          )}
          Refresh ports
        </button>
      </div>
    </div>
  );
}

function PortRow({
  row,
  url,
  copying,
  copied,
  sharing,
  shared,
  agentBrowserBusy,
  agentBrowserOpened,
  screenshotBusy,
  error,
  onCopy,
  onShare,
  onOpenInAgentBrowser,
  onScreenshot,
}: {
  row: PreviewPortRow;
  url: string;
  copying: boolean;
  copied: boolean;
  sharing: boolean;
  shared: boolean;
  agentBrowserBusy: boolean;
  agentBrowserOpened: boolean;
  screenshotBusy: boolean;
  error?: string;
  onCopy: (port: number, url: string) => void;
  onShare: (port: number) => void;
  onOpenInAgentBrowser: (port: number) => void;
  onScreenshot: (port: number) => void;
}) {
  return (
    <div class="rounded-md border border-white/[0.08] bg-white/[0.03] px-2.5 py-2">
      <div class="flex items-center gap-2">
        <span class="font-mono text-[12.5px] font-semibold text-ink-50">:{row.port}</span>
        {platformPortLabel(row.port) ? (
          <span class="min-w-0 truncate text-[11px] text-ink-200" title={row.process}>
            {platformPortLabel(row.port)}
          </span>
        ) : (
          row.process && (
            <span class="min-w-0 truncate text-[11px] text-ink-400" title={row.process}>
              {row.process}
            </span>
          )
        )}
        <span class="ml-auto flex flex-none items-center gap-1">
          {!row.shareable && (
            <span
              class="rounded-md border border-white/10 bg-white/[0.06] px-1.5 py-0.5 text-[10px] text-ink-300"
              title="Platform port — the share service refuses public links for it"
            >
              platform
            </span>
          )}
          {row.shareCount > 0 && (
            <span class="rounded-md border border-white/10 bg-white/[0.06] px-1.5 py-0.5 text-[10px] text-ink-300">
              {row.shareCount} link{row.shareCount === 1 ? "" : "s"}
            </span>
          )}
        </span>
      </div>

      <div class="mt-1 truncate font-mono text-[10.5px] text-ink-400" title={url}>
        {url}
      </div>

      <div class="mt-1.5 flex flex-wrap items-center gap-1.5">
        <a
          href={url}
          target="_blank"
          rel="noopener noreferrer"
          class="inline-flex h-8 items-center gap-1.5 rounded-md bg-accent-blue px-2.5 text-[11.5px]
                 font-medium text-ink-900 transition hover:bg-accent-blue/85"
        >
          <ExternalLink class="h-3 w-3" />
          Open
        </a>
        <button
          type="button"
          onClick={() => onCopy(row.port, url)}
          disabled={copying}
          class="inline-flex h-8 items-center gap-1.5 rounded-md border border-white/10 bg-white/[0.05] px-2.5
                 text-[11.5px] font-medium text-ink-200 transition hover:bg-white/[0.09] hover:text-ink-100 disabled:opacity-50"
        >
          {copied ? <Check class="h-3 w-3" /> : <Copy class="h-3 w-3" />}
          {copied ? "Copied" : "Copy URL"}
        </button>
        {canOpenInAgentBrowser(row) && (
          <button
            type="button"
            onClick={() => onOpenInAgentBrowser(row.port)}
            disabled={agentBrowserBusy}
            title="Load this port in the project's shared Agent Browser, inside the container"
            class="inline-flex h-8 items-center gap-1.5 rounded-md border border-white/10 bg-white/[0.05] px-2.5
                   text-[11.5px] font-medium text-ink-200 transition hover:bg-white/[0.09] hover:text-ink-100 disabled:opacity-50"
          >
            {agentBrowserBusy ? (
              <Loader class="h-3 w-3 animate-spin" />
            ) : agentBrowserOpened ? (
              <Check class="h-3 w-3" />
            ) : (
              <Monitor class="h-3 w-3" />
            )}
            {agentBrowserBusy ? "Opening…" : agentBrowserOpened ? "Opened" : "Agent Browser"}
          </button>
        )}
        {row.shareable && (
          <>
            <button
              type="button"
              onClick={() => onScreenshot(row.port)}
              disabled={screenshotBusy}
              title="Photograph this port now and share the picture"
              class="inline-flex h-8 items-center gap-1.5 rounded-md border border-white/10 bg-white/[0.05] px-2.5
                     text-[11.5px] font-medium text-ink-200 transition hover:bg-white/[0.09] hover:text-ink-100 disabled:opacity-50"
            >
              {screenshotBusy ? (
                <Loader class="h-3 w-3 animate-spin" />
              ) : (
                <Camera class="h-3 w-3" />
              )}
              {screenshotBusy ? "Capturing…" : "Screenshot"}
            </button>
            <button
              type="button"
              onClick={() => onShare(row.port)}
              disabled={sharing}
              title={`Create a public link that works for ${PREVIEW_SHARE_TTL_HOURS} hours`}
              class="inline-flex h-8 items-center gap-1.5 rounded-md border border-white/10 bg-white/[0.05] px-2.5
                     text-[11.5px] font-medium text-ink-200 transition hover:bg-white/[0.09] hover:text-ink-100 disabled:opacity-50"
            >
              {sharing ? (
                <Loader class="h-3 w-3 animate-spin" />
              ) : shared ? (
                <Check class="h-3 w-3" />
              ) : (
                <Link2 class="h-3 w-3" />
              )}
              {sharing ? "Sharing…" : shared ? "Shared" : "Share 24h"}
            </button>
          </>
        )}
      </div>

      {error && <div class="mt-1 text-[11px] text-accent-red">{error}</div>}
    </div>
  );
}

function IssuedShareLink({ url, copied }: { url: string; copied: boolean }) {
  return (
    <div class="rounded-md border border-accent-blue/30 bg-accent-blue/[0.08] px-2.5 py-2">
      <div class="text-[11.5px] font-semibold text-ink-50">
        {copied ? "Public link copied" : "Public link created — copy it now"}
      </div>
      <code class="mt-1 block break-all font-mono text-[10.5px] text-ink-100">{url}</code>
      <div class="mt-1 text-[10.5px] text-ink-300">
        Shown once. Revoke it under the project's Sharing settings.
      </div>
    </div>
  );
}

function PreviewNotice({ text, hint }: { text: string; hint?: string }) {
  return (
    <div class="rounded-md border border-white/[0.08] bg-white/[0.02] px-2.5 py-2">
      <div class="text-[11.5px] text-ink-200">{text}</div>
      {hint && <div class="mt-1 text-[10.5px] leading-relaxed text-ink-400">{hint}</div>}
    </div>
  );
}

function unavailableText(reason: NonNullable<ProjectPreviewLinks["unavailable"]>): string {
  if (reason === "provisioning") return "Container is still provisioning.";
  if (reason === "missing") return "Container is missing — reprovision the project.";
  return "Container is stopped. Start it to see preview links.";
}

async function writeToClipboard(text: string): Promise<boolean> {
  if (!text) return false;
  try {
    await navigator.clipboard.writeText(text);
    return true;
  } catch {
    return false;
  }
}
