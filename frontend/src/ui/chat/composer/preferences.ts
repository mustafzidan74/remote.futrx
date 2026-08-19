import type {
  ChatMode,
  ChatModelPolicy,
  ChatProvider,
  ReasoningEffort,
  ServiceTier,
} from "../../../models/chat";
import type { RoutingDecision } from "../../../models/modelRouting";

export interface ComposerPreferences {
  provider: ChatProvider;
  model: string;
  mode: ChatMode;
  reasoningEffort: ReasoningEffort;
  serviceTier: ServiceTier;
  /** Whether the chat is pinned to the model above or routed per turn. */
  modelPolicy: ChatModelPolicy;
}

/**
 * The routing hint the pill shows on Auto. It is a separate shape from the
 * preferences because it is a server answer about the *next* turn, not a
 * setting anyone stored.
 */
export interface ComposerRoutingHint {
  decision: RoutingDecision | null;
  /** False on a deployment with no routing policy: Auto is not offered. */
  available: boolean;
}

export interface ComposerPreferenceActions {
  changeProvider: (provider: ChatProvider) => void;
  changeModel: (model: string) => void;
  changeModelPolicy: (policy: ChatModelPolicy) => void;
  changeMode: (mode: ChatMode) => void;
  changeReasoningEffort: (reasoningEffort: ReasoningEffort) => void;
  changeServiceTier: (serviceTier: ServiceTier) => void;
}
