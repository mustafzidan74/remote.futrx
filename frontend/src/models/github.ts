/**
 * The GitHub integration's wire types.
 *
 * These mirror `backend/internal/service/github`'s JSON exactly. Two things
 * are deliberately absent and must stay absent: the webhook's shared secret
 * (which the server returns exactly once, on the response that mints it, and
 * never again) and any GitHub token.
 */

/** The repository a project is bound to, as stored on the project's metadata. */
export interface GitHubLink {
  owner: string;
  repo: string;
  defaultBranch?: string;
  linkedAt: number;
  linkedBy?: string;
}

/** The panel's live view: the stored link plus what the container reports. */
export interface GitHubStatus {
  linked: boolean;
  owner?: string;
  repo?: string;
  defaultBranch?: string;
  linkedAt?: number;
  linkedBy?: string;
  /** False when the container is stopped; every field below is then unknown. */
  containerRunning: boolean;
  /** Whether `gh auth status` succeeded inside the container. */
  authOk: boolean;
  authError?: string;
  /** False when /workspace holds no git repository. */
  workspaceRepo: boolean;
  /** True only when /workspace is completely empty — the one state a clone is offered in. */
  workspaceEmpty: boolean;
  branch?: string;
  upstream?: string;
  ahead: number;
  behind: number;
  dirty: boolean;
  dirtyCount: number;
  /** The deterministic message the commit dialog pre-fills. */
  defaultCommitMessage?: string;
}

/** One recorded inbound webhook delivery. */
export interface GitHubDelivery {
  id: string;
  at: number;
  event: string;
  /** Which rule matched: "label", "command", or "review". */
  action?: string;
  number?: number;
  title?: string;
  url?: string;
  sender?: string;
  outcome: GitHubDeliveryOutcome;
  reason?: string;
  chatId?: string;
  runStarted?: boolean;
}

export type GitHubDeliveryOutcome =
  | "ran"
  | "chat-only"
  | "ignored"
  | "rejected"
  | "failed";

/** The automation settings, as the panel reads them back. */
export interface GitHubSettings {
  webhookConfigured: boolean;
  webhookUrl?: string;
  label: string;
  autoRun: boolean;
  commentBack: boolean;
  enabledAt?: number;
  enabledBy?: string;
  updatedAt?: number;
  /** Present only on the response that generated it. Never stored. */
  secret?: string;
  deliveries: GitHubDelivery[];
}

export interface UpdateGitHubSettingsInput {
  label?: string;
  autoRun?: boolean;
  commentBack?: boolean;
  rotate?: boolean;
  disable?: boolean;
}

/** Roll-up of a pull request's checks. Empty when it has none. */
export type GitHubChecks = "" | "passing" | "failing" | "pending";

export interface GitHubPullRequest {
  number: number;
  title: string;
  url: string;
  headBranch?: string;
  baseBranch?: string;
  author?: string;
  draft?: boolean;
  updatedAt?: string;
  checks?: GitHubChecks;
  checksPassed: number;
  checksTotal: number;
}

export interface CreateGitHubPRInput {
  title?: string;
  body?: string;
  base?: string;
  head?: string;
  /** Explicit confirmation that uncommitted changes may be committed. */
  commit?: boolean;
  commitMessage?: string;
}

export interface CreateGitHubPRResult {
  url: string;
  branch: string;
  committed: boolean;
}

/**
 * The 409 body a pull request request gets when /workspace is dirty and the
 * caller did not confirm a commit. It carries the default message so the
 * dialog never has to compose one.
 */
export interface GitHubDirtyWorkspace {
  error: string;
  dirty: true;
  defaultCommitMessage: string;
  dirtyCount: number;
}

export interface ImportGitHubCommentsResult {
  chatId: string;
  comments: number;
  /** False when the chat already had a run in flight; the prompt is returned anyway. */
  started: boolean;
  prompt: string;
}

/**
 * Thrown by `createPullRequest` when /workspace has uncommitted changes and
 * the request did not confirm a commit.
 *
 * It is a distinct error class rather than a status-code check at the call
 * site because the dialog it opens needs the two fields the server put in the
 * body: the deterministic default message, and how many paths would be swept
 * into the commit.
 */
export class GitHubDirtyWorkspaceError extends Error {
  defaultCommitMessage: string;
  dirtyCount: number;

  constructor(message: string, defaultCommitMessage: string, dirtyCount: number) {
    super(message);
    this.name = "GitHubDirtyWorkspaceError";
    this.defaultCommitMessage = defaultCommitMessage;
    this.dirtyCount = dirtyCount;
  }
}

/**
 * A proposed commit subject. `message` is always usable: it is the generated
 * subject when a model wrote one, and the deterministic dated default
 * otherwise, with `reason` saying which.
 */
export interface CommitMessageSuggestion {
  message: string;
  generated: boolean;
  fallback: string;
  reason?: string;
}
