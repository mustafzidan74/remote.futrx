import { AlertCircle } from "../../primitives/icons";

export function ErrorMessage({ message }: { message: string }) {
  return (
    <div class="flex items-start gap-2 text-accent-red text-sm rounded-lg border border-accent-red/25 bg-accent-red/[0.08] px-3 py-2">
      <AlertCircle class="w-4 h-4 flex-none mt-0.5" />
      <div dir="auto" class="bidi-auto min-w-0 break-words">{message}</div>
    </div>
  );
}
