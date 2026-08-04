import { describe, expect, it } from "vitest";

import {
  availableWorkspaces,
  commandItems,
  routeDefinition,
  workspaceIDFromPath,
} from "./workspaces";

describe("console workspace registry", () => {
  it("maps routes to the intended workspace and layout", () => {
    expect(workspaceIDFromPath("/admin/workbench")).toBe("admin");
    expect(routeDefinition("/user/agent").layout).toBe("canvas");
    expect(routeDefinition("/admin/config").layout).toBe("document");
  });

  it("uses the bootstrap response as the workspace capability gate", () => {
    const bootstrap = { workspaces: [{ id: "user" }] };
    expect(availableWorkspaces(bootstrap).map((workspace) => workspace.id)).toEqual(["user"]);
    expect(commandItems(bootstrap).every((item) => item.workspace.id === "user")).toBe(true);
  });

  it("does not invent Trainer navigation before the backend advertises it", () => {
    expect(availableWorkspaces({ workspaces: [{ id: "user" }, { id: "admin" }] }).map((workspace) => workspace.id)).not.toContain("trainer");
  });

  it("exposes the report document only when the backend advertises Trainer", () => {
    const bootstrap = { workspaces: [{ id: "user" }, { id: "trainer", capabilities: ["eval.reports"] }] };
    expect(availableWorkspaces(bootstrap).map((workspace) => workspace.id)).toEqual(["user", "trainer"]);
    expect(commandItems(bootstrap).find((item) => item.workspace.id === "trainer")).toMatchObject({
      path: "/trainer/reports",
      label: "Reports",
      layout: "document",
    });
    expect(routeDefinition("/trainer/reports").workspace.id).toBe("trainer");
  });
});
