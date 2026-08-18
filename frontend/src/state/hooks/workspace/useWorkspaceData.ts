import { useEffect, useState } from "preact/hooks";
import { workspaceApi } from "../../../api/workspaceApi";
import type { WorkspaceMessage } from "../../../types/workspaceApi";
import type { ChatMeta } from "../../../models/chat";
import type { ProjectMeta } from "../../../models/project";
import { workspaceDataProjector } from "../../workspace/workspaceDataProjector";
import {
  projectHealthState,
  type ProjectHealthMap,
} from "../../workspace/projectHealthState";

export function useWorkspaceData(enabled: boolean) {
  const [chats, setChats] = useState<ChatMeta[]>([]);
  const [projects, setProjects] = useState<ProjectMeta[]>([]);
  const [health, setHealth] = useState<ProjectHealthMap>({});

  useEffect(() => {
    if (!enabled) {
      setChats((current) => workspaceDataProjector.replaceChats([], current));
      setProjects((current) => workspaceDataProjector.replaceProjects([], current));
      setHealth({});
      return;
    }

    return workspaceApi.subscribe(applyWorkspaceMessage);
  }, [enabled]);

  function applyWorkspaceMessage(message: WorkspaceMessage) {
    switch (message.type) {
      case "workspace.snapshot":
        setChats((current) => workspaceDataProjector.replaceChats(message.chats, current));
        setProjects((current) =>
          workspaceDataProjector.replaceProjects(message.projects, current)
        );
        setHealth(projectHealthState.replace(message.health));
        break;
      case "chat.upsert":
        setChats((current) => workspaceDataProjector.upsertChat(current, message.chat));
        break;
      case "chat.delete":
        setChats((current) => workspaceDataProjector.removeChat(current, message.id));
        break;
      case "project.upsert":
        setProjects((current) => workspaceDataProjector.upsertProject(current, message.project));
        break;
      case "project.delete":
        setProjects((current) => workspaceDataProjector.removeProject(current, message.id));
        setHealth((current) => projectHealthState.apply(current, message.id));
        break;
      case "project.health":
        setHealth((current) =>
          projectHealthState.apply(current, message.id, message.health)
        );
        break;
    }
  }

  return {
    chats,
    projects,
    health,
  };
}
