import type { ChatMeta, ChatProvider, UpdateChatInput } from "../../models/chat";
import type { ProjectMeta } from "../../models/project";
import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";
import { BrowserDrawer } from "../../ui/chat/browser/BrowserDrawer";
import { ChatThread } from "../../ui/chat/ChatThread";
import { MediaViewerOverlay } from "../../ui/chat/files/MediaViewerOverlay";
import type { ChatComposerProps } from "../../ui/chat/composer/ChatComposer";
import { WorkspaceActions } from "../../ui/chat/header/WorkspaceActions";
import { HistoryDrawer } from "../../ui/chat/history/HistoryDrawer";
import { FileManagerDrawer } from "../../ui/chat/files/FileManagerDrawer";
import { ScheduleDrawer } from "../../ui/chat/schedules/ScheduleDrawer";
import { chatAttachmentState } from "../../state/chat/chatAttachmentState";
import { useAuthContext } from "../../state/context/AuthContext";
import { usePublishChatPhase } from "../../state/hooks/chat/useAgentActivity";
import { useChat } from "../../state/hooks/chat/useChat";
import { useChatBrowserController } from "../../state/hooks/chat/useChatBrowserController";
import { useChatComposerController } from "../../state/hooks/chat/useChatComposerController";
import { useChatDrawerController } from "../../state/hooks/chat/useChatDrawerController";
import { useChatKeyboardShortcuts } from "../../state/hooks/chat/useChatKeyboardShortcuts";
import { useChatPolicies } from "../../state/hooks/chat/useChatPolicies";
import { useChatPreferences } from "../../state/hooks/chat/useChatPreferences";
import { useAgentEndpointChoices } from "../../state/hooks/chat/useAgentEndpointChoices";
import { endpointBadge } from "../../state/settings/agentEndpointsState";
import { useModelRoutingPreview } from "../../state/hooks/chat/useModelRoutingPreview";
import { useChatReadMarker } from "../../state/hooks/chat/useChatReadMarker";
import { usePlaybooks } from "../../state/hooks/chat/usePlaybooks";
import { useSnippets } from "../../state/hooks/chat/useSnippets";
import { useSlashCommands } from "../../state/hooks/chat/useSlashCommands";
import { useTerminalOverlayController } from "../../state/hooks/chat/useTerminalOverlayController";
import { useWorkspaceGitRepos } from "../../state/hooks/chat/useWorkspaceGitRepos";

export function ChatContainer({
  chat,
  projects,
  highlightAt,
  onHamburger,
  onSelectChat,
}: {
  chat: ChatMeta;
  projects: ProjectMeta[];
  /** A message instant a search hit or deep link asked the thread to reveal. */
  highlightAt?: number | null;
  onHamburger: () => void;
  /** Opens another chat — the Team panel's links to the companion threads. */
  onSelectChat: (chatId: string) => void;
}) {
  const {
    meta,
    blocks,
    activity,
    eventCount,
    hasOlder,
    loadingOlder,
    status,
    error,
    canSendPrompt,
    sendPrompt,
    promptOutcome,
    cancel,
    rewind,
    loadOlder,
    refreshMeta,
  } = useChat(chat.id);
  const preferences = useChatPreferences({ chat, loadedMeta: meta, refreshMeta });
  const { displayMeta, displayMode, selectedSkills } = preferences;
  const attachmentBasePath = chatAttachmentState.basePath(displayMeta, projects);
  const composer = useChatComposerController({
    chatId: chat.id,
    eventCount,
    blockCount: blocks.length,
    status,
    canSendPrompt,
    sendPrompt,
    promptOutcome,
    rewind,
    refreshMeta,
    attachmentBasePath,
  });
  const browser = useChatBrowserController({
    chat: displayMeta,
    projects,
    blocks,
    text: composer.text,
    setText: composer.setText,
    textareaRef: composer.textareaRef,
  });
  // Same resolution the browser drawer uses; the header's preview chip needs
  // the project's slug to build preview URLs.
  const chatProject = browser.browserProject;
  const terminal = useTerminalOverlayController(chat.id);
  // The composer's Playbooks menu. Its context comes from the chat's project
  // and the newest preview URL the conversation mentioned, so a template can
  // name the project it is about without a port scan.
  const playbookContext = useMemo(
    () => ({
      projectName: chatProject?.name,
      slug: chatProject?.slug,
      previewUrl: browser.previewUrl,
    }),
    [chatProject?.name, chatProject?.slug, browser.previewUrl],
  );
  const playbookChatState = useMemo(
    () => ({
      provider: displayMeta.provider || ("codex" as const),
      selectedSkills,
      mode: displayMode,
    }),
    [displayMeta.provider, selectedSkills, displayMode],
  );
  // Team mode may only seat a provider that is actually logged in on the host;
  // the auth context already keeps those three sockets live for the setup gate.
  const { claudeAuth, codexAuth, kimiAuth } = useAuthContext();
  const connectedProviders = useMemo(() => {
    const connected: ChatProvider[] = [];
    if (claudeAuth.authenticated) connected.push("claude");
    if (codexAuth.authenticated) connected.push("codex");
    if (kimiAuth.authenticated) connected.push("kimi");
    return connected;
  }, [claudeAuth.authenticated, codexAuth.authenticated, kimiAuth.authenticated]);
  const applyMeta = preferences.applyMeta;
  const applyPlaybookMeta = useCallback(
    (patch: UpdateChatInput) => applyMeta(patch),
    [applyMeta],
  );
  const playbooks = usePlaybooks({
    enabled: true,
    context: playbookContext,
    current: playbookChatState,
    applyMeta: applyPlaybookMeta,
    insertPrompt: composer.insertText,
    submitPrompt: composer.submitText,
  });
  // The user's own snippet library. It shares the playbook context and adds
  // the current draft, which is what {{selection}} stands for.
  const snippets = useSnippets({
    enabled: true,
    context: playbookContext,
    draft: composer.text,
    insertText: composer.insertText,
    setText: composer.setText,
  });
  // "Save as snippet" on a message hands its text to the composer's Snippets
  // menu, which opens with the editor already filled in.
  const [pendingSnippet, setPendingSnippet] = useState<string | null>(null);
  const handleSaveSnippet = useCallback((text: string) => setPendingSnippet(text), []);
  const clearPendingSnippet = useCallback(() => setPendingSnippet(null), []);
  // Post-run policies. `displayMeta` is the optimistic merge of the loaded
  // chat and the local preference edits, which is also what the workspace
  // socket refreshes when the driver spends a round — so the pill and the
  // popover follow an unattended loop without polling.
  const policies = useChatPolicies({
    chat: displayMeta,
    streaming: status === "streaming",
    connectedProviders,
    applyMeta,
    sendPrompt: (text) => composer.submitTest(text),
  });
  // Slash commands reuse the very handlers the composer's buttons already
  // call, so `/autopilot on` and the autopilot popover cannot drift apart.
  const slash = useSlashCommands({
    project: chatProject,
    provider: displayMeta.provider || "codex",
    text: composer.text,
    setText: composer.setText,
    textareaRef: composer.textareaRef,
    insertText: composer.insertText,
    submitTest: composer.submitTest,
    playbooks,
    snippets,
    policies,
    changeMode: preferences.changeMode,
    selectSkill: preferences.selectSkill,
    onAgentBrowserOpened: browser.openAgentBrowserPane,
  });
  // One send path: a message that parses as a command runs it, anything else
  // goes to the agent exactly as before.
  const handleSend = useCallback(() => {
    if (slash.interceptSend()) return;
    composer.handleSend();
  }, [slash, composer]);
  const drawers = useChatDrawerController({
    chatId: chat.id,
    showBrowser: browser.openBrowserDrawer,
    hideBrowser: browser.closeBrowserDrawer,
  });

  useChatReadMarker({ chatId: chat.id, eventCount, status });
  // The sidebar row is a sibling of this container, so the phase it shows is
  // published rather than threaded through the workspace context — see
  // `agentPhaseStore`.
  usePublishChatPhase(chat.id, status === "streaming" ? activity.phase : "idle");
  useChatKeyboardShortcuts({ status, onCancel: cancel });
  const { hasRepos } = useWorkspaceGitRepos({ chatId: chat.id, status });
  const workspaceActions = {
    cwd: displayMeta.cwd || "~",
    onOpenTerminal: terminal.openTerminal,
    onToggleBrowser: browser.browserOpen ? browser.closeBrowserDrawer : drawers.openBrowser,
    onToggleHistory: drawers.historyOpen ? drawers.closeHistory : drawers.openHistory,
    onToggleFiles: drawers.filesOpen ? drawers.closeFiles : drawers.openFiles,
    onToggleSchedules: drawers.schedulesOpen ? drawers.closeSchedules : drawers.openSchedules,
    browserOpen: browser.browserOpen,
    historyOpen: drawers.historyOpen,
    filesOpen: drawers.filesOpen,
    schedulesOpen: drawers.schedulesOpen,
    showHistory: hasRepos,
    showSchedules: !!displayMeta.projectId,
  };
  const activePane = drawers.historyOpen
    ? "history"
    : drawers.filesOpen
      ? "files"
      : drawers.schedulesOpen
        ? "schedules"
        : browser.browserOpen
          ? "browser"
          : null;
  const previousMobilePane = useRef<typeof activePane>(null);

  useEffect(() => {
    if (!window.matchMedia("(max-width: 767px)").matches) return;

    if (activePane) {
      previousMobilePane.current = activePane;
      requestAnimationFrame(() => {
        document
          .getElementById(`workspace-${activePane}-pane`)
          ?.querySelector<HTMLElement>("[data-workspace-pane-close]")
          ?.focus();
      });
      return;
    }

    const closedPane = previousMobilePane.current;
    if (!closedPane) return;
    previousMobilePane.current = null;
    requestAnimationFrame(() => {
      const triggers = document.querySelectorAll<HTMLElement>(
        `[data-workspace-action="${closedPane}"]`
      );
      Array.from(triggers).find((trigger) => trigger.offsetParent !== null)?.focus();
    });
  }, [activePane]);

  // The third-party endpoints a chat may be pointed at. One fetch serves
  // every chat: the register changes only when an administrator edits it.
  const endpointChoices = useAgentEndpointChoices(true);
  const endpointBadgeText = endpointBadge(
    endpointChoices,
    displayMeta.endpointId,
    displayMeta.model,
  );

  // The routed-model hint for the next turn. It is only asked for while the
  // chat is actually on Auto, so a pinned chat costs nothing. A chat pointed
  // at an endpoint is pinned to it, so the hint is not asked for either.
  const routingPreview = useModelRoutingPreview({
    enabled: displayMeta.modelPolicy === "auto" && !displayMeta.endpointId,
    draft: composer.text,
    mode: displayMode,
    provider: displayMeta.provider || "codex",
    model: displayMeta.model || "",
    projectId: displayMeta.projectId,
    selectedSkills,
  });

  const composerView: ChatComposerProps = {
    chatId: chat.id,
    projectId: displayMeta.projectId,
    streaming: status === "streaming",
    canSendPrompt,
    preferences: {
      provider: displayMeta.provider || "codex",
      model: displayMeta.model || "",
      mode: displayMode,
      reasoningEffort: displayMeta.reasoningEffort || "",
      serviceTier: displayMeta.serviceTier || "",
      modelPolicy: displayMeta.modelPolicy,
      endpointId: displayMeta.endpointId,
    },
    preferenceActions: {
      changeProvider: preferences.changeProvider,
      changeModel: preferences.changeModel,
      changeModelPolicy: preferences.changeModelPolicy,
      changeEndpoint: preferences.changeEndpoint,
      changeMode: preferences.changeMode,
      changeReasoningEffort: preferences.changeReasoningEffort,
      changeServiceTier: preferences.changeServiceTier,
    },
    routing: { decision: routingPreview.decision, available: !routingPreview.unavailable },
    endpointChoices,
    queuedPrompts: composer.queue.queuedPrompts,
    selectedSkills,
    playbooks,
    snippets,
    pendingSnippet,
    onPendingSnippetHandled: clearPendingSnippet,
    policies,
    slash,
    attachments: composer.upload.attachments,
    uploading: composer.upload.uploading,
    dragging: composer.drag.dragging,
    text: composer.text,
    textareaRef: composer.textareaRef,
    fileInputRef: composer.fileInputRef,
    onTextChange: composer.setText,
    onFilesSelected: composer.upload.doUpload,
    onPaste: composer.handlePaste,
    onSend: handleSend,
    onCancel: cancel,
    onRemoveQueued: composer.queue.removeQueuedPrompt,
    onRemoveAttachment: composer.upload.removeAttachment,
    onSelectSkill: preferences.selectSkill,
    onRemoveSelectedSkill: preferences.removeSelectedSkill,
  };

  return (
    <div class="relative flex-1 h-full min-h-0 overflow-hidden">
      <div class="flex h-full min-h-0 w-full overflow-hidden">
        <div class={`min-w-0 flex-1 h-full ${activePane ? "hidden md:block" : ""}`}>
          <ChatThread
            chat={displayMeta}
            project={chatProject}
            activity={activity}
            blocks={blocks}
            highlightAt={highlightAt ?? null}
            hasOlder={hasOlder}
            loadingOlder={loadingOlder}
            status={status}
            error={error}
            composer={composerView}
            policies={policies}
            endpointBadge={endpointBadgeText}
            showJump={composer.scroll.showJump}
            scrollRef={composer.scroll.scrollRef}
            contentRef={composer.scroll.contentRef}
            bottomRef={composer.scroll.bottomRef}
            onHamburger={onHamburger}
            onScroll={composer.scroll.onScroll}
            onJumpToBottom={composer.scroll.jumpToBottom}
            onAnswerQuestion={composer.handleAnswerQuestion}
            onLoadOlder={loadOlder}
            onRewind={composer.handleRewind}
            onSaveSnippet={handleSaveSnippet}
            onOpenAgentBrowser={browser.openAgentBrowserPane}
            onOpenCompanionChat={onSelectChat}
            mobileToolbar={
              <aside class="workspace-action-toolbar relative z-30 flex flex-none justify-end border-b border-white/10 bg-[#101318] px-3 py-2 md:hidden">
                <WorkspaceActions {...workspaceActions} orientation="horizontal" />
              </aside>
            }
          />
        </div>
        <HistoryDrawer
          chatId={chat.id}
          open={drawers.historyOpen}
          onClose={drawers.closeHistory}
        />
        <FileManagerDrawer
          chatId={chat.id}
          open={drawers.filesOpen}
          onClose={drawers.closeFiles}
        />
        <ScheduleDrawer
          chatId={chat.id}
          open={drawers.schedulesOpen}
          onClose={drawers.closeSchedules}
        />
        <BrowserDrawer
          open={browser.browserOpen}
          projectId={browser.browserProject?.id || ""}
          projectName={browser.browserProject?.name || ""}
          projectSlug={browser.browserProject?.slug || ""}
          apps={browser.containerApps}
          appsLoading={browser.appsLoading}
          selectedPort={browser.selectedAppPort}
          agentBrowserRequest={browser.agentBrowserRequest}
          onSelectPort={browser.setSelectedAppPort}
          onRefreshApps={() => void browser.loadContainerApps()}
          onCaptureElement={browser.insertBrowserElementContext}
          onClose={browser.closeBrowserDrawer}
        />
        <aside class="workspace-action-rail top-chrome z-20 hidden w-12 flex-none flex-col items-center border-l border-white/10 bg-[#101318] px-1.5 pb-2 md:flex">
          <WorkspaceActions {...workspaceActions} orientation="vertical" />
        </aside>
      </div>
      {terminal.TerminalOverlay && (
        <terminal.TerminalOverlay
          chat={displayMeta}
          open={terminal.terminalOpen}
          onClose={terminal.closeTerminal}
        />
      )}
      <MediaViewerOverlay />
    </div>
  );
}
