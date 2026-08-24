/**
 * Completion-API models a chat can be pointed at: the enabled free-tier
 * providers, and the local model on the server.
 *
 * These are not agents. They answer from the conversation alone — no files, no
 * shell, no repository — which is why the composer keeps them in their own
 * section and the chat header says so while one is in force. What they buy is
 * cost: a question that does not need the repository does not need to spend an
 * agent subscription.
 */
export type DirectModelSource = "pool" | "local";

export interface DirectModelChoice {
  source: DirectModelSource;
  /** The provider's registry id. Absent for the local model. */
  providerId?: string;
  /** The vendor's name, or "On this server" for the local model. */
  providerLabel?: string;
  model: string;
  modelLabel?: string;
}

/** A chat's choice. An unset `source` means the chat runs an agent. */
export interface DirectModelRef {
  source?: DirectModelSource | "";
  providerId?: string;
  model?: string;
}

export const NO_DIRECT_MODEL: DirectModelRef = { source: "", providerId: "", model: "" };

/** True when this chat answers from a completion API rather than an agent. */
export function isDirect(ref: DirectModelRef | undefined | null): boolean {
  return !!ref?.source;
}

/** Whether a stored choice and an offered one are the same model. */
export function sameDirectModel(ref: DirectModelRef | undefined, choice: DirectModelChoice): boolean {
  if (!ref?.source) return false;
  return (
    ref.source === choice.source &&
    (ref.providerId || "") === (choice.providerId || "") &&
    (ref.model || "") === choice.model
  );
}

/** What the pill and the header call this model. */
export function directModelLabel(ref: DirectModelRef | undefined): string {
  return ref?.model || (ref?.source === "local" ? "local model" : "direct model");
}

/**
 * The chat header's badge for an answer-only chat.
 *
 * It exists to prevent one specific confusion: an operator asks a chat to fix
 * a file, gets a polite explanation instead of an edit, and cannot see why.
 * The badge names the model and says plainly what it cannot do.
 */
export function directBadge(
  ref: DirectModelRef | undefined,
  choices: DirectModelChoice[],
): { short: string; title: string } | null {
  if (!isDirect(ref)) return null;
  const match = choices.find((choice) => sameDirectModel(ref, choice));
  const modelName = match?.modelLabel || ref?.model || "a direct model";
  const where = match?.providerLabel || (ref?.source === "local" ? "on this server" : "");
  return {
    short: where ? `${modelName} · ${where}` : modelName,
    title: `${modelName} answers this chat. It cannot read or write files, run commands, or see the repository — switch to an agent for that.`,
  };
}
