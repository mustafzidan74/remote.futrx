import assert from "node:assert/strict";
import test from "node:test";
import type {
  GitHubDelivery,
  GitHubPullRequest,
  GitHubSettings,
  GitHubStatus,
} from "../../models/github.ts";
import {
  checksTone,
  defaultBase,
  deliveryTone,
  describeAutomation,
  describeBranchState,
  describeChecks,
  describeDelivery,
  describeGitHub,
  githubActions,
  needsNewBranch,
  repoFullName,
  repoUrl,
  showsUntrustedInputWarning,
  suggestBranchName,
} from "./githubPanelState.ts";

function status(overrides: Partial<GitHubStatus> = {}): GitHubStatus {
  return {
    linked: true,
    owner: "futrx-com",
    repo: "remote.futrx",
    defaultBranch: "main",
    containerRunning: true,
    authOk: true,
    workspaceRepo: true,
    workspaceEmpty: false,
    branch: "feat/x",
    upstream: "origin/feat/x",
    ahead: 0,
    behind: 0,
    dirty: false,
    dirtyCount: 0,
    ...overrides,
  };
}

function settings(overrides: Partial<GitHubSettings> = {}): GitHubSettings {
  return {
    webhookConfigured: true,
    label: "remote-agent",
    autoRun: false,
    commentBack: false,
    deliveries: [],
    ...overrides,
  };
}

function pull(overrides: Partial<GitHubPullRequest> = {}): GitHubPullRequest {
  return {
    number: 12,
    title: "Add a flag",
    url: "https://github.com/o/r/pull/12",
    checksPassed: 0,
    checksTotal: 0,
    ...overrides,
  };
}

function delivery(overrides: Partial<GitHubDelivery> = {}): GitHubDelivery {
  return { id: "d1", at: 0, event: "issues", outcome: "ignored", ...overrides };
}

/* ---------------------------------------------------------------- *
 * Identity
 * ---------------------------------------------------------------- */

test("the repository name and link come from the stored owner and repo", () => {
  assert.equal(repoFullName(status()), "futrx-com/remote.futrx");
  assert.equal(repoUrl(status()), "https://github.com/futrx-com/remote.futrx");
});

test("an unlinked project has no name and no link", () => {
  assert.equal(repoFullName(undefined), "");
  assert.equal(repoUrl(undefined), "");
  assert.equal(repoFullName({ owner: "o", repo: undefined }), "");
});

/* ---------------------------------------------------------------- *
 * Summaries
 * ---------------------------------------------------------------- */

test("the panel summary names the blocking condition, not just the link", () => {
  assert.match(describeGitHub(undefined, true), /Checking/);
  assert.match(describeGitHub(undefined, false), /could not be read/);
  assert.match(describeGitHub(status({ linked: false }), false), /Not linked/);
  assert.match(
    describeGitHub(status({ containerRunning: false }), false),
    /container is stopped/,
  );
  assert.match(describeGitHub(status({ authOk: false }), false), /No usable GitHub credential/);
  assert.match(
    describeGitHub(status({ workspaceRepo: false, workspaceEmpty: true }), false),
    /empty and can be cloned/,
  );
  assert.match(
    describeGitHub(status({ workspaceRepo: false, workspaceEmpty: false }), false),
    /not a git repository/,
  );
  assert.match(describeGitHub(status(), false), /futrx-com\/remote\.futrx/);
});

test("the branch sentence reports every divergence it knows about", () => {
  assert.equal(describeBranchState(status()), "on feat/x, in sync.");
  assert.equal(describeBranchState(status({ ahead: 2 })), "on feat/x, 2 ahead.");
  assert.equal(
    describeBranchState(status({ ahead: 2, behind: 1, dirtyCount: 3, dirty: true })),
    "on feat/x, 2 ahead, 1 behind, 3 uncommitted changes.",
  );
  assert.equal(
    describeBranchState(status({ dirtyCount: 1, dirty: true })),
    "on feat/x, 1 uncommitted change.",
  );
  assert.equal(
    describeBranchState(status({ upstream: "" })),
    "on feat/x, no upstream.",
  );
  assert.equal(
    describeBranchState(status({ branch: "" })),
    "no branch checked out, in sync.",
  );
});

/* ---------------------------------------------------------------- *
 * What the buttons may do
 * ---------------------------------------------------------------- */

test("actions are gated in order: link, container, credential, repository", () => {
  const cases: Array<{ name: string; value: GitHubStatus | undefined; reason: RegExp }> = [
    { name: "nothing loaded", value: undefined, reason: /Link a repository/ },
    { name: "not linked", value: status({ linked: false }), reason: /Link a repository/ },
    { name: "container down", value: status({ containerRunning: false }), reason: /Start this project/ },
    { name: "no credential", value: status({ authOk: false }), reason: /GITHUB_TOKEN/ },
  ];
  for (const { name, value, reason } of cases) {
    const actions = githubActions(value);
    assert.equal(actions.canOpenPR, false, name);
    assert.equal(actions.canClone, false, name);
    assert.match(actions.blockedReason, reason, name);
  }
});

test("an empty workspace offers a clone and nothing else", () => {
  const actions = githubActions(status({ workspaceRepo: false, workspaceEmpty: true }));
  assert.equal(actions.canClone, true);
  assert.equal(actions.canOpenPR, false);
  assert.match(actions.blockedReason, /Clone the repository/);
});

test("a non-empty workspace that is not a repository offers nothing", () => {
  const actions = githubActions(status({ workspaceRepo: false, workspaceEmpty: false }));
  assert.equal(actions.canClone, false, "cloning over existing files is never offered");
  assert.equal(actions.canOpenPR, false);
  assert.match(actions.blockedReason, /not a git repository/);
});

test("a healthy linked project can open and list pull requests", () => {
  const actions = githubActions(status());
  assert.equal(actions.canOpenPR, true);
  assert.equal(actions.canListPRs, true);
  assert.equal(actions.canClone, false);
  assert.equal(actions.blockedReason, "");
});

/* ---------------------------------------------------------------- *
 * Branch suggestions
 * ---------------------------------------------------------------- */

test("the default base is the repository's default branch", () => {
  assert.equal(defaultBase(status()), "main");
  assert.equal(defaultBase(status({ defaultBranch: undefined })), "");
  assert.equal(defaultBase(undefined), "");
});

test("work sitting on the default branch needs a new branch", () => {
  assert.equal(needsNewBranch(status({ branch: "main" })), true);
  assert.equal(needsNewBranch(status({ branch: "feat/x" })), false);
  assert.equal(needsNewBranch(status({ branch: "main", defaultBranch: undefined })), false);
  assert.equal(needsNewBranch(undefined), false);
});

test("a suggested branch name is always one the server would accept", () => {
  const now = new Date("2026-08-18T00:00:00Z");
  assert.equal(suggestBranchName("Add a login flag", now), "remote/add-a-login-flag");
  assert.equal(suggestBranchName("Fix: the CI!!! ", now), "remote/fix-the-ci");
  assert.equal(suggestBranchName("", now), "remote/changes-2026-08-18");
  assert.equal(suggestBranchName("!!!", now), "remote/changes-2026-08-18");
  // No leading dash, no double slash, no trailing dash — the three shapes the
  // server rejects.
  const long = suggestBranchName("x".repeat(200), now);
  assert.ok(long.length <= 48, long);
  assert.ok(!long.endsWith("-"), long);
  assert.ok(!long.includes("//"), long);
});

/* ---------------------------------------------------------------- *
 * Checks and deliveries
 * ---------------------------------------------------------------- */

test("the checks column counts runs and colours the verdict", () => {
  assert.equal(describeChecks(pull()), "no checks");
  assert.equal(checksTone(pull()), "muted");

  assert.equal(
    describeChecks(pull({ checks: "passing", checksPassed: 5, checksTotal: 5 })),
    "5/5 checks",
  );
  assert.equal(checksTone(pull({ checks: "passing" })), "ok");
  assert.equal(checksTone(pull({ checks: "pending" })), "warn");
  assert.equal(checksTone(pull({ checks: "failing" })), "bad");
});

test("every delivery outcome explains itself", () => {
  assert.match(describeDelivery(delivery({ outcome: "ran", number: 7 })), /#7 started a run/);
  assert.match(
    describeDelivery(delivery({ outcome: "chat-only", number: 7, reason: "autoRun is off" })),
    /created a chat.*autoRun is off/,
  );
  assert.match(
    describeDelivery(delivery({ outcome: "ignored", reason: "not labelled" })),
    /ignored: not labelled/,
  );
  assert.match(
    describeDelivery(delivery({ outcome: "rejected", reason: "signature" })),
    /rejected: signature/,
  );
  assert.match(describeDelivery(delivery({ outcome: "failed" })), /failed: unknown error/);
  // A delivery with no reason still reads as a sentence.
  assert.match(describeDelivery(delivery({ outcome: "ignored" })), /no rule matched/);
});

test("delivery tone separates a run from a rejection", () => {
  assert.equal(deliveryTone(delivery({ outcome: "ran" })), "ok");
  assert.equal(deliveryTone(delivery({ outcome: "chat-only" })), "warn");
  assert.equal(deliveryTone(delivery({ outcome: "rejected" })), "bad");
  assert.equal(deliveryTone(delivery({ outcome: "failed" })), "bad");
  assert.equal(deliveryTone(delivery({ outcome: "ignored" })), "muted");
});

/* ---------------------------------------------------------------- *
 * Automation wording
 * ---------------------------------------------------------------- */

test("the automation summary states plainly whether anything can start a run", () => {
  assert.match(describeAutomation(undefined, true), /Loading/);
  assert.match(describeAutomation(undefined, false), /could not be loaded/);
  assert.match(
    describeAutomation(settings({ webhookConfigured: false }), false),
    /No webhook is configured/,
  );
  const off = describeAutomation(settings(), false);
  assert.match(off, /Automatic runs are OFF/);
  assert.match(off, /remote-agent/);
  const on = describeAutomation(settings({ autoRun: true, label: "agent" }), false);
  assert.match(on, /Automatic runs are ON/);
  assert.match(on, /agent/);
});

test("the untrusted-input warning stays up for as long as automation is armed", () => {
  assert.equal(showsUntrustedInputWarning(settings({ autoRun: true })), true);
  assert.equal(showsUntrustedInputWarning(settings({ autoRun: false })), false);
  assert.equal(showsUntrustedInputWarning(undefined), false);
});
