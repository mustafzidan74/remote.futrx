import { TerminalIcon } from "../../../primitives/icons";
import type { ToolCallProps } from "../ToolCallTypes";
import { CodeBlock } from "../CodeBlock";
import { ToolShell } from "../ToolShell";
import { truncate } from "../utils";

export function BashCall({ input, output, status, isError, defaultOpen }: Omit<ToolCallProps, "name">) {
  const command = (input?.command as string) ?? "";
  const description = (input?.description as string) ?? "";
  return (
    <ToolShell
      icon={<TerminalIcon class="w-4 h-4" />}
      label={<span class="font-mono text-[12.5px]"><span class="text-ink-300">$ </span>{truncate(command, 120)}</span>}
      badge={description ? truncate(description, 30) : undefined}
      status={status}
      isError={isError}
      defaultOpen={defaultOpen}
    >
      {output ? <CodeBlock text={truncate(output, 6000)} /> : null}
    </ToolShell>
  );
}
