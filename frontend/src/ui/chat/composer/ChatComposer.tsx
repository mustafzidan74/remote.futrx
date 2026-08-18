import type { RefObject } from "preact";
import { useEffect, useState } from "preact/hooks";
import { useVoiceInput } from "../../../state/hooks/chat/useVoiceInput";
import { modelDisplayLabel, providerDisplayLabel } from "../../../config/chat";
import type { QueuedPrompt, SelectedSkill } from "../../../models/chat";
import type { RegisteredSkill } from "../../../models/skill";
import type { Attachment } from "../../../models/upload";
import type { ChatPolicies } from "../../../state/hooks/chat/useChatPolicies";
import type { PlaybookLibrary } from "../../../state/hooks/chat/usePlaybooks";
import { AlertCircle, ChevronDown, Settings, X } from "../../primitives/icons";
import { AttachmentTray } from "./AttachmentTray";
import { AttachButton } from "./AttachButton";
import { ComposerAgentControls } from "./ComposerAgentControls";
import { ComposerDropOverlay } from "./ComposerDropOverlay";
import { ComposerExecutionControls } from "./ComposerExecutionControls";
import { PromptTextarea } from "./PromptTextarea";
import { QueuedPromptList } from "./QueuedPromptList";
import { SelectedSkillChips } from "./SelectedSkillChips";
import { SendControls } from "./SendControls";
import { VoiceInputButton } from "./VoiceInputButton";
import { VoiceLiveStrip } from "./VoiceLiveStrip";
import type { ComposerPreferenceActions, ComposerPreferences } from "./preferences";

export interface ChatComposerProps {
  chatId: string;
  projectId?: string;
  streaming: boolean;
  canSendPrompt: boolean;
  preferences: ComposerPreferences;
  preferenceActions: ComposerPreferenceActions;
  queuedPrompts: QueuedPrompt[];
  selectedSkills: SelectedSkill[];
  playbooks: PlaybookLibrary;
  policies: ChatPolicies;
  attachments: Attachment[];
  uploading: boolean;
  dragging: boolean;
  text: string;
  textareaRef: RefObject<HTMLTextAreaElement>;
  fileInputRef: RefObject<HTMLInputElement>;
  onTextChange: (text: string) => void;
  onFilesSelected: (files: File[]) => void;
  onPaste: (event: ClipboardEvent) => void;
  onSend: () => void;
  onCancel: () => void;
  onRemoveQueued: (id: string) => void;
  onRemoveAttachment: (id: string) => void;
  onSelectSkill: (skill: RegisteredSkill) => void;
  onRemoveSelectedSkill: (skill: SelectedSkill) => void;
}

export function ChatComposer({
  chatId,
  projectId,
  streaming,
  canSendPrompt,
  preferences,
  preferenceActions,
  queuedPrompts,
  selectedSkills,
  playbooks,
  policies,
  attachments,
  uploading,
  dragging,
  text,
  textareaRef,
  fileInputRef,
  onTextChange,
  onFilesSelected,
  onPaste,
  onSend,
  onCancel,
  onRemoveQueued,
  onRemoveAttachment,
  onSelectSkill,
  onRemoveSelectedSkill,
}: ChatComposerProps) {
  const [mobileSettingsOpen, setMobileSettingsOpen] = useState(false);
  const disconnected = !canSendPrompt && !streaming;
  // Dictation writes into the same draft the textarea does, so it lives here
  // rather than in the container: everything it needs is already a prop.
  const voice = useVoiceInput({
    chatId,
    text,
    onTextChange,
    textareaRef,
    disabled: uploading || disconnected,
  });
  // Sending while dictating has to wait for the transcript. The last words
  // reach the composer through a state update, so sending in the same tick
  // would post the text as it was one hypothesis ago — and the server engine
  // has not even uploaded its clip yet at that point.
  const [sendAfterDictation, setSendAfterDictation] = useState(false);

  useEffect(() => {
    if (!sendAfterDictation || voice.active) return;
    setSendAfterDictation(false);
    // A session that ended on an error leaves a banner the user should read;
    // nothing gets sent on their behalf until they have.
    if (voice.session.status !== "error") onSend();
  }, [sendAfterDictation, voice.active, voice.session.status, onSend]);

  /**
   * The one way a prompt leaves this composer. Both the form's submit button
   * and the textarea's Ctrl/Cmd+Enter shortcut route through here, because
   * sending straight from the shortcut while the microphone is live would post
   * the draft as it stood one hypothesis ago and then let the rest of the
   * dictation land in the composer that was just cleared.
   */
  function requestSend() {
    if (!voice.active) {
      onSend();
      return;
    }
    voice.stop();
    setSendAfterDictation(true);
  }

  function submit(event: Event) {
    event.preventDefault();
    requestSend();
  }
  const hasContent = text.trim().length > 0 || attachments.some((attachment) => attachment.serverPath);
  const canSend = !uploading && !disconnected && hasContent;
  const settingsSummary = `${providerDisplayLabel(preferences.provider)} · ${modelDisplayLabel(preferences.model, preferences.provider)}`;

  function toggleMobileSettings() {
    setMobileSettingsOpen((open) => {
      if (!open) textareaRef.current?.blur();
      return !open;
    });
  }

  return (
    <div class="codex-composer-shell flex-none z-20 relative bg-[#0b0d11] border-t border-white/10">
      {dragging && <ComposerDropOverlay />}

      {playbooks.notice && (
        <div class="mx-3 mt-2 flex items-start gap-2 rounded-md border border-accent-blue/30 bg-accent-blue/[0.08] px-2.5 py-2">
          <AlertCircle class="mt-0.5 h-3.5 w-3.5 flex-none text-accent-blue" aria-hidden="true" />
          <span class="min-w-0 flex-1 break-words text-[11.5px] leading-4 text-ink-100">
            {playbooks.notice}
          </span>
          <button
            type="button"
            onClick={playbooks.dismissNotice}
            class="grid h-5 w-5 flex-none place-items-center rounded text-ink-300 hover:bg-white/[0.08] hover:text-ink-50"
            aria-label="Dismiss playbook notice"
          >
            <X class="h-3 w-3" />
          </button>
        </div>
      )}

      {/*
        The live dictation strip sits above the composer card rather than only
        inside the textarea. It is fed by the voice session, not by the draft,
        so a microphone that is working but not reaching the composer is
        visible instead of indistinguishable from a dead one.
      */}
      <VoiceLiveStrip voice={voice} />

      <SelectedSkillChips skills={selectedSkills} onRemove={onRemoveSelectedSkill} />
      <QueuedPromptList queuedPrompts={queuedPrompts} onRemove={onRemoveQueued} />
      <AttachmentTray attachments={attachments} onRemove={onRemoveAttachment} />

      <div class="codex-composer-card mx-3 my-2 overflow-visible rounded-xl border border-white/10 bg-[#15171c] shadow-[0_8px_24px_rgba(0,0,0,0.18)]">
        <form
          onSubmit={submit}
          class="codex-composer-form composer-form flex gap-1.5 items-end px-2 pt-2"
        >
          <AttachButton
            fileInputRef={fileInputRef}
            uploading={uploading}
            disconnected={disconnected}
            onFilesSelected={onFilesSelected}
          />
          <PromptTextarea
            textareaRef={textareaRef}
            text={text}
            uploading={uploading}
            streaming={streaming}
            disconnected={disconnected}
            onTextChange={onTextChange}
            onPaste={onPaste}
            onSend={requestSend}
            lang={voice.languageTag}
          />
          <button
            type="button"
            onClick={toggleMobileSettings}
            class={`codex-mobile-settings-trigger min-w-0 items-center gap-1.5 rounded-full border px-3 text-left transition md:hidden
                    ${mobileSettingsOpen ? "border-accent-blue/40 bg-accent-blue/[0.12] text-accent-blue" : "border-white/10 bg-white/[0.045] text-ink-200"}`}
            aria-label={`${mobileSettingsOpen ? "Hide" : "Show"} composer settings. ${settingsSummary}`}
            aria-controls="mobile-composer-settings"
            aria-expanded={mobileSettingsOpen}
          >
            <Settings class="h-3.5 w-3.5 flex-none" aria-hidden="true" />
            <span class="min-w-0 flex-1 truncate text-[12px] font-semibold">{settingsSummary}</span>
            <ChevronDown
              class={`h-3 w-3 flex-none transition-transform ${mobileSettingsOpen ? "rotate-180" : ""}`}
              aria-hidden="true"
            />
          </button>
          <VoiceInputButton voice={voice} disabled={uploading || disconnected} />
          <SendControls
            streaming={streaming}
            canSend={canSend}
            disconnected={disconnected}
            onCancel={onCancel}
          />
        </form>

        {mobileSettingsOpen && (
          <div
            id="mobile-composer-settings"
            class="codex-mobile-settings-panel border-t border-white/[0.07] px-2.5 pb-2.5 pt-2 md:hidden"
            role="group"
            aria-label="Composer settings"
          >
            <div class="mb-1.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-ink-400">
              Agent and model
            </div>
            <ComposerAgentControls
              projectId={projectId}
              model={preferences.model}
              provider={preferences.provider}
              streaming={streaming}
              selectedSkills={selectedSkills}
              playbooks={playbooks}
              onSelectSkill={onSelectSkill}
              onProviderChange={preferenceActions.changeProvider}
              onModelChange={preferenceActions.changeModel}
            />

            <div class="mb-1.5 mt-2.5 text-[10px] font-semibold uppercase tracking-[0.12em] text-ink-400">
              Execution
            </div>
            <ComposerExecutionControls
              preferences={preferences}
              preferenceActions={preferenceActions}
              policies={policies}
              canSendPrompt={canSendPrompt}
              streaming={streaming}
            />
          </div>
        )}

        <div class="codex-composer-control-deck hidden min-w-0 flex-wrap items-center justify-between gap-1.5 border-t border-white/[0.07] px-2 py-1.5 md:flex">
          <ComposerAgentControls
            projectId={projectId}
            model={preferences.model}
            provider={preferences.provider}
            streaming={streaming}
            selectedSkills={selectedSkills}
            playbooks={playbooks}
            onSelectSkill={onSelectSkill}
            onProviderChange={preferenceActions.changeProvider}
            onModelChange={preferenceActions.changeModel}
          />

          <ComposerExecutionControls
            preferences={preferences}
            preferenceActions={preferenceActions}
            policies={policies}
            canSendPrompt={canSendPrompt}
            streaming={streaming}
          />
        </div>
      </div>
    </div>
  );
}
