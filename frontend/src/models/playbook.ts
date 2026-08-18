import type { ChatMode, ChatProvider } from "./chat";

/** A skill a playbook preselects, in the shape the chat store already keeps. */
export interface PlaybookSkillRef {
  name?: string;
  command?: string;
  provider?: ChatProvider;
  source?: string;
}

/**
 * A one-click prompt template offered in the composer. The library is
 * server-owned and admin-editable; every signed-in user reads the same list.
 */
export interface Playbook {
  id: string;
  title: string;
  icon?: string;
  /** One-line description shown under the title in the composer menu. */
  hint?: string;
  /** May contain `{{project}}`, `{{slug}}`, `{{previewUrl}}` placeholders. */
  prompt: string;
  skills?: PlaybookSkillRef[];
  mode?: ChatMode | "";
  provider?: ChatProvider | "";
  order: number;
}
