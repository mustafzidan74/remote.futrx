import { File } from "../../../primitives/icons";
import type { ToolCallProps } from "../ToolCallTypes";
import { CodeBlock } from "../CodeBlock";
import { ToolShell } from "../ToolShell";
import { shortPath, truncate } from "../utils";

export function ReadCall({ input, output, status, isError, defaultOpen }: Omit<ToolCallProps, "name">) {
  const path = (input?.file_path as string) ?? "";
  return (
    <ToolShell
      icon={<File class="w-4 h-4" />}
      label={<><span class="text-ink-300">Read</span> <span class="font-mono">{shortPath(path)}</span></>}
      status={status}
      isError={isError}
      defaultOpen={defaultOpen}
    >
      {output ? <CodeBlock text={truncate(output, 8000)} /> : null}
    </ToolShell>
  );
}
