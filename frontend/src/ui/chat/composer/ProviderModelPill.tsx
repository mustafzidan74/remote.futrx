import { useEffect, useRef, useState } from "preact/hooks";
import type { ChatModelPolicy, ChatProvider } from "../../../models/chat";
import type { RoutingDecision } from "../../../models/modelRouting";
import {
  PROVIDER_OPTIONS,
  modelDisplayLabel,
  modelOptionsForProvider,
  providerDisplayLabel,
} from "../../../config/chat";
import { Bot, ChevronDown, Zap } from "../../primitives/icons";

/**
 * Agent and model in one pill: "Claude · Opus".
 *
 * They used to be a four-way segmented control and a separate select, which
 * spent a third of the toolbar on a pair of settings that are read far more
 * often than they are changed. Reading them is now free; changing them is one
 * click, and the two lists sit in the same panel because picking an agent is
 * almost always followed by picking its model.
 *
 * The pill also owns the chat's model policy. On Auto the platform picks per
 * turn, so the pill stops naming a fixed model and names the one the *next*
 * turn will use instead — otherwise the operator would be reading a label
 * that no longer describes what is about to run.
 */
export function ProviderModelPill({
  provider,
  model,
  modelPolicy,
  routedNext,
  routingAvailable,
  streaming,
  onProviderChange,
  onModelChange,
  onModelPolicyChange,
}: {
  provider: ChatProvider;
  model: string;
  modelPolicy: ChatModelPolicy;
  /** What routing would pick for the next turn, when the chat is on Auto. */
  routedNext?: RoutingDecision | null;
  /** False on a deployment with no routing policy: the Auto entry is hidden. */
  routingAvailable: boolean;
  streaming: boolean;
  onProviderChange: (provider: ChatProvider) => void;
  onModelChange: (model: string) => void;
  onModelPolicyChange: (policy: ChatModelPolicy) => void;
}) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement>(null);
  const options = modelOptionsForProvider(provider);
  const auto = modelPolicy === "auto";
  const pinnedLabel = `${providerDisplayLabel(provider)} · ${modelDisplayLabel(model, provider)}`;
  const label = auto ? `Auto → ${routedLabel(routedNext)}` : pinnedLabel;
  const title = auto
    ? routedNext?.reason || "The platform picks the model for each turn"
    : "Choose the agent and model";

  useEffect(() => {
    if (!open) return;
    function closeOnOutsideClick(event: MouseEvent) {
      const target = event.target as Node | null;
      if (target && !rootRef.current?.contains(target)) setOpen(false);
    }
    // Captured and swallowed: an open menu must eat Escape so it does not also
    // reach the chat shortcut that cancels a running agent.
    function closeOnEscape(event: KeyboardEvent) {
      if (event.key !== "Escape") return;
      event.stopPropagation();
      setOpen(false);
    }
    window.addEventListener("mousedown", closeOnOutsideClick);
    window.addEventListener("keydown", closeOnEscape, true);
    return () => {
      window.removeEventListener("mousedown", closeOnOutsideClick);
      window.removeEventListener("keydown", closeOnEscape, true);
    };
  }, [open]);

  useEffect(() => {
    if (streaming) setOpen(false);
  }, [streaming]);

  function pickModel(value: string) {
    setOpen(false);
    // Re-picking the model a chat already has still has to leave Auto, so the
    // change is forwarded whenever the chat is routed even if the id matches.
    if (value !== model || auto) onModelChange(value);
  }

  return (
    <div ref={rootRef} class="codex-agent-pill-root relative flex-none">
      <button
        type="button"
        onClick={() => setOpen((current) => !current)}
        disabled={streaming}
        class={`composer-pill max-w-[8.5rem] sm:max-w-[13rem] ${open ? "composer-pill-active" : ""}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={`Agent and model — ${label}`}
        title={streaming ? "Cannot change the agent while it is working" : title}
      >
        {auto ? (
          <Zap class="h-4 w-4 flex-none text-accent-blue" aria-hidden="true" />
        ) : (
          <Bot class="h-4 w-4 flex-none text-ink-400" aria-hidden="true" />
        )}
        <span class="min-w-0 truncate font-semibold text-ink-100">{label}</span>
        <ChevronDown class="h-4 w-4 flex-none text-ink-400" aria-hidden="true" />
      </button>

      {open && (
        <div
          class="popover-surface theme-menu-surface absolute bottom-full left-0 z-40 mb-2
                 w-[min(21rem,calc(100vw-1.5rem))]"
          role="dialog"
          aria-label="Agent and model"
        >
          <div class="popover-section">
            <div class="popover-label">Agent</div>
            <div class="mt-1.5 grid grid-cols-2 gap-1">
              {PROVIDER_OPTIONS.map((option) => {
                const active = option.value === provider;
                return (
                  <button
                    key={option.value}
                    type="button"
                    onClick={() => {
                      if (option.value !== provider) onProviderChange(option.value);
                    }}
                    class={`h-8 rounded-md px-2 text-[12px] font-semibold transition
                            ${active
                              ? "bg-accent-blue text-ink-900"
                              : "bg-white/[0.05] text-ink-200 hover:bg-white/[0.09] hover:text-ink-50"}`}
                    aria-pressed={active}
                  >
                    {option.label}
                  </button>
                );
              })}
            </div>
          </div>

          <div class="popover-section max-h-[13rem] overflow-y-auto touch-scroll scrollbar-thin">
            <div class="popover-label">Model</div>
            <div class="mt-1 space-y-0.5" role="listbox" aria-label="Model">
              {routingAvailable && (
                <button
                  type="button"
                  onClick={() => {
                    setOpen(false);
                    onModelPolicyChange("auto");
                  }}
                  class={`flex w-full items-center justify-between gap-3 rounded-md px-2.5 py-2 text-left transition
                          ${auto ? "bg-accent-blue/[0.14] text-accent-blue" : "text-ink-100 hover:bg-white/[0.07]"}`}
                  role="option"
                  aria-selected={auto}
                >
                  <span class="min-w-0">
                    <span class="flex items-center gap-1 text-[12.5px] font-semibold">
                      <Zap class="h-3 w-3 flex-none" aria-hidden="true" />
                      Auto
                    </span>
                    <span class="block truncate text-[11.5px] text-ink-300">
                      {routedNext
                        ? `next turn: ${routedLabel(routedNext)}`
                        : "the platform picks per turn"}
                    </span>
                  </span>
                  {auto && <span class="h-2 w-2 flex-none rounded-full bg-accent-blue" />}
                </button>
              )}
              {model && !options.some((option) => option.value === model) && (
                <button
                  type="button"
                  onClick={() => pickModel(model)}
                  class={`w-full rounded-md px-2.5 py-2 text-left ${
                    auto ? "text-ink-100 hover:bg-white/[0.07]" : "bg-accent-blue/[0.14] text-accent-blue"
                  }`}
                  role="option"
                  aria-selected={!auto}
                >
                  <span class="block text-[12.5px] font-semibold">{model}</span>
                  <span class="block text-[11.5px] text-ink-300">custom model</span>
                </button>
              )}
              {options.map((option) => {
                const active = !auto && (model || "") === option.value;
                return (
                  <button
                    key={option.value || "auto"}
                    type="button"
                    onClick={() => pickModel(option.value)}
                    class={`flex w-full items-center justify-between gap-3 rounded-md px-2.5 py-2 text-left transition
                            ${active ? "bg-accent-blue/[0.14] text-accent-blue" : "text-ink-100 hover:bg-white/[0.07]"}`}
                    role="option"
                    aria-selected={active}
                  >
                    <span class="min-w-0">
                      <span class="block truncate text-[12.5px] font-semibold">{option.label}</span>
                      <span class="block truncate text-[11.5px] text-ink-300">{option.sub}</span>
                    </span>
                    {active && <span class="h-2 w-2 flex-none rounded-full bg-accent-blue" />}
                  </button>
                );
              })}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

/** How the routed destination reads in the pill and in the Auto entry. */
function routedLabel(decision?: RoutingDecision | null): string {
  if (!decision) return "picking…";
  const provider = decision.provider as ChatProvider;
  return `${providerDisplayLabel(provider)} ${modelDisplayLabel(decision.model, provider)}`;
}
