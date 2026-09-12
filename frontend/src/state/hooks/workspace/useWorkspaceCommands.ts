import type { ChatMeta } from "../../../models/chat";
import type { ProjectMeta } from "../../../models/project";
import { useConfirm } from "../../context/ConfirmContext";
import { useWorkspaceContext } from "../../context/WorkspaceContext";
import { chatApi } from "../../../api/chatApi";

export function useWorkspaceCommands() {
  const workspace = useWorkspaceContext();
  const confirm = useConfirm();

  // Opens the new-project dialog, which owns the name field and the template
  // picker. Errors surface inside the dialog rather than in an alert().
  function newProject() {
    workspace.openNewProject();
  }

  async function newChatInProject(projectId?: string) {
    try {
      await workspace.createChat(projectId);
    } catch (error) {
      alert("create chat failed: " + (error as Error).message);
    }
  }

  async function deleteChat(chat: ChatMeta, event: Event) {
    event.stopPropagation();
    await confirm({
      title: "Delete chat",
      description: "This action cannot be undone.",
      message: `"${chat.title || "Untitled chat"}" and its full message history will be permanently removed.`,
      confirmLabel: "Delete chat",
      pendingLabel: "Deleting\u2026",
      action: () => workspace.deleteChat(chat.id),
    });
  }

  async function toggleChatUnread(chat: ChatMeta, event: Event) {
    event.stopPropagation();
    const unread = (chat.lastMessageAt || 0) > (chat.lastReadAt || 0);
    try {
      if (unread) await chatApi.markRead(chat.id);
      else await chatApi.markUnread(chat.id);
    } catch (error) {
      alert("read state update failed: " + (error as Error).message);
    }
  }

  async function forkChat(chat: ChatMeta, event: Event) {
    event.stopPropagation();
    try {
      await workspace.forkChat(chat.id);
    } catch (error) {
      alert("fork failed: " + (error as Error).message);
    }
  }

  async function reorderProjects(projectIds: string[]) {
    try {
      await workspace.reorderProjects(projectIds);
    } catch (error) {
      alert("reorder failed: " + (error as Error).message);
    }
  }

  async function deleteProject(project: ProjectMeta, event: Event) {
    event.stopPropagation();
    const chatsInProject = workspace.chats.filter((chat) => chat.projectId === project.id).length;
    // The files are recoverable; the chats are not, so they stay in the text.
    const chatNote =
      chatsInProject > 0
        ? ` The ${chatsInProject} chat${chatsInProject === 1 ? "" : "s"} inside it are removed for good.`
        : "";
    await confirm({
      title: "Delete project",
      description: "The project can be restored from Settings -> Trash for 7 days.",
      message:
        `The container for "${project.name}" is destroyed and the project moves to Trash.${chatNote}`,
      confirmLabel: "Move to Trash",
      pendingLabel: "Moving to Trash\u2026",
      action: () => workspace.deleteProject(project.id),
    });
  }

  async function startProject(project: ProjectMeta, event: Event) {
    event.stopPropagation();
    try {
      await workspace.startProject(project.id);
    } catch (error) {
      alert("start failed: " + (error as Error).message);
    }
  }

  async function stopProject(project: ProjectMeta, event: Event) {
    event.stopPropagation();
    try {
      await workspace.stopProject(project.id);
    } catch (error) {
      alert("stop failed: " + (error as Error).message);
    }
  }

  return {
    newProject,
    newChatInProject,
    deleteChat,
    toggleChatUnread,
    forkChat,
    reorderProjects,
    deleteProject,
    startProject,
    stopProject,
  };
}
