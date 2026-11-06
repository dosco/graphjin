export const workspaceDefinitions = {
  user: {
    id: "user",
    label: "User",
    description: "Your work",
    defaultPath: "/user/agent",
    items: [
      { path: "/user/agent", label: "Agent", layout: "canvas" },
      { path: "/user/tasks", label: "Tasks", layout: "collection" },
      { path: "/user/watches", label: "Watches", layout: "collection" },
      { path: "/user/artifacts", label: "Artifacts", layout: "collection" },
      { path: "/user/memory", label: "Memory", layout: "collection" },
      { path: "/user/skills", label: "Skills", layout: "collection" },
    ],
  },
  admin: {
    id: "admin",
    label: "Admin",
    description: "Server",
    defaultPath: "/admin/runtime",
    items: [
      { path: "/admin/runtime", label: "Runtime", layout: "collection" },
      { path: "/admin/workbench", label: "Workbench", layout: "canvas" },
      { path: "/admin/catalog", label: "Catalog", layout: "collection" },
      { path: "/admin/workflows", label: "Workflows", layout: "collection" },
      { path: "/admin/security", label: "Security", layout: "collection" },
      { path: "/admin/code", label: "Code", layout: "document" },
      { path: "/admin/config", label: "Config", layout: "document" },
      { path: "/admin/api", label: "API", layout: "document" },
    ],
  },
  trainer: {
    id: "trainer",
    label: "Trainer",
    description: "Evals",
    defaultPath: "/trainer/reports",
    items: [
      { path: "/trainer/reports", label: "Reports", layout: "document" },
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
