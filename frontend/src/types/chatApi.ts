import type { ChatEvent, SyntheticKind } from "../models/chat";

export interface ChatStream {
  readonly isOpen: boolean;
  /**
   * `synthetic` labels a prompt the composer built rather than one the user
   * typed, so the transcript can badge it. The server validates the label.
   */
  sendPrompt(text: string, clientId?: string, synthetic?: SyntheticKind): boolean;
  cancel(): boolean;
  close(): void;
}

export interface ChatStreamCallbacks {
  onOpen: () => void;
  onEvent: (event: ChatEvent) => void;
  onClose: () => void;
}
