import { useEffect, useState } from "preact/hooks";
import type { ChatMeta } from "../../../models/chat";
import type { ProjectMeta } from "../../../models/project";
import { workspaceSidebarState } from "../../workspace/workspaceSidebarState";

export function useSidebarState(
  open: boolean,
  onClose: () => void,
  projects: ProjectMeta[],
  chats: ChatMeta[]
) {
  const [query, setQuery] = useState("");
  const [collapsed, setCollapsed] = useState<Record<string, boolean>>({});
  const [recentOpen, setRecentOpen] = useState(true);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(() =>
    workspaceSidebarState.readCollapsed()
  );

  useEffect(() => {
    if (!open) return;
    const handler = (event: KeyboardEvent) => {
      if (event.key === "Escape") onClose();
    };
    window.addEventListener("keydown", handler);
    return () => window.removeEventListener("keydown", handler);
  }, [open, onClose]);

  useEffect(() => {
    setCollapsed((current) => {
      const next = workspaceSidebarState.collapsedProjects(projects, chats);
      return workspaceSidebarState.hasSameCollapsedProjects(current, next) ? current : next;
    });
  }, [projects, chats]);

  useEffect(() => {
    workspaceSidebarState.writeCollapsed(sidebarCollapsed);
  }, [sidebarCollapsed]);

  function toggleCollapsed(id: string) {
    setCollapsed((current) => ({ ...current, [id]: !current[id] }));
  }

  function toggleSidebarCollapsed() {
    setSidebarCollapsed((current) => !current);
  }

  return {
    query,
    setQuery,
    collapsed,
    toggleCollapsed,
    recentOpen,
    toggleRecent: () => setRecentOpen((current) => !current),
    sidebarCollapsed,
    toggleSidebarCollapsed,
  };
}
