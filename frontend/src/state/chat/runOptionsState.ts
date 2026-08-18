import {
  CHAT_MODE_OPTIONS,
  reasoningEffortOptionsForProvider,
  serviceTierOptionsForProvider,
  type ChatMode,
  type ChatProvider,
  type ReasoningEffort,
  type ServiceTier,
} from "../../config/chatCatalog.ts";

/**
 * The composer toolbar used to spell every run setting out as its own control.
 * They now live behind one "Run options" button, so the button has to say what
 * is set without being opened — this is that sentence.
 *
 * Only settings that differ from "let the provider decide" earn a word: a
 * summary that always reads "Code · Auto · Auto" teaches nothing, while
 * "Full auto · XHigh · 🛫" is the whole state at a glance.
 */

export interface RunOptionsState {
  provider: ChatProvider;
  mode: ChatMode;
  reasoningEffort: ReasoningEffort;
  serviceTier: ServiceTier;
  autopilot: boolean;
  autoTest: boolean;
}

export const AUTOPILOT_BADGE = "🛫";
export const AUTO_TEST_BADGE = "🧪";

/** "Full auto · XHigh · 🛫" — the label printed on the Run options button. */
export function runOptionsSummary(state: RunOptionsState): string {
  const parts = [modeLabel(state.mode)];
  const thinking = reasoningEffortLabel(state.provider, state.reasoningEffort);
  if (thinking) parts.push(thinking);
  const speed = serviceTierLabel(state.provider, state.serviceTier);
  if (speed) parts.push(speed);
  if (state.autopilot) parts.push(AUTOPILOT_BADGE);
  if (state.autoTest) parts.push(AUTO_TEST_BADGE);
  return parts.join(" · ");
}

/**
 * The same sentence in words. Emoji read aloud as "airplane departure", which
 * is not what a screen-reader user needs to hear about a scheduling policy.
 */
export function runOptionsAriaLabel(state: RunOptionsState): string {
  const parts = [`${modeLabel(state.mode)} mode`];
  const thinking = reasoningEffortLabel(state.provider, state.reasoningEffort);
  if (thinking) parts.push(`${thinking} thinking`);
  const speed = serviceTierLabel(state.provider, state.serviceTier);
  if (speed) parts.push(`${speed} speed`);
  if (state.autopilot) parts.push("autopilot on");
  if (state.autoTest) parts.push("auto-test on");
  return `Run options: ${parts.join(", ")}`;
}

function modeLabel(mode: ChatMode): string {
  return CHAT_MODE_OPTIONS.find((option) => option.value === mode)?.label ?? "Chat";
}

/** Empty when the provider has no ladder, or the chat is on the provider default. */
function reasoningEffortLabel(provider: ChatProvider, effort: ReasoningEffort): string {
  if (!effort) return "";
  const options = reasoningEffortOptionsForProvider(provider);
  return options.find((option) => option.value === effort)?.label ?? "";
}

function serviceTierLabel(provider: ChatProvider, tier: ServiceTier): string {
  if (!tier) return "";
  const options = serviceTierOptionsForProvider(provider);
  return options.find((option) => option.value === tier)?.label ?? "";
}
