import { useCallback, useEffect, useRef, useState } from "preact/hooks";
import type { ChatStatus, PromptOutcome, SyntheticKind } from "../../../models/chat";
import { chatComposerSessionStore } from "../../chat/composerSessionStore";
import { useAttachmentUpload } from "./useAttachmentUpload";
import { useAutosizeTextarea } from "./useAutosizeTextarea";
import { useDragUpload } from "./useDragUpload";
import { usePromptQueue } from "./usePromptQueue";
import { useThreadScroll } from "./useThreadScroll";

export function useChatComposerController({
  chatId,
  eventCount,
  blockCount,
  status,
  canSendPrompt,
  sendPrompt,
  promptOutcome,
  rewind,
  refreshMeta,
  attachmentBasePath,
}: {
  chatId: string;
  eventCount: number;
  blockCount: number;
  status: ChatStatus;
  canSendPrompt: boolean;
  sendPrompt: (text: string, clientId?: string, synthetic?: SyntheticKind) => boolean;
  promptOutcome: PromptOutcome | null;
  rewind: (beforeT: number) => Promise<unknown>;
  refreshMeta: () => Promise<void>;
  attachmentBasePath: string;
}) {
  // Initialise from the per-chat session store and mirror every change back to it.
  // ChatContainer remounts on chat switch (it is keyed by chatId), so this is
  // what makes a half-typed message survive leaving and returning to a chat.
  const [text, setTextState] = useState(() => chatComposerSessionStore.getDraft(chatId));
  const setText = useCallback(
    (value: string | ((prev: string) => string)) => {
      setTextState((prev) => {
        const next = typeof value === "function" ? (value as (prev: string) => string)(prev) : value;
        chatComposerSessionStore.setDraft(chatId, next);
        return next;
      });
    },
    [chatId],
  );
  const fileInputRef = useRef<HTMLInputElement>(null);
  const { textareaRef, focusInput } = useAutosizeTextarea(text);
  const upload = useAttachmentUpload(chatId, attachmentBasePath);
  const drag = useDragUpload(upload.doUpload);
  const scroll = useThreadScroll(chatId, `${eventCount}:${blockCount}`);
  const queue = usePromptQueue({
    chatId,
    status,
    canSendPrompt,
    sendPrompt,
    promptOutcome,
    onSent: scroll.unlockAutoScroll,
  });

  useEffect(() => {
    scroll.unlockAutoScroll();
  }, [chatId]);

  async function handleRewind(t: number, promptText: string) {
    if (status === "streaming") {
      alert("Cancel the current run before rewinding this chat.");
      return;
    }
    if (!confirm("Rewind to this prompt? Messages from this point forward will be removed.")) return;
    try {
      await rewind(t);
      queue.clearQueuedPrompts();
      setText(promptText);
      await refreshMeta();
      scroll.unlockAutoScroll();
      setTimeout(() => {
        scroll.jumpToBottom();
        focusInput();
      }, 0);
    } catch (rewindError) {
      alert("rewind failed: " + (rewindError as Error).message);
    }
  }

  function handlePaste(event: ClipboardEvent) {
    const items = event.clipboardData?.items;
    if (!items) return;
    const files: File[] = [];
    for (let i = 0; i < items.length; i++) {
      const item = items[i];
      if (item.kind === "file") {
        const file = item.getAsFile();
        if (file) files.push(file);
      }
    }
    if (files.length) {
      event.preventDefault();
      upload.doUpload(files);
    }
  }

  function handleSend() {
    if (upload.uploading || (!chatComposerSessionStore.allowsQueue(status) && !canSendPrompt)) return;
    const userText = text.trim();
    const paths = upload.attachments
      .filter((attachment) => attachment.serverPath)
      .map((attachment) => attachment.serverPath);
    if (!userText && paths.length === 0) return;
    const finalText = paths.length
      ? chatComposerSessionStore.promptWithAttachments(userText, paths)
      : userText;

    if (status === "streaming") {
      queue.queuePrompt(finalText);
    } else {
      const sent = sendPrompt(finalText);
      if (!sent) return;
    }

    setText("");
    upload.clearAttachments();
    scroll.unlockAutoScroll();
    setTimeout(focusInput, 0);
  }

  // insertText and submitText exist for prompt sources that are not the
  // textarea — today the Playbooks menu. They deliberately share the queueing
  // and scroll behaviour of the normal send path instead of reimplementing it.

  /**
   * Puts a ready-made prompt in the composer without destroying a draft: an
   * empty composer is replaced, a half-typed one gets the prompt appended
   * after a blank line. `select` is a range inside `text` to highlight once
   * it lands, so an unfilled placeholder is the first thing the user types
   * over.
   */
  function insertText(value: string, select?: { start: number; end: number }) {
    const existing = text.trimEnd();
    const offset = existing ? existing.length + 2 : 0;
    const next = existing ? `${existing}\n\n${value}` : value;
    setText(next);
    setTimeout(() => {
      const textarea = textareaRef.current;
      textarea?.focus();
      const start = select ? offset + select.start : next.length;
      const end = select ? offset + select.end : next.length;
      textarea?.setSelectionRange(start, end);
    }, 0);
  }

  /** Sends (or queues) a prompt that did not come from the textarea. */
  function submitText(value: string): boolean {
    const userText = value.trim();
    if (!userText) return false;
    if (upload.uploading || (!chatComposerSessionStore.allowsQueue(status) && !canSendPrompt)) {
      return false;
    }
    if (status === "streaming") {
      queue.queuePrompt(userText);
    } else if (!sendPrompt(userText)) {
      return false;
    }
    scroll.unlockAutoScroll();
    setTimeout(focusInput, 0);
    return true;
  }

  /**
   * Sends a Playwright check from the Test menu. Unlike submitText it never
   * queues: the queue replays plain text, which would strip the label and turn
   * a badged verification pass into what looks like a user's own prompt.
   */
  function submitTest(value: string): boolean {
    const testText = value.trim();
    if (!testText || upload.uploading || status === "streaming" || !canSendPrompt) return false;
    if (!sendPrompt(testText, undefined, "autotest")) return false;
    scroll.unlockAutoScroll();
    setTimeout(focusInput, 0);
    return true;
  }

  function handleAnswerQuestion(answer: string) {
    const sent = sendPrompt(answer);
    if (sent) scroll.unlockAutoScroll();
  }

  return {
    text,
    setText,
    textareaRef,
    fileInputRef,
    upload,
    drag,
    scroll,
    queue,
    handlePaste,
    handleSend,
    insertText,
    submitText,
    submitTest,
    handleAnswerQuestion,
    handleRewind,
  };
}
