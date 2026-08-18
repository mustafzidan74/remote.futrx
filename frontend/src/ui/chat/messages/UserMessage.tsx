import { RotateCcw } from "../../primitives/icons";

export function UserMessage({
  text,
  t,
  onRewind,
}: {
  text: string;
  t: number;
  onRewind?: (t: number, text: string) => void;
}) {
  return (
    <div class="group flex justify-end">
      <div class="max-w-[92%] sm:max-w-[78%] flex flex-col items-end gap-1.5">
        <div
          dir="auto"
          class="codex-user-bubble bidi-auto bg-accent-blue/15 border border-accent-blue/30
                 rounded-[18px] rounded-br-md px-3.5 py-2.5 text-[14.5px] leading-relaxed
                 whitespace-pre-wrap break-words shadow-sm">
          {text}
        </div>
        {onRewind && (
          <button
            type="button"
            onClick={() => onRewind(t, text)}
            class="inline-flex items-center gap-1.5 h-7 px-2 rounded-md text-[12px]
                   text-ink-300 hover:text-ink-100 hover:bg-white/[0.07]
                   opacity-100 md:opacity-0 md:group-hover:opacity-100 transition"
            title="Rewind and edit from here"
          >
            <RotateCcw class="w-3.5 h-3.5" />
            Rewind
          </button>
        )}
      </div>
    </div>
  );
}
