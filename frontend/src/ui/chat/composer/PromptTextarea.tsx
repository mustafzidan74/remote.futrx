import type { RefObject } from "preact";

export function PromptTextarea({
  textareaRef,
  text,
  uploading,
  streaming,
  disconnected,
  onTextChange,
  onPaste,
  onSend,
  lang,
}: {
  textareaRef: RefObject<HTMLTextAreaElement>;
  text: string;
  uploading: boolean;
  streaming: boolean;
  disconnected: boolean;
  onTextChange: (text: string) => void;
  onPaste: (event: ClipboardEvent) => void;
  onSend: () => void;
  /**
   * BCP-47 tag of the dictation language, when one is selected. It tells the
   * browser how to shape and spell-check the text that voice input inserts;
   * `dir` stays "auto" so the base direction still follows the content.
   */
  lang?: string;
}) {
  return (
    <textarea
      ref={textareaRef}
      value={text}
      onInput={(event) => onTextChange((event.currentTarget as HTMLTextAreaElement).value)}
      onKeyDown={(event) => {
        if (
          event.key === "Enter" &&
          (event.ctrlKey || event.metaKey) &&
          !event.shiftKey &&
          !event.isComposing
        ) {
          event.preventDefault();
          onSend();
        }
      }}
      onPaste={(event) => onPaste(event as ClipboardEvent)}
      rows={1}
      dir="auto"
      lang={lang || undefined}
      enterkeyhint="enter"
      aria-keyshortcuts="Control+Enter Meta+Enter"
      autocomplete="off"
      autocapitalize="off"
      autocorrect="off"
      spellcheck={false}
      placeholder={
        uploading ? "Uploading..." :
        streaming ? "Queue next prompt while the agent is working" :
        disconnected ? "Connecting..." :
        "Ask anything, @ to add files, / for commands"
      }
      disabled={disconnected}
      class="codex-composer-textarea min-w-0 flex-1 resize-none rounded-md
             bg-transparent border-0 text-ink-100 placeholder:text-ink-300
             focus:outline-none
             px-2.5 py-2.5 text-[16px] sm:text-[14px] leading-normal
             min-h-[40px] max-h-[220px]
             disabled:opacity-60 transition-colors"
    />
  );
}
