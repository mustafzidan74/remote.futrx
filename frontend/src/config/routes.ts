import type { ApplicationPath } from "../types/transport";

function applicationPath(path: ApplicationPath): ApplicationPath {
  return path;
}

export const API_ROUTES = {
  authSession: "/auth/me",
  googleOAuth: "/api/admin/auth/google",
  audit: {
    collection: (query: string) =>
      `/api/admin/audit${query ? `?${query}` : ""}`,
    export: (query: string) =>
      `/api/admin/audit/export${query ? `?${query}` : ""}`,
  },
  chats: {
    collection: "/api/chats",
    item: (id: string) => `/api/chats/${encodeURIComponent(id)}`,
    read: (id: string) => `/api/chats/${encodeURIComponent(id)}/read`,
    unread: (id: string) => `/api/chats/${encodeURIComponent(id)}/unread`,
    fork: (id: string) => `/api/chats/${encodeURIComponent(id)}/fork`,
    files: (id: string, path = "") =>
      `/api/chats/${encodeURIComponent(id)}/files${path ? `?path=${encodeURIComponent(path)}` : ""}`,
    filesSearch: (id: string, query: string) =>
      `/api/chats/${encodeURIComponent(id)}/files/search?q=${encodeURIComponent(query)}`,
    fileDownload: (id: string, path: string) =>
      `/api/chats/${encodeURIComponent(id)}/files/download?path=${encodeURIComponent(path)}`,
    folderDownload: (id: string, path = "") =>
      `/api/chats/${encodeURIComponent(id)}/files/download-folder${path ? `?path=${encodeURIComponent(path)}` : ""}`,
    mediaOpen: (id: string, path: string) =>
      `/api/chats/${encodeURIComponent(id)}/media-open?path=${encodeURIComponent(path)}`,
    ideOpen: (id: string, path: string) =>
      `/api/chats/${encodeURIComponent(id)}/ide-open?path=${encodeURIComponent(path)}`,
    events: (id: string, query: string) =>
      `/api/chats/${encodeURIComponent(id)}/events${query ? `?${query}` : ""}`,
    rewind: (id: string) => `/api/chats/${encodeURIComponent(id)}/rewind`,
    historyRepos: (id: string) =>
      `/api/chats/${encodeURIComponent(id)}/history/repos`,
    historyCommits: (id: string, query: string) =>
      `/api/chats/${encodeURIComponent(id)}/history/commits?${query}`,
    historyDiff: (id: string, query: string) =>
      `/api/chats/${encodeURIComponent(id)}/history/diff?${query}`,
    historyCheckout: (id: string) =>
      `/api/chats/${encodeURIComponent(id)}/history/checkout`,
    schedules: (id: string) =>
      `/api/chats/${encodeURIComponent(id)}/schedules`,
  },
  schedules: {
    item: (id: string) => `/api/schedules/${encodeURIComponent(id)}`,
    run: (id: string) => `/api/schedules/${encodeURIComponent(id)}/run`,
  },
  claudeAuth: {
    status: "/api/claude/auth-status",
    startLogin: "/api/claude/login/start",
    submitCode: "/api/claude/login/code",
    cancelLogin: "/api/claude/login/cancel",
  },
  codexAuth: {
    status: "/api/codex/auth-status",
    startDeviceLogin: "/api/codex/login/device",
  },
  kimiAuth: {
    status: "/api/kimi/auth-status",
    startDeviceLogin: "/api/kimi/login/device",
  },
  projects: {
    collection: "/api/projects",
    item: (id: string) => `/api/projects/${encodeURIComponent(id)}`,
    reorder: "/api/projects/reorder",
    health: "/api/projects/health",
    trash: "/api/projects/trash",
    restore: (id: string) => `/api/projects/${encodeURIComponent(id)}/restore`,
    purge: (id: string) => `/api/projects/${encodeURIComponent(id)}/purge`,
    snapshots: (id: string) => `/api/projects/${encodeURIComponent(id)}/snapshots`,
    snapshot: (id: string, snapshotId: string) =>
      `/api/projects/${encodeURIComponent(id)}/snapshots/${encodeURIComponent(snapshotId)}`,
    snapshotRestore: (id: string, snapshotId: string) =>
      `/api/projects/${encodeURIComponent(id)}/snapshots/${encodeURIComponent(snapshotId)}/restore`,
    start: (id: string, force = false) =>
      `/api/projects/${encodeURIComponent(id)}/start${force ? "?force=1" : ""}`,
    stop: (id: string) => `/api/projects/${encodeURIComponent(id)}/stop`,
    restart: (id: string) => `/api/projects/${encodeURIComponent(id)}/restart`,
    container: (id: string) =>
      `/api/projects/${encodeURIComponent(id)}/container`,
    resources: (id: string) =>
      `/api/projects/${encodeURIComponent(id)}/resources`,
    repairNetwork: (id: string) =>
      `/api/projects/${encodeURIComponent(id)}/repair-network`,
    apps: (id: string) => `/api/projects/${encodeURIComponent(id)}/apps`,
    screenshot: (id: string) => `/api/projects/${encodeURIComponent(id)}/screenshot`,
    screenshots: (id: string) => `/api/projects/${encodeURIComponent(id)}/screenshots`,
    screenshotSend: (id: string, screenshotId: string) =>
      `/api/projects/${encodeURIComponent(id)}/screenshots/${encodeURIComponent(screenshotId)}/send`,
    agentBrowser: (id: string, scope?: string) =>
      `/api/projects/${encodeURIComponent(id)}/agent-browser${scope ? `?scope=${encodeURIComponent(scope)}` : ""}`,
    startAgentBrowser: (id: string) =>
      `/api/projects/${encodeURIComponent(id)}/agent-browser/start`,
    navigateAgentBrowser: (id: string) =>
      `/api/projects/${encodeURIComponent(id)}/agent-browser/navigate`,
    secrets: (id: string) =>
      `/api/projects/${encodeURIComponent(id)}/secrets`,
    secret: (id: string, key: string) =>
      `/api/projects/${encodeURIComponent(id)}/secrets/${encodeURIComponent(key)}`,
    portal: (id: string) => `/api/projects/${encodeURIComponent(id)}/portal`,
    clientMessage: (id: string) =>
      `/api/projects/${encodeURIComponent(id)}/client-message`,
    shares: (id: string) => `/api/projects/${encodeURIComponent(id)}/shares`,
    share: (id: string, shareId: string) =>
      `/api/projects/${encodeURIComponent(id)}/shares/${encodeURIComponent(shareId)}`,
    usage: (id: string, query = "") =>
      `/api/projects/${encodeURIComponent(id)}/usage${query ? `?${query}` : ""}`,
    access: (id: string) => `/api/projects/${encodeURIComponent(id)}/access`,
    accessMember: (id: string, email: string) =>
      `/api/projects/${encodeURIComponent(id)}/access/${encodeURIComponent(email)}`,
  },
  settings: "/api/me/settings",
  snippets: {
    collection: "/api/me/snippets",
    item: (id: string) => `/api/me/snippets/${encodeURIComponent(id)}`,
    use: (id: string) => `/api/me/snippets/${encodeURIComponent(id)}/use`,
    import: "/api/me/snippets/import",
  },
  usage: {
    summary: (query: string) => `/api/usage/summary${query ? `?${query}` : ""}`,
    records: (query: string) => `/api/usage/records${query ? `?${query}` : ""}`,
    prices: "/api/admin/usage/prices",
    rebuild: "/api/admin/usage/rebuild",
  },
  serverInfo: "/api/server/info",
  adminResources: "/api/admin/resources",
  selfUpdate: {
    status: "/api/admin/update/status",
    check: "/api/admin/update/check",
    apply: "/api/admin/update/apply",
  },
  notifications: {
    settings: "/api/admin/notifications",
    test: "/api/admin/notifications/test",
    digestSendNow: "/api/admin/notifications/digest/send-now",
  },
  monitoring: {
    settings: "/api/admin/monitoring",
    ping: "/api/admin/monitoring/ping",
  },
  transcription: {
    transcribe: "/api/transcribe",
    clientConfig: "/api/transcribe/config",
    settings: "/api/admin/transcription",
    test: "/api/admin/transcription/test",
  },
  skills: (query: string) => `/api/skills?${query}`,
  globalSkills: {
    collection: "/api/admin/skills-global",
    item: (name: string) =>
      `/api/admin/skills-global/${encodeURIComponent(name)}`,
    import: "/api/admin/skills-global/import",
  },
  secretsVault: {
    collection: "/api/admin/secrets",
    item: (key: string) => `/api/admin/secrets/${encodeURIComponent(key)}`,
    test: (key: string) => `/api/admin/secrets/${encodeURIComponent(key)}/test`,
  },
  playbooks: {
    collection: "/api/playbooks",
    admin: "/api/admin/playbooks",
  },
  agentPreferences: "/api/admin/agent-preferences",
  search: (query: string) => `/api/search?${query}`,
  templates: "/api/templates",
  uploads: "/api/uploads",
  users: {
    collection: "/api/admin/users",
    item: (email: string) =>
      `/api/admin/users/${encodeURIComponent(email)}`,
    role: (email: string) =>
      `/api/admin/users/${encodeURIComponent(email)}/role`,
  },
} as const;

export const WEB_SOCKET_ROUTES = {
  workspace: applicationPath("/ws/workspace"),
  claudeAuthStatus: applicationPath("/ws/claude/auth-status"),
  codexAuthStatus: applicationPath("/ws/codex/auth-status"),
  kimiAuthStatus: applicationPath("/ws/kimi/auth-status"),
  chat: (chatId: string, sinceSeq: number): ApplicationPath => {
    const route = applicationPath(`/ws/chat/${encodeURIComponent(chatId)}`);
    return sinceSeq > 0
      ? applicationPath(`${route}?since=${sinceSeq}`)
      : route;
  },
  terminal: (chatId: string): ApplicationPath =>
    applicationPath(`/ws/terminal?chat=${encodeURIComponent(chatId)}`),
} as const;
