import type {
  MCPProjectEntry,
  MCPScope,
  MCPServer,
  MCPServerDraftPayload,
  MCPTransport,
} from "../../models/mcp";

/**
 * The editable form behind the Add/Edit dialog.
 *
 * It is deliberately flat and always carries both transports' fields, so
 * flipping the transport switch keeps what the operator already typed instead
 * of throwing it away. Args, env, and headers are edited as text — one entry
 * per line — because that is how they are read from a README and pasted.
 */
export interface MCPDraft {
  name: string;
  transport: MCPTransport;
  /** One argument per line. */
  command: string;
  argsText: string;
  /** `NAME=value` per line. */
  envText: string;
  url: string;
  /** `Header-Name: value` per line. */
  headersText: string;
  description: string;
  scopeAll: boolean;
  projectIds: string[];
  providers: string[];
  secretRefs: string[];
}

const NAME_PATTERN = /^[A-Za-z0-9][A-Za-z0-9_-]*$/;
const ENV_NAME_PATTERN = /^[A-Za-z_][A-Za-z0-9_]*$/;
const HEADER_NAME_PATTERN = /^[A-Za-z0-9-]+$/;
const PLACEHOLDER_PATTERN = /\$\{([A-Za-z_][A-Za-z0-9_]*)\}/g;

/** The agent CLIs this platform can configure, mirroring the backend. */
export const MCP_PROVIDERS = ["claude", "codex"] as const;

export const MCP_PROVIDER_LABELS: Record<string, string> = {
  claude: "Claude Code",
  codex: "Codex",
  kimi: "Kimi Code",
  antigravity: "Antigravity",
};

/**
 * A handful of starting points, offered as one-click prefills in the Add
 * dialog. They are examples, not a curated catalogue: nothing is enabled
 * until an admin saves it, and every one of them still needs a scope.
 */
export interface MCPTemplate {
  id: string;
  label: string;
  hint: string;
  draft: Partial<MCPDraft>;
}

export const MCP_TEMPLATES: MCPTemplate[] = [
  {
    id: "playwright",
    label: "Playwright",
    hint: "Drives a browser the agent controls itself. Separate from the Agent Browser.",
    draft: {
      name: "playwright",
      transport: "stdio",
      command: "npx",
      argsText: "@playwright/mcp@latest",
      description: "Browser automation through Playwright.",
    },
  },
  {
    id: "postgres",
    label: "Postgres",
    hint: "Read-only SQL over a connection string. Put the password in the vault.",
    draft: {
      name: "postgres",
      transport: "stdio",
      command: "npx",
      argsText: "-y\n@modelcontextprotocol/server-postgres\npostgresql://app:${PG_PASSWORD}@db.example.com:5432/app",
      secretRefs: ["PG_PASSWORD"],
      description: "Query the client database.",
    },
  },
  {
    id: "mysql",
    label: "MySQL",
    hint: "Same shape as Postgres, with the MySQL server package.",
    draft: {
      name: "mysql",
      transport: "stdio",
      command: "npx",
      argsText: "-y\n@benborla29/mcp-server-mysql",
      envText:
        "MYSQL_HOST=db.example.com\nMYSQL_PORT=3306\nMYSQL_USER=app\nMYSQL_PASS=${MYSQL_PASSWORD}\nMYSQL_DB=app",
      secretRefs: ["MYSQL_PASSWORD"],
      description: "Query the client database.",
    },
  },
  {
    id: "fetch",
    label: "Fetch / HTTP",
    hint: "Lets the agent retrieve a URL and read it as markdown.",
    draft: {
      name: "fetch",
      transport: "stdio",
      command: "uvx",
      argsText: "mcp-server-fetch",
      description: "Fetch a web page and convert it to markdown.",
    },
  },
  {
    id: "custom-http",
    label: "Custom HTTP server",
    hint: "A remote endpoint — Jira, a WooCommerce store, an internal API.",
    draft: {
      name: "",
      transport: "http",
      url: "https://example.com/mcp",
      headersText: "Authorization: Bearer ${API_TOKEN}",
      secretRefs: ["API_TOKEN"],
      description: "",
    },
  },
];

/**
 * MCPServersState holds the pure transitions of the registry editor: building
 * and validating a draft, parsing the three text areas, keeping the table
 * sorted after a write, and describing an entry in words. Keeping them here —
 * not in the component — is what makes them testable, and mirrors the
 * backend's own rules so a form error arrives before the round trip.
 */
class MCPServersState {
  /** A blank draft for the Add dialog. */
  emptyDraft(): MCPDraft {
    return {
      name: "",
      transport: "stdio",
      command: "",
      argsText: "",
      envText: "",
      url: "",
      headersText: "",
      description: "",
      scopeAll: true,
      projectIds: [],
      providers: [...MCP_PROVIDERS],
      secretRefs: [],
    };
  }

  /** The draft for editing an existing entry. */
  draftFrom(server: MCPServer): MCPDraft {
    return {
      name: server.name,
      transport: server.transport,
      command: server.command ?? "",
      argsText: (server.args ?? []).join("\n"),
      envText: this.formatPairs(server.env, "="),
      url: server.url ?? "",
      headersText: this.formatPairs(server.headers, ": "),
      description: server.description ?? "",
      scopeAll: server.scope?.all ?? false,
      projectIds: [...(server.scope?.projectIds ?? [])],
      providers:
        server.enabledForProviders && server.enabledForProviders.length > 0
          ? [...server.enabledForProviders]
          : [...MCP_PROVIDERS],
      secretRefs: [...(server.secretRefs ?? [])],
    };
  }

  /** A draft prefilled from one of the seed templates. */
  draftFromTemplate(template: MCPTemplate): MCPDraft {
    return { ...this.emptyDraft(), ...template.draft };
  }

  /** One entry per non-empty line, trimmed of trailing whitespace only. */
  parseLines(text: string): string[] {
    return text
      .split("\n")
      .map((line) => line.trimEnd())
      .filter((line) => line.trim() !== "");
  }

  /** `NAME=value` lines into a map. The first separator wins. */
  parsePairs(text: string, separator: string): Record<string, string> {
    const pairs: Record<string, string> = {};
    for (const line of this.parseLines(text)) {
      const index = line.indexOf(separator);
      if (index <= 0) continue;
      const name = line.slice(0, index).trim();
      const value = line.slice(index + separator.length).trim();
      if (name) pairs[name] = value;
    }
    return pairs;
  }

  formatPairs(pairs: Record<string, string> | undefined, separator: string): string {
    if (!pairs) return "";
    return Object.keys(pairs)
      .sort()
      .map((name) => `${name}${separator}${pairs[name]}`)
      .join("\n");
  }

  /** Every `${KEY}` referenced anywhere in a draft, sorted and de-duplicated. */
  placeholders(draft: MCPDraft): string[] {
    const found = new Set<string>();
    const scan = (value: string) => {
      for (const match of value.matchAll(PLACEHOLDER_PATTERN)) found.add(match[1]);
    };
    scan(draft.url);
    scan(draft.argsText);
    scan(draft.envText);
    scan(draft.headersText);
    return [...found].sort();
  }

  /** The first problem with a draft, or null when it is submittable. */
  validate(draft: MCPDraft, options: { platform: boolean }): string | null {
    const name = draft.name.trim();
    if (!name) return "Name is required.";
    if (!NAME_PATTERN.test(name) || name.length > 48) {
      return "Name must start with a letter or digit and use only letters, digits, '-' or '_'.";
    }
    if (options.platform && !draft.scopeAll && draft.projectIds.length === 0) {
      return "Pick at least one project, or scope the server to all projects.";
    }
    if (draft.providers.length === 0) {
      return "Pick at least one agent that should get this server.";
    }
    if (draft.transport === "stdio") {
      if (!draft.command.trim()) return "A stdio server needs a command.";
      const env = this.parsePairs(draft.envText, "=");
      for (const envName of Object.keys(env)) {
        if (!ENV_NAME_PATTERN.test(envName)) {
          return `"${envName}" is not a valid environment variable name.`;
        }
      }
    } else {
      const url = draft.url.trim();
      if (!/^https?:\/\/[^\s]+$/.test(url)) {
        return "An http server needs an absolute http:// or https:// URL.";
      }
      const headers = this.parsePairs(draft.headersText, ": ");
      for (const headerName of Object.keys(headers)) {
        if (!HEADER_NAME_PATTERN.test(headerName)) {
          return `"${headerName}" is not a valid header name.`;
        }
      }
    }
    const declared = new Set(draft.secretRefs);
    for (const key of this.placeholders(draft)) {
      if (!declared.has(key)) {
        return `\${${key}} has no matching vault key. Add ${key} to the secret references.`;
      }
    }
    return null;
  }

  /** The wire payload for a draft: only the fields its transport uses. */
  toPayload(draft: MCPDraft, options: { platform: boolean }): MCPServerDraftPayload {
    const scope: MCPScope = options.platform
      ? draft.scopeAll
        ? { all: true }
        : { all: false, projectIds: [...draft.projectIds] }
      : { all: false };
    const payload: MCPServerDraftPayload = {
      name: draft.name.trim(),
      transport: draft.transport,
      scope,
      enabledForProviders: [...draft.providers],
      description: draft.description.trim(),
      secretRefs: [...draft.secretRefs],
    };
    if (draft.transport === "stdio") {
      payload.command = draft.command.trim();
      payload.args = this.parseLines(draft.argsText);
      payload.env = this.parsePairs(draft.envText, "=");
      return payload;
    }
    payload.url = draft.url.trim();
    payload.headers = this.parsePairs(draft.headersText, ": ");
    return payload;
  }

  /** Insert or replace one entry, keeping the table in name order. */
  upsert(list: MCPServer[], server: MCPServer): MCPServer[] {
    const next = list.filter((entry) => entry.name !== server.name);
    next.push(server);
    return this.sort(next);
  }

  remove(list: MCPServer[], name: string): MCPServer[] {
    return list.filter((entry) => entry.name !== name);
  }

  sort(list: MCPServer[]): MCPServer[] {
    return [...list].sort((left, right) => left.name.localeCompare(right.name));
  }

  /** "All projects" or "2 projects", for the scope column. */
  scopeLabel(scope: MCPScope | undefined): string {
    if (!scope || scope.all) return "All projects";
    const count = scope.projectIds?.length ?? 0;
    if (count === 0) return "No projects";
    return count === 1 ? "1 project" : `${count} projects`;
  }

  /** What the entry connects to, in one line, for the table's second column. */
  destinationLabel(server: MCPServer): string {
    if (server.transport === "http") return server.url ?? "";
    return [server.command ?? "", ...(server.args ?? [])].join(" ").trim();
  }

  /** The agents an entry is written for, named as the UI names them. */
  providerLabels(server: MCPServer): string[] {
    const providers =
      server.enabledForProviders && server.enabledForProviders.length > 0
        ? server.enabledForProviders
        : [...MCP_PROVIDERS];
    return providers.map((id) => MCP_PROVIDER_LABELS[id] ?? id);
  }

  /** Which of a project's available entries are switched off. */
  disabledNames(entries: MCPProjectEntry[]): string[] {
    return entries.filter((entry) => !entry.enabled).map((entry) => entry.name);
  }

  /** The project-only entries, which are the ones a member may edit. */
  projectOwned(entries: MCPProjectEntry[]): MCPProjectEntry[] {
    return entries.filter((entry) => entry.source === "project");
  }

  /** Flip one entry's toggle, returning the new disabled list to send. */
  toggle(entries: MCPProjectEntry[], name: string): string[] {
    return entries
      .filter((entry) => (entry.name === name ? entry.enabled : !entry.enabled))
      .map((entry) => entry.name);
  }

  /** "Materialized 3 minutes ago", or the never state. */
  materializedLabel(at: number | undefined, now: number): string {
    if (!at) return "Not yet written into this container.";
    const seconds = Math.max(0, Math.round((now - at) / 1000));
    if (seconds < 60) return "Materialized just now.";
    const minutes = Math.round(seconds / 60);
    if (minutes < 60) return `Materialized ${minutes} minute${minutes === 1 ? "" : "s"} ago.`;
    const hours = Math.round(minutes / 60);
    if (hours < 24) return `Materialized ${hours} hour${hours === 1 ? "" : "s"} ago.`;
    const days = Math.round(hours / 24);
    return `Materialized ${days} day${days === 1 ? "" : "s"} ago.`;
  }
}

export const mcpServersState = new MCPServersState();
