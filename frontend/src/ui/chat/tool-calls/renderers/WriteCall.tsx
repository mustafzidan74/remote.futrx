import { File } from "../../../primitives/icons";
import type { ToolCallProps } from "../ToolCallTypes";
import { CodeBlock } from "../CodeBlock";
import { ToolShell } from "../ToolShell";
import { shortPath, truncate } from "../utils";

export function WriteCall({ input, output, status, isError, defaultOpen }: Omit<ToolCallProps, "name">) {
  const path = (input?.file_path as string) ?? "";
  const content = (input?.content as string) ?? "";
  return (
    <ToolShell
      icon={<File class="w-4 h-4" />}
      label={<><span class="text-ink-300">Write</span> <span class="font-mono">{shortPath(path)}</span></>}
      badge={`${content.split("\n").length} lines`}
      status={status}
      isError={isError}
      defaultOpen={defaultOpen}
    >
      <CodeBlock text={truncate(content, 8000)} />
      {output && isError ? <div class="border-t border-ink-500 p-3 text-accent-red font-mono text-xs">{output}</div> : null}
    </ToolShell>
  );
}
