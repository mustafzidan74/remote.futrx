import type { ChatMeta } from "../models/chat";
import type { ProjectHealth } from "../models/health";
import type { ProjectMeta } from "../models/project";

export type WorkspaceMessage =
  | {
      type: "workspace.snapshot";
      chats: ChatMeta[];
      projects: ProjectMeta[];
      /** Health rows for the projects in this snapshot; absent when the monitor is off. */
      health?: ProjectHealth[];
    }
  | { type: "chat.upsert"; chat: ChatMeta }
  | { type: "chat.delete"; id: string }
  | { type: "project.upsert"; project: ProjectMeta }
  | { type: "project.delete"; id: string }
  /** One project's health verdict. An absent health clears the row. */
  | { type: "project.health"; id: string; health?: ProjectHealth };
