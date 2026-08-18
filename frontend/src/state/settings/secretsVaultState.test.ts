import assert from "node:assert/strict";
import test from "node:test";
import type { VaultSecret } from "../../models/secretsVault.ts";
import { secretsVaultState } from "./secretsVaultState.ts";

function entry(overrides: Partial<VaultSecret> = {}): VaultSecret {
  return {
    key: "GITHUB_TOKEN",
    kind: "env",
    scope: { all: true },
    updatedAt: 1700000000000,
    masked: "••••••••1234",
    hasValue: true,
    ...overrides,
  };
}

test("a blank draft creates an all-projects environment entry", () => {
  const draft = secretsVaultState.emptyDraft();
  assert.equal(draft.kind, "env");
  assert.equal(draft.scopeAll, true);
  assert.equal(draft.ssh.port, 22);
  assert.equal(draft.clear, false);
});

test("editing an entry starts with a blank value so a save keeps the stored one", () => {
  const draft = secretsVaultState.draftFrom(
    entry({
      kind: "ssh",
      scope: { all: false, projectIds: ["p1"] },
      ssh: { name: "hestia", host: "h.example.com", port: 2222, user: "admin" },
    }),
  );
  assert.equal(draft.value, "");
  assert.equal(draft.ssh.privateKey, "");
  assert.equal(draft.ssh.port, 2222);
  assert.equal(draft.scopeAll, false);
  assert.deepEqual(draft.projectIds, ["p1"]);
});

test("a key must be usable as an environment variable name", () => {
  const draft = { ...secretsVaultState.emptyDraft(), value: "x" };
  assert.equal(secretsVaultState.validate({ ...draft, key: "" }, { creating: true }), "Key is required.");
  assert.match(
    secretsVaultState.validate({ ...draft, key: "my-key" }, { creating: true }) ?? "",
    /A-Za-z_/,
  );
  assert.equal(secretsVaultState.validate({ ...draft, key: "GITHUB_TOKEN" }, { creating: true }), null);
});

test("an environment value may not span lines", () => {
  const draft = { ...secretsVaultState.emptyDraft(), key: "K", value: "a\nb" };
  assert.match(
    secretsVaultState.validate(draft, { creating: true }) ?? "",
    /cannot span lines/,
  );
});

test("a value is required on create and optional on edit", () => {
  const draft = { ...secretsVaultState.emptyDraft(), key: "K", value: "" };
  assert.equal(secretsVaultState.validate(draft, { creating: true }), "Value is required.");
  assert.equal(secretsVaultState.validate(draft, { creating: false }), null);
});

test("a scope must select something", () => {
  const draft = {
    ...secretsVaultState.emptyDraft(),
    key: "K",
    value: "x",
    scopeAll: false,
    projectIds: [],
  };
  assert.match(secretsVaultState.validate(draft, { creating: true }) ?? "", /at least one project/);
});

test("file paths are confined to the two container roots", () => {
  const cases: Array<[string, boolean]> = [
    ["/root/.npmrc", true],
    ["/root/.composer/auth.json", true],
    ["/workspace/.secrets/deploy.json", true],
    ["/etc/passwd", false],
    ["/root", false],
    ["/root/../etc/shadow", false],
    ["relative/path", false],
    ["", false],
  ];
  for (const [path, valid] of cases) {
    assert.equal(secretsVaultState.isValidFilePath(path), valid, path);
  }
});

test("an ssh target needs a usable name, host, user, and port", () => {
  const base = {
    ...secretsVaultState.emptyDraft(),
    key: "HESTIA",
    kind: "ssh" as const,
  };
  const ssh = {
    name: "hestia",
    host: "203.0.113.10",
    port: 22,
    user: "admin",
    privateKey: "KEY",
    knownHostsLine: "",
  };
  assert.equal(secretsVaultState.validate({ ...base, ssh }, { creating: true }), null);
  assert.match(
    secretsVaultState.validate({ ...base, ssh: { ...ssh, name: "-bad" } }, { creating: true }) ?? "",
    /Target name/,
  );
  assert.match(
    secretsVaultState.validate({ ...base, ssh: { ...ssh, host: " " } }, { creating: true }) ?? "",
    /Host is required/,
  );
  assert.match(
    secretsVaultState.validate({ ...base, ssh: { ...ssh, port: 70000 } }, { creating: true }) ?? "",
    /between 1 and 65535/,
  );
  assert.match(
    secretsVaultState.validate({ ...base, ssh: { ...ssh, privateKey: "" } }, { creating: true }) ?? "",
    /private key is required/,
  );
  assert.match(
    secretsVaultState.validate(
      { ...base, ssh: { ...ssh, knownHostsLine: "a\nb" } },
      { creating: true },
    ) ?? "",
    /single line/,
  );
});

test("the payload carries only the fields its kind uses", () => {
  const env = secretsVaultState.toPayload({
    ...secretsVaultState.emptyDraft(),
    key: " GITHUB_TOKEN ",
    value: "ghp_x",
    description: "  gh CLI  ",
  });
  assert.equal(env.key, "GITHUB_TOKEN");
  assert.equal(env.value, "ghp_x");
  assert.equal(env.description, "gh CLI");
  assert.deepEqual(env.scope, { all: true });
  assert.equal(env.path, undefined);
  assert.equal(env.ssh, undefined);

  const file = secretsVaultState.toPayload({
    ...secretsVaultState.emptyDraft(),
    key: "NPMRC",
    kind: "file",
    path: " /root/.npmrc ",
    value: "token",
    scopeAll: false,
    projectIds: ["p1", "p2"],
  });
  assert.equal(file.path, "/root/.npmrc");
  assert.deepEqual(file.scope, { all: false, projectIds: ["p1", "p2"] });

  const ssh = secretsVaultState.toPayload({
    ...secretsVaultState.emptyDraft(),
    key: "HESTIA",
    kind: "ssh",
    ssh: {
      name: " hestia ",
      host: " h.example.com ",
      port: 0,
      user: " admin ",
      privateKey: "KEY",
      knownHostsLine: " h ssh-ed25519 AAA ",
    },
  });
  assert.equal(ssh.ssh?.name, "hestia");
  assert.equal(ssh.ssh?.port, 22, "a missing port falls back to 22");
  assert.equal(ssh.ssh?.knownHostsLine, "h ssh-ed25519 AAA");
  assert.equal(ssh.value, undefined, "an ssh entry carries no value field");
});

test("clearing wipes the submitted value instead of keeping the stored one", () => {
  const cleared = secretsVaultState.toPayload({
    ...secretsVaultState.emptyDraft(),
    key: "GITHUB_TOKEN",
    value: "typed but discarded",
    clear: true,
  });
  assert.equal(cleared.clear, true);
  assert.equal(cleared.value, "");

  const clearedSsh = secretsVaultState.toPayload({
    ...secretsVaultState.emptyDraft(),
    key: "HESTIA",
    kind: "ssh",
    clear: true,
    ssh: { name: "hestia", host: "h", port: 22, user: "root", privateKey: "KEY" },
  });
  assert.equal(clearedSsh.ssh?.privateKey, "");
});

test("writes keep the table in key order", () => {
  const list = [entry({ key: "ZED" }), entry({ key: "ALPHA" })];
  const sorted = secretsVaultState.sort(list);
  assert.deepEqual(sorted.map((item) => item.key), ["ALPHA", "ZED"]);

  const upserted = secretsVaultState.upsert(sorted, entry({ key: "MID" }));
  assert.deepEqual(upserted.map((item) => item.key), ["ALPHA", "MID", "ZED"]);

  const replaced = secretsVaultState.upsert(upserted, entry({ key: "MID", masked: "••••••••9999" }));
  assert.equal(replaced.length, 3);
  assert.equal(replaced[1].masked, "••••••••9999");

  assert.deepEqual(
    secretsVaultState.remove(replaced, "MID").map((item) => item.key),
    ["ALPHA", "ZED"],
  );
});

test("describes scope and destination for the table", () => {
  assert.equal(secretsVaultState.scopeLabel({ all: true }), "All projects");
  assert.equal(secretsVaultState.scopeLabel({ all: false, projectIds: ["p1"] }), "1 project");
  assert.equal(secretsVaultState.scopeLabel({ all: false, projectIds: ["p1", "p2"] }), "2 projects");
  assert.equal(secretsVaultState.scopeLabel({ all: false, projectIds: [] }), "No projects");
  assert.equal(secretsVaultState.scopeLabel(undefined), "All projects");

  assert.equal(secretsVaultState.destinationLabel(entry()), "$GITHUB_TOKEN");
  assert.equal(
    secretsVaultState.destinationLabel(entry({ kind: "file", path: "/root/.npmrc" })),
    "/root/.npmrc",
  );
  assert.equal(
    secretsVaultState.destinationLabel(
      entry({ kind: "ssh", ssh: { name: "hestia", host: "h", port: 22, user: "root" } }),
    ),
    "ssh hestia",
  );
});

test("publishes the documented SSH_TARGET_* contract", () => {
  assert.deepEqual(secretsVaultState.sshEnvVars("hestia-prod.eu"), [
    "SSH_TARGET_HESTIA_PROD_EU_HOST",
    "SSH_TARGET_HESTIA_PROD_EU_USER",
    "SSH_TARGET_HESTIA_PROD_EU_PORT",
  ]);
});

test("lists the inherited keys a project overrides", () => {
  assert.deepEqual(
    secretsVaultState.shadowedKeys([
      { key: "GITHUB_TOKEN", kind: "env", source: "global", shadowed: true },
      { key: "NPMRC", kind: "file", source: "global", shadowed: false },
    ]),
    ["GITHUB_TOKEN"],
  );
});
