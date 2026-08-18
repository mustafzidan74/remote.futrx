import type {
  GitHubDelivery,
  GitHubPullRequest,
  GitHubSettings,
  GitHubStatus,
} from "../../models/github";

/**
 * Everything the GitHub panel needs to *decide* or *say*, with no side effects
 * and no Preact.
 *
 * The panel's whole difficulty is that it renders one of about eight states —
 * not linked, container down, no credential, empty workspace, clean, ahead,
 * dirty, diverged — and each state enables a different set of buttons. Working
 * that out in JSX produces a thicket of `&&`s that nobody can test; working it
 * out here produces one function per question and a table of cases.
 */

/** The one-line summary under the panel's heading. */
export function describeGitHub(
  status: GitHubStatus | undefined,
  loading: boolean,
): string {
  if (loading && !status) return "Checking this project's GitHub link…";
  if (!status) return "This project's GitHub link could not be read.";
  if (!status.linked) return "Not linked to a repository yet.";
  const name = repoFullName(status);
  if (!status.containerRunning) return `Linked to ${name}. The container is stopped.`;
  if (!status.authOk) return `Linked to ${name}. No usable GitHub credential in the container.`;
  if (!status.workspaceRepo) {
    return status.workspaceEmpty
      ? `Linked to ${name}. /workspace is empty and can be cloned into.`
      : `Linked to ${name}. /workspace has files but is not a git repository.`;
  }
  return `Linked to ${name} — ${describeBranchState(status)}`;
}

/** owner/repo, or an empty string when nothing is linked. */
export function repoFullName(
  status: Pick<GitHubStatus, "owner" | "repo"> | undefined,
): string {
  if (!status?.owner || !status?.repo) return "";
  return `${status.owner}/${status.repo}`;
}

/** The repository's page on github.com. */
export function repoUrl(status: Pick<GitHubStatus, "owner" | "repo"> | undefined): string {
  const name = repoFullName(status);
  return name ? `https://github.com/${name}` : "";
}

/**
 * "on main, 2 ahead, 1 behind, 3 uncommitted changes" — the sentence a person
 * would say when asked where the working tree stands.
 */
export function describeBranchState(status: GitHubStatus): string {
  const parts: string[] = [];
  parts.push(status.branch ? `on ${status.branch}` : "no branch checked out");
  if (!status.upstream) parts.push("no upstream");
  if (status.ahead > 0) parts.push(`${status.ahead} ahead`);
  if (status.behind > 0) parts.push(`${status.behind} behind`);
  if (status.dirtyCount > 0) {
    parts.push(`${status.dirtyCount} uncommitted change${status.dirtyCount === 1 ? "" : "s"}`);
  }
  if (parts.length === 1 && status.upstream) parts.push("in sync");
  return parts.join(", ") + ".";
}

/** What the actions in the panel are allowed to do right now, and why not. */
export interface GitHubActionAvailability {
  canClone: boolean;
  canOpenPR: boolean;
  canListPRs: boolean;
  /** Why the pull request button is disabled, for its title attribute. */
  blockedReason: string;
}

export function githubActions(status: GitHubStatus | undefined): GitHubActionAvailability {
  const blocked = (reason: string): GitHubActionAvailability => ({
    canClone: false,
    canOpenPR: false,
    canListPRs: false,
    blockedReason: reason,
  });
  if (!status || !status.linked) return blocked("Link a repository first.");
  if (!status.containerRunning) return blocked("Start this project's container first.");
  if (!status.authOk) {
    return blocked(
      "This container has no usable GitHub credential. Add a GITHUB_TOKEN secret to " +
        "the project or to the platform vault.",
    );
  }
  if (!status.workspaceRepo) {
    return {
      // Cloning is the one action an empty workspace enables, and the only
      // one it enables: everything else needs a repository to act on.
      canClone: status.workspaceEmpty,
      canOpenPR: false,
      canListPRs: true,
      blockedReason: status.workspaceEmpty
        ? "Clone the repository into /workspace first."
        : "/workspace has files but is not a git repository, so nothing can be pushed.",
    };
  }
  return { canClone: false, canOpenPR: true, canListPRs: true, blockedReason: "" };
}

/** The branch a new pull request defaults to targeting. */
export function defaultBase(status: GitHubStatus | undefined): string {
  return status?.defaultBranch ?? "";
}

/**
 * True when opening a pull request from the current branch would be refused
 * because it *is* the base. The panel uses it to pre-fill a new branch name
 * rather than letting the operator discover the problem from a 409.
 */
export function needsNewBranch(status: GitHubStatus | undefined): boolean {
  if (!status?.branch) return false;
  const base = defaultBase(status);
  return base !== "" && status.branch === base;
}

/**
 * A branch name suggested for work currently sitting on the default branch.
 *
 * It is derived from the title so the branch and the pull request read as the
 * same thing, and it is sanitized to the same grammar the server accepts —
 * a name the server would reject is worse than no suggestion at all.
 */
export function suggestBranchName(title: string, now: Date): string {
  const slug = title
    .toLowerCase()
    .replace(/[^a-z0-9]+/g, "-")
    .replace(/^-+|-+$/g, "")
    .slice(0, 40)
    .replace(/-+$/g, "");
  const stamp = now.toISOString().slice(0, 10);
  return slug ? `remote/${slug}` : `remote/changes-${stamp}`;
}

/** The check column: "3/5 checks" or "no checks". */
export function describeChecks(pull: GitHubPullRequest): string {
  if (!pull.checksTotal) return "no checks";
  return `${pull.checksPassed}/${pull.checksTotal} checks`;
}

/** Tailwind-agnostic tone for the check badge. */
export type ChecksTone = "ok" | "warn" | "bad" | "muted";

export function checksTone(pull: GitHubPullRequest): ChecksTone {
  switch (pull.checks) {
    case "passing":
      return "ok";
    case "pending":
      return "warn";
    case "failing":
      return "bad";
    default:
      return "muted";
  }
}

/** Human wording for one recorded delivery's outcome. */
export function describeDelivery(delivery: GitHubDelivery): string {
  const subject = delivery.number ? `#${delivery.number}` : delivery.event;
  switch (delivery.outcome) {
    case "ran":
      return `${subject} started a run.`;
    case "chat-only":
      return `${subject} created a chat. ${delivery.reason || "Nothing was run."}`;
    case "ignored":
      return `${subject} ignored: ${delivery.reason || "no rule matched."}`;
    case "rejected":
      return `${subject} rejected: ${delivery.reason || "verification failed."}`;
    case "failed":
      return `${subject} failed: ${delivery.reason || "unknown error."}`;
    default:
      return `${subject}: ${delivery.outcome}`;
  }
}

export function deliveryTone(delivery: GitHubDelivery): ChecksTone {
  switch (delivery.outcome) {
    case "ran":
      return "ok";
    case "chat-only":
      return "warn";
    case "rejected":
    case "failed":
      return "bad";
    default:
      return "muted";
  }
}

/** The one-line summary of the automation half. */
export function describeAutomation(
  settings: GitHubSettings | undefined,
  loading: boolean,
): string {
  if (loading && !settings) return "Loading webhook settings…";
  if (!settings) return "Webhook settings could not be loaded.";
  if (!settings.webhookConfigured) {
    return "No webhook is configured. Generate a secret to accept GitHub events.";
  }
  const label = settings.label;
  if (!settings.autoRun) {
    return `Webhook active. Automatic runs are OFF: a matching event creates a chat and notifies, ` +
      `but starts nothing. Trigger label: ${label}.`;
  }
  return `Webhook active. Automatic runs are ON for issues labelled ${label}, ` +
    `/remote comments from collaborators, and reviews requesting changes.`;
}

/**
 * Whether the panel should warn loudly. Automatic runs mean text written by
 * anyone who can label an issue reaches an agent running as root, so the
 * warning is shown whenever that is armed — not only while it is being armed.
 */
export function showsUntrustedInputWarning(settings: GitHubSettings | undefined): boolean {
  return settings?.autoRun === true;
}
