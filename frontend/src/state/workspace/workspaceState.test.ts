import assert from "node:assert/strict";
import test from "node:test";
import type { ChatMeta } from "../../models/chat.ts";
import type { ProjectMeta } from "../../models/project.ts";
import { workspaceSidebarState } from "./workspaceSidebarState.ts";
import { workspaceUiState } from "./workspaceUiState.ts";

const projects: ProjectMeta[] = [
  {
    id: "older",
    name: "Older project",
    slug: "older-project",
    cwd: "/older",
    containerName: "older",
    status: "running",
    order: 1,
    createdAt: 1,
    updatedAt: 1,
  },
  {
    id: "newer",
    name: "Newer project",
    slug: "newer-project",
    cwd: "/newer",
    containerName: "newer",
    status: "running",
    order: 2,
    createdAt: 2,
    updatedAt: 2,
  },
];

const chats: ChatMeta[] = [
  { id: "old-chat", title: "Old", projectId: "newer", createdAt: 1, lastMessageAt: 1 },
  { id: "new-chat", title: "New", projectId: "newer", createdAt: 2, lastMessageAt: 2 },
  { id: "loose", title: "Loose", createdAt: 3, lastMessageAt: 3 },
];

test("preserves workspace UI transitions and sidebar ordering", () => {
  const open = workspaceUiState.reduce(workspaceUiState.createInitial(), { type: "open-sidebar" });
  assert.deepEqual(workspaceUiState.reduce(open, { type: "select-chat", chatId: "new-chat" }), {
    activeChatId: "new-chat",
    containerProjectId: null,
    containerTab: null,
    settingsTab: null,
    sidebarOpen: false,
    view: "chat",
  });

  // A destination can be asked for a specific sub-tab; asking again without one
  // clears it, so the page keeps whatever the operator last opened.
  const snapshots = workspaceUiState.reduce(open, {
    type: "show-project-containers",
    projectId: "newer",
    tab: "snapshots",
  });
  assert.equal(snapshots.containerTab, "snapshots");
  assert.equal(
    workspaceUiState.reduce(snapshots, { type: "show-project-containers", projectId: "newer" })
      .containerTab,
    null,
  );
  assert.equal(workspaceUiState.reduce(open, { type: "show-settings", tab: "trash" }).settingsTab, "trash");

  const model = workspaceSidebarState.model(chats, projects, "");
  assert.deepEqual(model.visibleProjects.map((node) => node.project.id), ["newer", "older"]);
  assert.deepEqual(model.visibleProjects[0].chats.map((chat) => chat.id), ["new-chat", "old-chat"]);
  assert.deepEqual(model.visibleLooseChats.map((chat) => chat.id), ["loose"]);
});

test("offers a cross-project recent strip only when it adds something", () => {
  const model = workspaceSidebarState.model(chats, projects, "");
  assert.deepEqual(model.recentChats.map((chat) => chat.id), ["loose", "new-chat", "old-chat"]);

  // One project: the project list already is the recent list.
  const single = workspaceSidebarState.model(chats, [projects[0]], "");
  assert.deepEqual(single.recentChats, []);

  // A search answers "where is that chat"; the strip answers a different one.
  assert.deepEqual(workspaceSidebarState.model(chats, projects, "new").recentChats, []);

  assert.equal(workspaceSidebarState.recentChats(chats, 1).length, 1);
});

test("points a project row at its newest chat", () => {
  assert.equal(workspaceSidebarState.mostRecentChatId(chats, "newer"), "new-chat");
  // A project with no chats has nothing to open, so the row starts one.
  assert.equal(workspaceSidebarState.mostRecentChatId(chats, "older"), null);
});
