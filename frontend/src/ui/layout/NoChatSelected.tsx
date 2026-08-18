import type { ComponentChildren, ComponentType } from "preact";
import { Bot, Folder, Menu, MessageSquare, Plus } from "../primitives/icons";

/**
 * The main area with nothing selected.
 *
 * A workspace with no projects is somebody's first minute with the platform,
 * so it gets the three steps that turn an empty server into a working chat
 * rather than a single "New project" button and a paragraph. Once there are
 * projects the same space goes back to being a short prompt to pick one.
 */
export function NoChatSelected({
  hasProjects,
  onNewProject,
  onNewChat,
  onOpenAgentSettings,
  onHamburger,
}: {
  hasProjects: boolean;
  onNewProject: () => void;
  onNewChat: () => void;
  onOpenAgentSettings: () => void;
  onHamburger: () => void;
}) {
  return (
    <div class="flex-1 flex flex-col min-h-0">
      <header class="codex-header top-chrome flex-none z-20 bg-[#101318] border-b border-white/10 px-3 pb-2 flex items-center gap-2 min-h-[52px]">
        <button
          type="button"
          onClick={onHamburger}
          class="md:hidden h-10 w-10 text-ink-100 rounded-md hover:bg-white/[0.08] grid place-items-center"
          aria-label="Toggle sidebar"
        >
          <Menu class="w-5 h-5" />
        </button>
        <span class="text-sm text-ink-200">
          {hasProjects ? "No chat selected" : "Welcome to Remote"}
        </span>
      </header>

      <div class="flex-1 overflow-y-auto touch-scroll grid place-items-center p-5">
        {hasProjects ? (
          <div class="max-w-sm space-y-5 text-center">
            <div class="mx-auto w-16 h-16 rounded-lg bg-white/[0.06] border border-white/10 grid place-items-center">
              <MessageSquare class="w-8 h-8 opacity-70" />
            </div>
            <div class="text-ink-200">
              <div class="font-semibold text-lg text-ink-50">Choose a chat or start fresh</div>
              <div class="text-xs mt-2 leading-relaxed text-ink-300">
                Pick a project on the left, then create a chat inside it. Each project is its own
                sandboxed container.
              </div>
            </div>
            <div class="flex gap-2 justify-center">
              <button
                type="button"
                onClick={onNewProject}
                class="inline-flex items-center gap-2 bg-accent-blue hover:bg-accent-blue/85 active:scale-[0.99]
                       text-ink-900 text-sm font-medium px-4 h-11 rounded-md transition"
              >
                <Folder class="w-4 h-4" /> New project
              </button>
              <button
                type="button"
                onClick={onNewChat}
                class="inline-flex items-center gap-2 bg-white/[0.08] hover:bg-white/[0.12]
                       text-ink-100 text-sm font-medium px-4 h-11 rounded-md transition"
              >
                <Plus class="w-4 h-4" /> Loose chat
              </button>
            </div>
          </div>
        ) : (
          <div class="w-full max-w-lg">
            <div class="text-center">
              <div class="text-xl font-semibold text-ink-50">Three steps to your first chat</div>
              <p class="mt-1.5 text-[13px] leading-relaxed text-ink-300">
                Remote runs coding agents inside sandboxed containers you own. Nothing is
                installed on your machine.
              </p>
            </div>

            <ol class="mt-5 space-y-2.5">
              <WelcomeStep
                step={1}
                Icon={Bot}
                title="Connect an agent"
                hint="Sign in to Claude, Codex or Kimi once on the host. Every project shares that login."
              >
                <button
                  type="button"
                  onClick={onOpenAgentSettings}
                  class="inline-flex h-9 items-center gap-2 rounded-md bg-white/[0.08] px-3 text-[13px]
                         font-medium text-ink-100 transition hover:bg-white/[0.12]"
                >
                  Open agent settings
                </button>
              </WelcomeStep>

              <WelcomeStep
                step={2}
                Icon={Folder}
                title="Create a project"
                hint="A project is a container with a stack preinstalled — WordPress, Laravel, Node, Python, or blank."
              >
                <button
                  type="button"
                  onClick={onNewProject}
                  class="inline-flex h-9 items-center gap-2 rounded-md bg-accent-blue px-3 text-[13px]
                         font-medium text-ink-900 transition hover:bg-accent-blue/85"
                >
                  <Plus class="w-4 h-4" /> New project
                </button>
              </WelcomeStep>

              <WelcomeStep
                step={3}
                Icon={MessageSquare}
                title="Start a chat"
                hint="Ask for what you want. The agent edits files, runs commands, and previews the result inside the container."
              >
                <span class="text-[12.5px] text-ink-400">
                  Available once the first project exists.
                </span>
              </WelcomeStep>
            </ol>
          </div>
        )}
      </div>
    </div>
  );
}

function WelcomeStep({
  step,
  Icon,
  title,
  hint,
  children,
}: {
  step: number;
  Icon: ComponentType<{ class?: string }>;
  title: string;
  hint: string;
  children: ComponentChildren;
}) {
  return (
    <li class="flex items-start gap-3 rounded-lg border border-white/10 bg-[#101318] p-3.5">
      <span
        class="grid h-8 w-8 flex-none place-items-center rounded-md bg-white/[0.06] text-ink-200"
        aria-hidden="true"
      >
        <Icon class="h-4 w-4" />
      </span>
      <div class="min-w-0 flex-1">
        <div class="text-[14px] font-semibold text-ink-50">
          <span class="text-ink-400 tabular-nums">{step}.</span> {title}
        </div>
        <p class="mt-1 text-[12.5px] leading-relaxed text-ink-300">{hint}</p>
        <div class="mt-2.5">{children}</div>
      </div>
    </li>
  );
}
