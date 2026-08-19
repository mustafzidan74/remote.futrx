import { TerminalIcon } from "../../../primitives/icons";
import type { ToolCallProps } from "../ToolCallTypes";
import { CodeBlock } from "../CodeBlock";
import { ToolShell } from "../ToolShell";
import { truncate } from "../utils";

export function GenericCall({ name, input, output, status, isError, defaultOpen }: ToolCallProps) {
  return (
    <ToolShell
      icon={<TerminalIcon class="w-4 h-4" />}
      label={<span class="text-ink-300">{name}</span>}
      status={status}
      isError={isError}
      defaultOpen={defaultOpen}
    >
      <div class="divide-y divide-ink-500">
        {input && Object.keys(input).length > 0 && (
          <div>
            <div class="px-3 py-1 text-[11px] text-ink-300 bg-white/[0.04]">Input</div>
            <CodeBlock text={JSON.stringify(input, null, 2)} lang="json" />
          </div>
        )}
        {output && (
          <div>
            <div class="px-3 py-1 text-[11px] text-ink-300 bg-white/[0.04]">Output</div>
            <CodeBlock text={truncate(output, 6000)} />
          </div>
        )}
      </div>
    </ToolShell>
  );
}
