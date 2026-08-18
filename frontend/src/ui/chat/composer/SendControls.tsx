import { ArrowUp, Clock, Square } from "../../primitives/icons";

export function SendControls({
  streaming,
  canSend,
  disconnected,
  onCancel,
}: {
  streaming: boolean;
  canSend: boolean;
  disconnected: boolean;
  onCancel: () => void;
}) {
  return (
    <div class="codex-send-controls flex flex-none items-center gap-1.5">
      <button
        type="submit"
        disabled={!canSend}
        class={`codex-send-button flex-none w-10 h-10 rounded-lg
                ${streaming ? "bg-accent-blue hover:bg-accent-blue/85" : "bg-accent-green hover:bg-accent-green/85"}
                disabled:bg-ink-500 disabled:cursor-not-allowed
                active:scale-[0.98] disabled:active:scale-100 grid place-items-center text-ink-900 transition`}
        aria-label={streaming ? "Queue prompt" : "Send"}
        title={canSend ? (streaming ? "Queue prompt" : "Send") : disconnected ? "Connecting" : "Send"}
      >
        {streaming ? <Clock class="w-4 h-4" /> : <ArrowUp class="w-4 h-4" />}
      </button>
      {streaming && (
        <button
          type="button"
          onClick={onCancel}
          class="codex-cancel-button flex-none w-10 h-10 rounded-lg bg-accent-red hover:bg-accent-red/85
                 active:scale-[0.98] grid place-items-center text-ink-900 transition"
          aria-label="Cancel"
          title="Cancel current generation"
        >
          <Square class="w-3.5 h-3.5" />
        </button>
      )}
    </div>
  );
}
