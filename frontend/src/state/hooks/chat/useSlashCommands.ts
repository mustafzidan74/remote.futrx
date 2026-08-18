import type { RefObject } from "preact";
import { useCallback, useEffect, useMemo, useRef, useState } from "preact/hooks";
import { projectApi } from "../../../api/projectApi";
import { PUBLIC_HOSTNAME } from "../../../config/runtime";
import type { ChatMode, ChatProvider } from "../../../models/chat";
import type { ProjectMeta } from "../../../models/project";
import type { ProjectScreenshot, ScreenshotDelivery } from "../../../models/screenshot";
import type { RegisteredSkill } from "../../../models/skill";
import { buildProjectPreviewUrl } from "../../../shared/projectPreviewUrls";
import {
  DEPLOY_FALLBACK_PROMPT,
  REVIEW_PROMPT,
  applySlashCommand,
  buildSlashRegistry,
  filterSlashCommands,
  findSlashCommand,
  parseSlashInput,
  parsePortArg,
  parseBrowserArg,
  parseOnOffArg,
  pickDeployPlaybook,
  pickDeploySkill,
  pickReviewSkill,
  skillInvocation,
  slashHelpText,
  slashTokenAt,
  type SlashCommand,
} from "../../chat/slashCommandState";
import { testCommandPrompt } from "../../chat/testPrompts";
import { isShareablePort } from "../../projects/projectShareState";
import {
  githubActions,
  needsNewBranch,
  suggestBranchName,
} from "../../projects/githubPanelState";
import { GitHubDirtyWorkspaceError } from "../../../models/github";
import type { ChatPolicies } from "./useChatPolicies";
import type { PlaybookLibrary } from "./usePlaybooks";
import { useAgentBrowserOpener } from "../projects/useAgentBrowserOpener";
import { useAvailableSkills } from "./useAvailableSkills";

/**
 * Slash commands in the composer.
 *
 * The hook owns two things the pure registry cannot: where the caret is, and
 * what each command actually does. Every action it performs already exists as
 * a button somewhere — the Test menu, the Playbooks menu, the autopilot
 * popover, the preview chip — and is reached here through the very same
 * handler, so a command and its button can never drift apart.
 */

export interface SlashStatus {
  tone: "info" | "error" | "busy";
  text: string;
  /** Rendered as a monospace block under the text: /help output, sink results. */
  detail?: string;
}

export interface SlashScreenshotCard {
  screenshot: ProjectScreenshot;
  projectId: string;
  delivered?: ScreenshotDelivery[];
  publicUrl?: string;
  /** False when no notification sink is configured on this server. */
  canSend: boolean;
}

export interface SlashCommands {
  open: boolean;
  items: SlashCommand[];
  activeIndex: number;
  /** What has been typed after the slash, echoed in the menu footer. */
  query: string;
  /** Mouse hover moves the selection, so a click never picks a stale row. */
  setActiveIndex: (index: number) => void;
  status: SlashStatus | null;
  screenshot: SlashScreenshotCard | null;
  /** True while a command is doing network work. */
  busy: boolean;
  /** Handles a textarea keystroke; true means the menu consumed it. */
  onKeyDown: (event: KeyboardEvent) => boolean;
  /** Re-reads the caret after input, a click, or an arrow key. */
  syncCaret: () => void;
  select: (entry: SlashCommand, options?: { send?: boolean }) => void;
  close: () => void;
  dismissStatus: () => void;
  dismissScreenshot: () => void;
  /** Wraps the composer's send: true when a command consumed the message. */
  interceptSend: () => boolean;
  /** Sends a stored capture through the notification sinks. */
  sendScreenshot: (card: SlashScreenshotCard) => void;
  /** Puts a capture's link into the draft. */
  insertScreenshot: (card: SlashScreenshotCard) => void;
}

export interface SlashCommandInput {
  project: ProjectMeta | null;
  provider: ChatProvider;
  text: string;
  setText: (text: string) => void;
  textareaRef: RefObject<HTMLTextAreaElement>;
  insertText: (value: string, select?: { start: number; end: number }) => void;
  submitTest: (value: string) => boolean;
  playbooks: PlaybookLibrary;
  policies: ChatPolicies;
  changeMode: (mode: ChatMode) => void;
  selectSkill: (skill: RegisteredSkill) => void;
  /** Reveals the workspace Browser pane once the Agent Browser is driven. */
  onAgentBrowserOpened?: () => void;
}

export function useSlashCommands({
  project,
  provider,
  text,
  setText,
  textareaRef,
  insertText,
  submitTest,
  playbooks,
  policies,
  changeMode,
  selectSkill,
  onAgentBrowserOpened,
}: SlashCommandInput): SlashCommands {
  const [caret, setCaret] = useState(0);
  const [activeIndex, setActiveIndex] = useState(0);
  // Escape dismisses the token it was pressed on, not the menu forever: the
  // very next keystroke would otherwise re-open what the user just closed.
  const [dismissedAt, setDismissedAt] = useState<number | null>(null);
  const [status, setStatus] = useState<SlashStatus | null>(null);
  const [screenshot, setScreenshot] = useState<SlashScreenshotCard | null>(null);
  const [busy, setBusy] = useState(false);
  // "/skills" pins the menu to one group rather than opening a second picker.
  const [groupFilter, setGroupFilter] = useState<SlashCommand["group"] | null>(null);
  const alive = useRef(true);

  const projectId = project?.id ?? null;
  const { skills } = useAvailableSkills(provider, projectId ?? undefined);
  const agentBrowser = useAgentBrowserOpener({
    projectId,
    onOpened: onAgentBrowserOpened ? () => onAgentBrowserOpened() : undefined,
  });

  useEffect(() => {
    alive.current = true;
    return () => {
      alive.current = false;
    };
  }, []);

  const registry = useMemo(
    () => buildSlashRegistry({ playbooks: playbooks.playbooks, skills }),
    [playbooks.playbooks, skills],
  );

  const found = slashTokenAt(text, caret);
  const token = found && found.start === dismissedAt ? null : found;
  const items = useMemo(() => {
    if (!token) return [];
    const matched = filterSlashCommands(registry, token.query);
    return groupFilter ? matched.filter((entry) => entry.group === groupFilter) : matched;
  }, [registry, token?.query, token?.start, groupFilter]);
  const open = token !== null && items.length > 0;

  useEffect(() => {
    setActiveIndex(0);
  }, [token?.query, token?.start, groupFilter]);

  // A new draft — a sent message, a switched chat — is a fresh start for both
  // the menu and whatever the last command had to say.
  useEffect(() => {
    if (text === "") {
      setDismissedAt(null);
      setGroupFilter(null);
    }
  }, [text]);

  const syncCaret = useCallback(() => {
    const position = textareaRef.current?.selectionStart;
    setCaret(typeof position === "number" ? position : 0);
  }, [textareaRef]);

  const close = useCallback(() => {
    setDismissedAt(found?.start ?? null);
    setGroupFilter(null);
  }, [found?.start]);

  const focusComposer = useCallback(
    (position: number) => {
      setCaret(position);
      setTimeout(() => {
        const textarea = textareaRef.current;
        textarea?.focus();
        textarea?.setSelectionRange(position, position);
      }, 0);
    },
    [textareaRef],
  );

  const report = useCallback((next: SlashStatus | null) => {
    if (alive.current) setStatus(next);
  }, []);

  /* ---------------------------------------------------------------- *
   * The commands
   * ---------------------------------------------------------------- */

  /**
   * `/skills` is the one command that answers with the menu itself: it leaves
   * a bare slash where the command was and pins the list to the skill group,
   * so the picker opens where the user is already typing rather than as a
   * second popover somewhere else.
   */
  const openSkillMenu = useCallback(
    (baseText: string, anchor: number) => {
      setText(baseText.slice(0, anchor) + "/" + baseText.slice(anchor));
      setGroupFilter("skill");
      setDismissedAt(null);
      focusComposer(anchor + 1);
      report({ tone: "info", text: "Pick a skill to select it and name it in your prompt." });
    },
    [focusComposer, report, setText],
  );

  const requireProject = useCallback((): string | null => {
    if (projectId) return projectId;
    report({ tone: "error", text: "This chat is not attached to a project container." });
    return null;
  }, [projectId, report]);

  const runSnapshot = useCallback(
    async (label: string) => {
      const id = requireProject();
      if (!id) return;
      setBusy(true);
      report({ tone: "busy", text: "Taking a snapshot…" });
      try {
        const record = await projectApi.createSnapshot(id, { label });
        report({
          tone: "info",
          text: record.label
            ? `Snapshot "${record.label}" started. It appears under Project settings → Snapshots when packing finishes.`
            : "Snapshot started. It appears under Project settings → Snapshots when packing finishes.",
        });
      } catch (cause) {
        report({ tone: "error", text: (cause as Error).message || "Could not start the snapshot." });
      } finally {
        if (alive.current) setBusy(false);
      }
    },
    [report, requireProject],
  );

  /**
   * `/pr [title]` opens a pull request from the composer.
   *
   * It reads the panel's own status first rather than guessing, for two
   * reasons: it needs a branch name when the workspace is sitting on the
   * default branch (GitHub refuses a pull request from a branch onto itself),
   * and it needs to know whether to ask about uncommitted changes before it
   * pushes anything.
   *
   * The commit message is never composed here and never written by an agent:
   * the server generates a deterministic one, and this command shows exactly
   * that string in the confirmation.
   */
  const runPullRequest = useCallback(
    async (arg: string) => {
      const id = requireProject();
      if (!id) return;
      const title = arg.trim();
      setBusy(true);
      report({ tone: "busy", text: "Opening a pull request…" });
      try {
        const status = await projectApi.getGitHubStatus(id);
        if (!alive.current) return;
        if (!status.linked) {
          report({
            tone: "error",
            text: "This project is not linked to a GitHub repository. Link one under Project settings → GitHub.",
          });
          return;
        }
        const actions = githubActions(status);
        if (!actions.canOpenPR) {
          report({ tone: "error", text: actions.blockedReason });
          return;
        }
        const head = needsNewBranch(status)
          ? suggestBranchName(title, new Date())
          : (status.branch ?? "");

        let commit = false;
        let commitMessage = status.defaultCommitMessage ?? "";
        if (status.dirty) {
          if (!confirmCommit(status.dirtyCount, commitMessage)) {
            report({ tone: "info", text: "Pull request cancelled." });
            return;
          }
          commit = true;
        }

        const open = (withCommit: boolean, message: string) =>
          projectApi.createGitHubPullRequest(id, {
            title,
            head,
            commit: withCommit,
            commitMessage: message,
          });

        let created;
        try {
          created = await open(commit, commitMessage);
        } catch (cause) {
          // The workspace went dirty between reading the status and pushing,
          // or the status was stale. The server tells us what it would commit;
          // ask once, then retry.
          if (!(cause instanceof GitHubDirtyWorkspaceError)) throw cause;
          commitMessage = cause.defaultCommitMessage;
          if (!confirmCommit(cause.dirtyCount, commitMessage)) {
            report({ tone: "info", text: "Pull request cancelled." });
            return;
          }
          created = await open(true, commitMessage);
        }
        if (!alive.current) return;
        report({
          tone: "info",
          text: `Pull request opened on ${created.branch}.`,
          detail: created.url,
        });
      } catch (cause) {
        report({
          tone: "error",
          text: (cause as Error).message || "Could not open the pull request.",
        });
      } finally {
        if (alive.current) setBusy(false);
      }
    },
    [report, requireProject],
  );

  const openPreview = useCallback(async () => {
    const id = requireProject();
    if (!id || !project) return;
    setBusy(true);
    try {
      const apps = await projectApi.listApps(id);
      const port = apps.map((app) => app.port).filter(isShareablePort).sort((a, b) => a - b)[0];
      if (port === undefined) {
        report({ tone: "error", text: "No app is listening in this project yet." });
        return;
      }
      const url = buildProjectPreviewUrl(project.slug, port, PUBLIC_HOSTNAME);
      if (!url) {
        report({ tone: "error", text: "This deployment has no public preview hostname." });
        return;
      }
      window.open(url, "_blank", "noopener,noreferrer");
      report({ tone: "info", text: `Opened ${url}` });
    } catch (cause) {
      report({ tone: "error", text: (cause as Error).message || "Could not list the project ports." });
    } finally {
      if (alive.current) setBusy(false);
    }
  }, [project, report, requireProject]);

  const runScreenshot = useCallback(
    async (arg: string) => {
      const id = requireProject();
      if (!id) return;
      const explicit = parsePortArg(arg);
      if (arg.trim() && explicit === null) {
        report({ tone: "error", text: "Usage: /screenshot [port between 1024 and 65535]" });
        return;
      }
      setBusy(true);
      setScreenshot(null);
      report({ tone: "busy", text: "Capturing the preview…" });
      try {
        let port = explicit;
        if (port === null) {
          const apps = await projectApi.listApps(id);
          port = apps.map((app) => app.port).filter(isShareablePort).sort((a, b) => a - b)[0] ?? null;
        }
        if (port === null) {
          report({ tone: "error", text: "No app is listening in this project yet." });
          return;
        }
        const result = await projectApi.captureScreenshot(id, { port });
        if (!alive.current) return;
        setScreenshot({
          screenshot: result.screenshot,
          projectId: id,
          canSend: result.notifications,
        });
        report(null);
      } catch (cause) {
        report({ tone: "error", text: (cause as Error).message || "Could not capture the preview." });
      } finally {
        if (alive.current) setBusy(false);
      }
    },
    [report, requireProject],
  );

  const sendScreenshot = useCallback(
    async (card: SlashScreenshotCard) => {
      setBusy(true);
      report({ tone: "busy", text: "Sending the screenshot…" });
      try {
        const result = await projectApi.sendScreenshot(card.projectId, card.screenshot.id);
        if (!alive.current) return;
        setScreenshot({
          ...card,
          delivered: result.delivered,
          publicUrl: result.publicUrl,
        });
        report({
          tone: "info",
          text: "Screenshot sent.",
          detail: (result.delivered ?? [])
            .map((row) => `${row.sink}: ${row.delivered ? "sent" : row.error || "failed"}`)
            .join("\n"),
        });
      } catch (cause) {
        report({ tone: "error", text: (cause as Error).message || "Could not send the screenshot." });
      } finally {
        if (alive.current) setBusy(false);
      }
    },
    [report],
  );

  const insertScreenshot = useCallback(
    (card: SlashScreenshotCard) => {
      const { screenshot: shot } = card;
      insertText(
        `Screenshot of the preview (:${shot.port}${shot.path}, ${shot.width}×${shot.height}): ` +
          `${window.location.origin}${shot.url}`,
      );
      report(null);
      setScreenshot(null);
    },
    [insertText, report],
  );

  const runBrowser = useCallback(
    (arg: string) => {
      if (!requireProject()) return;
      const url = parseBrowserArg(arg);
      if (!url) {
        report({ tone: "error", text: "Usage: /browser <url or port>" });
        return;
      }
      report({ tone: "busy", text: `Loading ${url} in the Agent Browser…` });
      void agentBrowser.openUrl(url).then((result) => {
        if (!alive.current) return;
        report(
          result.ok
            ? { tone: "info", text: `Loaded ${url} in the Agent Browser.` }
            : {
                tone: "error",
                text: result.error || `Could not load ${url} in the Agent Browser.`,
              },
        );
      });
    },
    [agentBrowser, report, requireProject],
  );

  const runDeploy = useCallback(() => {
    const playbook = pickDeployPlaybook(playbooks.playbooks);
    if (playbook) {
      void playbooks.run(playbook);
      report({ tone: "info", text: `Loaded the "${playbook.title}" playbook. Review it, then send.` });
      return;
    }
    const skill = pickDeploySkill(skills);
    if (skill) {
      selectSkill(skill);
      insertText(skillInvocation(skill) + DEPLOY_FALLBACK_PROMPT);
      report({ tone: "info", text: `Selected the ${skill.command || skill.name} skill.` });
      return;
    }
    insertText(DEPLOY_FALLBACK_PROMPT);
    report({
      tone: "info",
      text: "No deploy playbook or skill is installed, so the generic deploy prompt was loaded.",
    });
  }, [insertText, playbooks, report, selectSkill, skills]);

  const runReview = useCallback(() => {
    changeMode("review");
    const skill = pickReviewSkill(skills);
    if (skill) selectSkill(skill);
    insertText(skill ? skillInvocation(skill) + REVIEW_PROMPT : REVIEW_PROMPT);
    report({
      tone: "info",
      text: skill
        ? `Review mode on, ${skill.command || skill.name} selected.`
        : "Review mode on. No review skill is registered on this server.",
    });
  }, [changeMode, insertText, report, selectSkill, skills]);

  const runAutopilot = useCallback(
    (arg: string) => {
      const { on, count } = parseOnOffArg(arg);
      if (on === null) {
        report({ tone: "error", text: "Usage: /autopilot on|off [rounds]" });
        return;
      }
      if (!on) {
        policies.stopAutopilot();
        report({ tone: "info", text: "Autopilot stopped." });
        return;
      }
      const rounds = count ?? policies.autopilot.maxRounds;
      policies.armAutopilot({
        maxRounds: String(rounds),
        maxDurationMin: String(policies.autopilot.maxDurationMin),
      });
      report({ tone: "info", text: `Autopilot armed for ${rounds} rounds.` });
    },
    [policies, report],
  );

  const runAutoTest = useCallback(
    (arg: string) => {
      const { on } = parseOnOffArg(arg);
      if (on === null) {
        report({ tone: "error", text: "Usage: /autotest on|off" });
        return;
      }
      policies.setAutoTest(on);
      report({ tone: "info", text: on ? "Auto-test on." : "Auto-test off." });
    },
    [policies, report],
  );

  const runTest = useCallback(
    (arg: string) => {
      if (!submitTest(testCommandPrompt(arg))) {
        report({
          tone: "error",
          text: "A check cannot start right now: wait for the current run to finish.",
        });
        return;
      }
      report(null);
    },
    [report, submitTest],
  );

  const runPlaybook = useCallback(
    (entry: SlashCommand, send: boolean) => {
      if (!entry.playbook) return;
      void playbooks.run(entry.playbook, { send });
      report(
        send
          ? null
          : { tone: "info", text: `Loaded the "${entry.playbook.title}" playbook. Review it, then send.` },
      );
    },
    [playbooks, report],
  );

  const runSkill = useCallback(
    (entry: SlashCommand) => {
      if (!entry.skill) return;
      selectSkill(entry.skill);
      insertText(skillInvocation(entry.skill));
      report({
        tone: "info",
        text: `${entry.skill.name} selected for this chat and named in the prompt.`,
      });
    },
    [insertText, report, selectSkill],
  );

  /** Runs one registry entry. `send` comes from Shift on the menu. */
  const run = useCallback(
    (entry: SlashCommand, arg: string, send: boolean) => {
      if (entry.group === "playbook") return runPlaybook(entry, send);
      if (entry.group === "skill") return runSkill(entry);
      switch (entry.action) {
        case "test":
          return runTest(arg);
        case "deploy":
          return runDeploy();
        case "snapshot":
          return void runSnapshot(arg);
        case "preview":
          return void openPreview();
        case "screenshot":
          return void runScreenshot(arg);
        case "autopilot":
          return runAutopilot(arg);
        case "autotest":
          return runAutoTest(arg);
        case "review":
          return runReview();
        case "pr":
          return void runPullRequest(arg);
        case "browser":
          return runBrowser(arg);
        // "skills" is absent on purpose: it needs to know where in the draft
        // to leave its slash, which only the two call sites below know.
        case "help":
          return report({ tone: "info", text: "Slash commands", detail: slashHelpText(registry) });
        default:
          return;
      }
    },
    [
      openPreview, registry, report, runAutoTest, runAutopilot, runBrowser, runDeploy,
      runPlaybook, runPullRequest, runReview, runScreenshot, runSkill, runSnapshot, runTest,
    ],
  );

  /* ---------------------------------------------------------------- *
   * Menu interaction
   * ---------------------------------------------------------------- */

  const select = useCallback(
    (entry: SlashCommand, options: { send?: boolean } = {}) => {
      const current = slashTokenAt(text, caret);
      // A command that takes an argument is only typed out: the user still has
      // to say what to test or what to label, so running it now would guess.
      if (entry.takesArg && current) {
        const applied = applySlashCommand(text, current, entry);
        setText(applied.text);
        setGroupFilter(null);
        focusComposer(applied.caret);
        return;
      }
      const stripped = current ? text.slice(0, current.start) + text.slice(current.end) : text;
      if (entry.action === "skills") {
        openSkillMenu(stripped, current?.start ?? stripped.length);
        return;
      }
      if (current) {
        setText(stripped);
        focusComposer(current.start);
      }
      setGroupFilter(null);
      run(entry, "", options.send === true);
    },
    [caret, focusComposer, openSkillMenu, run, setText, text],
  );

  const onKeyDown = useCallback(
    (event: KeyboardEvent): boolean => {
      if (!open) return false;
      switch (event.key) {
        case "ArrowDown":
          event.preventDefault();
          setActiveIndex((index) => (index + 1) % items.length);
          return true;
        case "ArrowUp":
          event.preventDefault();
          setActiveIndex((index) => (index - 1 + items.length) % items.length);
          return true;
        case "Enter":
        case "Tab": {
          // Ctrl/Cmd+Enter is "send the draft" and must keep meaning that even
          // with the menu up; picking is the unmodified key.
          if (event.key === "Enter" && (event.ctrlKey || event.metaKey)) return false;
          if (event.isComposing) return false;
          const entry = items[activeIndex];
          if (!entry) return false;
          event.preventDefault();
          select(entry, { send: event.shiftKey });
          return true;
        }
        case "Escape":
          event.preventDefault();
          event.stopPropagation();
          close();
          return true;
        default:
          return false;
      }
    },
    [activeIndex, close, items, open, select],
  );

  const interceptSend = useCallback((): boolean => {
    const parsed = parseSlashInput(text);
    if (!parsed) return false;
    const entry = findSlashCommand(registry, parsed.command);
    if (!entry) {
      report({
        tone: "error",
        text: `Unknown command /${parsed.command}. Type / to see what is available, or // to send a literal slash.`,
      });
      return true;
    }
    setText("");
    setGroupFilter(null);
    if (entry.action === "skills") {
      openSkillMenu("", 0);
      return true;
    }
    run(entry, parsed.arg, false);
    return true;
  }, [openSkillMenu, registry, report, run, setText, text]);

  return {
    open,
    items,
    activeIndex,
    query: token?.query ?? "",
    setActiveIndex,
    status,
    screenshot,
    busy,
    onKeyDown,
    syncCaret,
    select,
    close,
    dismissStatus: () => setStatus(null),
    dismissScreenshot: () => setScreenshot(null),
    interceptSend,
    sendScreenshot: (card) => void sendScreenshot(card),
    insertScreenshot,
  };
}

/**
 * The commit question `/pr` asks when /workspace is dirty.
 *
 * A native confirm is deliberate here: the composer has no dialog of its own,
 * and the alternative — pushing whatever happens to be uncommitted without
 * asking — is exactly what the server refuses to do. Anyone who wants to edit
 * the message uses the dialog under Project settings → GitHub, which offers
 * the same default in an editable field.
 */
function confirmCommit(dirtyCount: number, message: string): boolean {
  const paths = `${dirtyCount} uncommitted change${dirtyCount === 1 ? "" : "s"}`;
  return confirm(
    `There ${dirtyCount === 1 ? "is" : "are"} ${paths} in /workspace.\n\n` +
      `Commit them as "${message}" and open the pull request?\n\n` +
      "To edit the message, use Project settings → GitHub → Open pull request instead.",
  );
}
