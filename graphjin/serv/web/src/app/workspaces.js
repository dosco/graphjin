import {
  Activity,
  Bot,
  Braces,
  Code2,
  FileText,
  FlaskConical,
  Library,
  ListChecks,
  Radar,
  ShieldCheck,
  SlidersHorizontal,
  SquareTerminal,
  Workflow,
} from "lucide-react";

export const workspaceDefinitions = {
  user: {
    id: "user",
    label: "User",
    description: "Governed agent work",
    defaultPath: "/user/agent",
    items: [
      { path: "/user/agent", label: "Agent", icon: Bot, layout: "canvas" },
      { path: "/user/tasks", label: "Tasks", icon: ListChecks, layout: "collection" },
      { path: "/user/watches", label: "Watches", icon: Radar, layout: "collection" },
      { path: "/user/artifacts", label: "Artifacts", icon: FileText, layout: "collection" },
    ],
  },
  admin: {
    id: "admin",
    label: "Admin",
    description: "Runtime and configuration",
    defaultPath: "/admin/runtime",
    items: [
      { path: "/admin/runtime", label: "Runtime", icon: Activity, layout: "collection" },
      { path: "/admin/workbench", label: "Workbench", icon: SquareTerminal, layout: "canvas" },
      { path: "/admin/catalog", label: "Catalog", icon: Library, layout: "collection" },
      { path: "/admin/workflows", label: "Workflows", icon: Workflow, layout: "collection" },
      { path: "/admin/security", label: "Security", icon: ShieldCheck, layout: "collection" },
      { path: "/admin/code", label: "Code", icon: Code2, layout: "document" },
      { path: "/admin/config", label: "Config", icon: SlidersHorizontal, layout: "document" },
      { path: "/admin/api", label: "API", icon: Braces, layout: "document" },
    ],
  },
  trainer: {
    id: "trainer",
    label: "Trainer",
    description: "Evals and model training",
    defaultPath: "/trainer/reports",
    items: [
      { path: "/trainer/reports", label: "Reports", icon: FlaskConical, layout: "document" },
    ],
  },
};

export const defaultWorkspaceAvailability = ["user", "admin"];

export function workspaceIDFromPath(pathname) {
  const candidate = String(pathname || "").split("/").filter(Boolean)[0];
  return workspaceDefinitions[candidate] ? candidate : "user";
}

export function routeDefinition(pathname) {
  for (const workspace of Object.values(workspaceDefinitions)) {
    const item = workspace.items.find((candidate) => pathname.startsWith(candidate.path));
    if (item) {
      return { ...item, workspace };
    }
  }
  return { layout: "collection", workspace: workspaceDefinitions.user };
}

export function availableWorkspaces(bootstrap) {
  const ids = Array.isArray(bootstrap?.workspaces)
    ? bootstrap.workspaces.map((workspace) => workspace?.id).filter(Boolean)
    : defaultWorkspaceAvailability;
  return ids.map((id) => workspaceDefinitions[id]).filter(Boolean);
}

export function commandItems(bootstrap) {
  return availableWorkspaces(bootstrap).flatMap((workspace) =>
    workspace.items.map((item) => ({ ...item, workspace }))
  );
}
