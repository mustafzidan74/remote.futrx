import type {
  ChatMode,
  ChatModelPolicy,
  ApprovalPolicy,
  ChatProvider,
  ReasoningEffort,
  ServiceTier,
  SandboxPolicy,
} from "../../../models/chat";
import type { RoutingDecision } from "../../../models/modelRouting";

export interface ComposerPreferences {
  provider: ChatProvider;
  model: string;
  mode: ChatMode;
  reasoningEffort: ReasoningEffort;
  serviceTier: ServiceTier;
  approvalPolicy: ApprovalPolicy;
  sandboxPolicy: SandboxPolicy;
  /** Whether the chat is pinned to the model above or routed per turn. */
  modelPolicy: ChatModelPolicy;
  /**
   * The third-party agent endpoint this chat is pointed at, "" for the
   * vendor's own. An endpoint pins the chat, so it outranks modelPolicy.
   */
  endpointId: string;
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
  changeAgent: (provider: ChatProvider, model: string) => void;
  changeProvider: (provider: ChatProvider) => void;
  changeModel: (model: string) => void;
  changeModelPolicy: (policy: ChatModelPolicy) => void;
  /**
   * Points the chat at a third-party endpoint, or back at the vendor's own
   * with an empty id. The CLI and model travel with it because all three are
   * one choice: the endpoint decides which command line runs and which model
   * ids it may ask for.
   */
  changeEndpoint: (endpointId: string, cli: ChatProvider, model: string) => void;
  changeMode: (mode: ChatMode, modelPreset?: string, reasoningPreset?: string) => void;
  changeReasoningEffort: (reasoningEffort: ReasoningEffort) => void;
  changeServiceTier: (serviceTier: ServiceTier) => void;
  changeApprovalPolicy: (approvalPolicy: ApprovalPolicy) => void;
  changeSandboxPolicy: (sandboxPolicy: SandboxPolicy) => void;
}
