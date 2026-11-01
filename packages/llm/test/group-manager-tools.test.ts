import { beforeEach, describe, expect, it, vi } from "vitest";

const sdk = vi.hoisted(() => ({
  tools: new Map<string, (args: Record<string, unknown>) => Promise<{ content: Array<{ type: string; text: string }> }>>(),
}));

vi.mock("../src/mcp-server", () => ({
  createMcpServer: vi.fn((input: unknown) => input),
  defineMcpTool: vi.fn((name: string, _description: string, _schema: unknown, handler: (args: Record<string, unknown>) => Promise<{ content: Array<{ type: string; text: string }> }>) => {
    sdk.tools.set(name, handler);
    return { name };
  }),
}));

import { buildUserManagerServer } from "../src/work/tools";

describe("group manager tools", () => {
  const created: Array<Record<string, unknown>> = [];
  const events: unknown[] = [];

  beforeEach(() => {
    sdk.tools.clear();
    created.length = 0;
    events.length = 0;
    buildUserManagerServer({
      orgId: "org-1",
      runId: "run-1",
      emit: (event) => {
        events.push(event);
      },
      controlPlane: {
        evaluateActionPolicy: async () => ({ decision: "needs_approval", mode: "approval_required", reason: "admin", policy: { id: "p1", name: "group_management_default" } }),
        createActionRequest: async (input: Record<string, unknown>) => {
          created.push(input);
          return { id: `ar-${created.length}`, status: String(input.status) };
        },
        enqueueActionExecute: async () => {},
        listGroups: async () => ({ groups: [], idpRules: [], groupDataAccessEnabled: false }),
        listUsers: async () => ({ users: [], groups: [], memberships: [] }),
      } as never,
    });
  });

  it("lists groups and files approval-gated group_admin requests", async () => {
    expect(JSON.parse((await sdk.tools.get("list_groups")!({})).content[0]!.text)).toEqual({ groups: [], idpRules: [], groupDataAccessEnabled: false });

    const member = JSON.parse((await sdk.tools.get("request_group_change")!({ action: "add_member", groupId: "g1", userId: "u1", intent: "Ann joins Finance" })).content[0]!.text);
    expect(member).toMatchObject({ ok: true, decision: "pending_approval" });
    expect(created[0]).toMatchObject({ kind: "group_admin", scope: "internal", target: "g1", workRunId: "run-1", payload: { action: "add_member", groupId: "g1", userId: "u1" } });

    await sdk.tools.get("request_item_grant")!({ action: "grant_item", groupId: "g1", itemType: "skill", itemId: "docx", intent: "docx for Finance" });
    expect(created[1]).toMatchObject({ target: "skill:docx", payload: { action: "grant_item", itemType: "skill", itemId: "docx" } });

    await sdk.tools.get("request_data_access_change")!({
      action: "set_table_access", groupId: "g1", source: "shop", table: "orders", columns: ["id"],
      rowFilter: { column: "region", op: "eq", value: "emea" }, intent: "EMEA orders",
    });
    expect(created[2]).toMatchObject({ target: "shop:orders", payload: { rowFilter: { column: "region", op: "eq", value: "emea" } } });
    expect(events).toHaveLength(3);
  });

  it("rejects incomplete requests without filing them", async () => {
    const missing = JSON.parse((await sdk.tools.get("request_group_change")!({ action: "add_member", groupId: "g1", intent: "x" })).content[0]!.text);
    expect(missing).toEqual({ ok: false, error: "add_member needs userId" });
    const table = JSON.parse((await sdk.tools.get("request_data_access_change")!({ action: "set_table_access", groupId: "g1", intent: "x" })).content[0]!.text);
    expect(table.ok).toBe(false);
    expect(created).toEqual([]);
  });
});
