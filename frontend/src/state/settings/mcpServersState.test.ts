import assert from "node:assert/strict";
import test from "node:test";
import type { MCPProjectEntry, MCPServer } from "../../models/mcp.ts";
import { MCP_TEMPLATES, mcpServersState } from "./mcpServersState.ts";

function server(overrides: Partial<MCPServer> = {}): MCPServer {
  return {
    name: "fetch",
    transport: "stdio",
    command: "uvx",
    args: ["mcp-server-fetch"],
    scope: { all: true },
    updatedAt: 1700000000000,
    ...overrides,
  };
}

function projectEntry(overrides: Partial<MCPProjectEntry> = {}): MCPProjectEntry {
  return { ...server(), source: "platform", enabled: true, ...overrides };
}

test("a blank draft targets every supported agent and every project", () => {
  const draft = mcpServersState.emptyDraft();
  assert.equal(draft.transport, "stdio");
  assert.equal(draft.scopeAll, true);
  assert.deepEqual(draft.providers, ["claude", "codex"]);
  assert.deepEqual(draft.secretRefs, []);
});

test("editing an entry round-trips its args, env, and scope", () => {
  const draft = mcpServersState.draftFrom(
    server({
      args: ["-y", "@modelcontextprotocol/server-postgres"],
      env: { PGUSER: "app", PGPASSWORD: "${PG_PASSWORD}" },
      secretRefs: ["PG_PASSWORD"],
      scope: { all: false, projectIds: ["p1", "p2"] },
      enabledForProviders: ["codex"],
    }),
  );
  assert.equal(draft.argsText, "-y\n@modelcontextprotocol/server-postgres");
  assert.equal(draft.envText, "PGPASSWORD=${PG_PASSWORD}\nPGUSER=app");
  assert.equal(draft.scopeAll, false);
  assert.deepEqual(draft.projectIds, ["p1", "p2"]);
  assert.deepEqual(draft.providers, ["codex"]);
  assert.deepEqual(draft.secretRefs, ["PG_PASSWORD"]);
});

test("an entry with no explicit providers edits as all supported ones", () => {
  const draft = mcpServersState.draftFrom(server({ enabledForProviders: [] }));
  assert.deepEqual(draft.providers, ["claude", "codex"]);
});

test("the payload carries only the fields the chosen transport uses", () => {
  const stdio = mcpServersState.toPayload(
    {
      ...mcpServersState.emptyDraft(),
      name: "postgres",
      command: " npx ",
      argsText: "-y\n\n@modelcontextprotocol/server-postgres",
      envText: "PGPASSWORD=${PG_PASSWORD}",
      url: "https://left-over.example.com",
      headersText: "Authorization: Bearer left-over",
      secretRefs: ["PG_PASSWORD"],
    },
    { platform: true },
  );
  assert.equal(stdio.command, "npx");
  assert.deepEqual(stdio.args, ["-y", "@modelcontextprotocol/server-postgres"]);
  assert.deepEqual(stdio.env, { PGPASSWORD: "${PG_PASSWORD}" });
  assert.equal(stdio.url, undefined);
  assert.equal(stdio.headers, undefined);

  const http = mcpServersState.toPayload(
    {
      ...mcpServersState.emptyDraft(),
      name: "jira",
      transport: "http",
      url: " https://jira.example.com/mcp ",
      headersText: "Authorization: Bearer ${JIRA_TOKEN}",
      command: "left-over",
      secretRefs: ["JIRA_TOKEN"],
    },
    { platform: true },
  );
  assert.equal(http.url, "https://jira.example.com/mcp");
  assert.deepEqual(http.headers, { Authorization: "Bearer ${JIRA_TOKEN}" });
  assert.equal(http.command, undefined);
  assert.equal(http.args, undefined);
});

test("a project-only entry is sent without a scope of its own", () => {
  const payload = mcpServersState.toPayload(
    { ...mcpServersState.emptyDraft(), name: "shop", command: "npx", scopeAll: true },
    { platform: false },
  );
  assert.deepEqual(payload.scope, { all: false });
});

test("validation mirrors the backend's rules", () => {
  const base = mcpServersState.emptyDraft();
  const cases: Array<[string, Partial<typeof base>, RegExp]> = [
    ["a missing name", { command: "npx" }, /Name is required/],
    ["a name with a space", { name: "my server", command: "npx" }, /Name must start/],
    ["a stdio entry with no command", { name: "x" }, /needs a command/],
    [
      "an http entry with no URL",
      { name: "x", transport: "http" as const },
      /absolute http/,
    ],
    [
      "an http entry with a file URL",
      { name: "x", transport: "http" as const, url: "file:///etc/passwd" },
      /absolute http/,
    ],
    [
      "an empty explicit scope",
      { name: "x", command: "npx", scopeAll: false, projectIds: [] },
      /at least one project/,
    ],
    ["no providers", { name: "x", command: "npx", providers: [] }, /at least one agent/],
    [
      "an environment name that is not a POSIX name",
      { name: "x", command: "npx", envText: "not-a-name=v" },
      /not a valid environment variable name/,
    ],
    [
      "a placeholder nobody declared",
      { name: "x", command: "npx", envText: "TOKEN=${UNDECLARED}" },
      /has no matching vault key/,
    ],
  ];
  for (const [label, overrides, pattern] of cases) {
    const problem = mcpServersState.validate({ ...base, ...overrides }, { platform: true });
    assert.match(problem ?? "", pattern, label);
  }

  assert.equal(
    mcpServersState.validate(
      { ...base, name: "fetch", command: "uvx", argsText: "mcp-server-fetch" },
      { platform: true },
    ),
    null,
  );
});

test("a project entry does not need a scope to be valid", () => {
  const problem = mcpServersState.validate(
    { ...mcpServersState.emptyDraft(), name: "shop", command: "npx", scopeAll: false, projectIds: [] },
    { platform: false },
  );
  assert.equal(problem, null);
});

test("placeholders are found everywhere a value may appear", () => {
  const draft = {
    ...mcpServersState.emptyDraft(),
    transport: "http" as const,
    url: "https://e.example.com/${SITE}",
    argsText: "--token=${TOKEN}",
    envText: "A=${TOKEN}",
    headersText: "X-Key: ${HEADER_KEY}",
  };
  assert.deepEqual(mcpServersState.placeholders(draft), ["HEADER_KEY", "SITE", "TOKEN"]);
});

test("upsert keeps the table in name order and remove takes one out", () => {
  const list = [server({ name: "zeta" }), server({ name: "alpha" })];
  const sorted = mcpServersState.sort(list);
  assert.deepEqual(
    sorted.map((entry) => entry.name),
    ["alpha", "zeta"],
  );
  const withNew = mcpServersState.upsert(sorted, server({ name: "mid" }));
  assert.deepEqual(
    withNew.map((entry) => entry.name),
    ["alpha", "mid", "zeta"],
  );
  assert.deepEqual(
    mcpServersState.remove(withNew, "mid").map((entry) => entry.name),
    ["alpha", "zeta"],
  );
});

test("the table's summary columns describe what an entry does", () => {
  assert.equal(mcpServersState.scopeLabel({ all: true }), "All projects");
  assert.equal(mcpServersState.scopeLabel({ all: false, projectIds: ["p1"] }), "1 project");
  assert.equal(mcpServersState.scopeLabel({ all: false, projectIds: [] }), "No projects");
  assert.equal(mcpServersState.destinationLabel(server()), "uvx mcp-server-fetch");
  assert.equal(
    mcpServersState.destinationLabel(
      server({ transport: "http", url: "https://jira.example.com/mcp" }),
    ),
    "https://jira.example.com/mcp",
  );
  assert.deepEqual(mcpServersState.providerLabels(server()), ["Claude Code", "Codex"]);
  assert.deepEqual(
    mcpServersState.providerLabels(server({ enabledForProviders: ["codex"] })),
    ["Codex"],
  );
});

test("toggling one project entry leaves the others' state alone", () => {
  const entries = [
    projectEntry({ name: "a", enabled: true }),
    projectEntry({ name: "b", enabled: false }),
    projectEntry({ name: "c", enabled: true }),
  ];
  assert.deepEqual(mcpServersState.disabledNames(entries), ["b"]);
  assert.deepEqual(mcpServersState.toggle(entries, "a"), ["a", "b"]);
  assert.deepEqual(mcpServersState.toggle(entries, "b"), []);
});

test("only project-owned entries are editable by a member", () => {
  const entries = [
    projectEntry({ name: "inherited", source: "platform" }),
    projectEntry({ name: "mine", source: "project" }),
  ];
  assert.deepEqual(
    mcpServersState.projectOwned(entries).map((entry) => entry.name),
    ["mine"],
  );
});

test("the materialized line reads as a relative time, or says never", () => {
  const now = 1_700_000_000_000;
  assert.match(mcpServersState.materializedLabel(undefined, now), /Not yet written/);
  assert.match(mcpServersState.materializedLabel(now - 10_000, now), /just now/);
  assert.match(mcpServersState.materializedLabel(now - 5 * 60_000, now), /5 minutes ago/);
  assert.match(mcpServersState.materializedLabel(now - 3 * 3_600_000, now), /3 hours ago/);
  assert.match(mcpServersState.materializedLabel(now - 2 * 86_400_000, now), /2 days ago/);
});

test("every seed template produces a draft that only needs a name and a scope", () => {
  for (const template of MCP_TEMPLATES) {
    const draft = mcpServersState.draftFromTemplate(template);
    const problem = mcpServersState.validate(
      { ...draft, name: draft.name || "example" },
      { platform: true },
    );
    assert.equal(problem, null, `${template.id}: ${problem}`);
  }
});
