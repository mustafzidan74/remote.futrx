import { TerminalIcon } from "../../../primitives/icons";
import type { ToolCallProps } from "../ToolCallTypes";
import { CodeBlock } from "../CodeBlock";
import { ToolShell } from "../ToolShell";
import { shortPath, truncate } from "../utils";

export function SearchCall({ name, input, output, status, isError, defaultOpen }: ToolCallProps) {
  const pattern = (input?.pattern as string) ?? (input?.query as string) ?? "";
  const path = (input?.path as string) ?? "";
  return (
    <ToolShell
      icon={<TerminalIcon class="w-4 h-4" />}
      label={
        <>
          <span class="text-ink-300">{name}</span>{" "}
          <span class="font-mono">{pattern}</span>
          {path && <span class="text-ink-300 ml-1">in {shortPath(path)}</span>}
        </>
      }
      status={status}
      isError={isError}
      defaultOpen={defaultOpen}
    >
      {output ? <CodeBlock text={truncate(output, 6000)} /> : null}
    </ToolShell>
  );
}
