/** /api/admin/graphjin/config-file: admin-only GraphJin config file editor. */

import { NextResponse } from "next/server";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { callRoute } from "../_helpers/route";

const mocks = vi.hoisted(() => {
  class GraphjinConfigEditorError extends Error {
    constructor(message: string, readonly status = 400) {
      super(message);
    }
  }
  return {
    admin: true,
    GraphjinConfigEditorError,
    read: vi.fn(async (path: string) => ({ path, text: "auth:\n  secret: <hidden>\n", hash: "h1", hidden: 1 })),
    check: vi.fn(async () => ({ valid: true, scope: "core", reloadMode: "hot", changes: [], findings: [], errors: [] })),
    apply: vi.fn(async () => ({ hash: "h2" })),
    connection: vi.fn(async () => ({ endpoint: "http://graphjin:8080/api/v1/graphql", headers: {} })),
  };
});

vi.mock("@/lib/admin-auth", () => ({
  requireAdminActor: async () =>
    mocks.admin ? { role: "admin", userId: "u1" } : NextResponse.json({ error: "Admins only" }, { status: 403 }),
  isDenied: (value: unknown) => value instanceof Response,
}));
vi.mock("@/lib/db", () => ({ getOrgId: async () => "org-1" }));
vi.mock("@neko/llm/graphjin/config-editor", () => ({
  readEditableGraphjinConfig: mocks.read,
  checkEditedGraphjinConfig: mocks.check,
  applyEditedGraphjinConfig: mocks.apply,
  graphjinAdminConnection: mocks.connection,
  GraphjinConfigEditorError: mocks.GraphjinConfigEditorError,
}));

import { GET, POST } from "@/app/api/admin/graphjin/config-file/route";

describe("/api/admin/graphjin/config-file", () => {
  beforeEach(() => {
    mocks.admin = true;
    process.env.OPENNEKO_GRAPHJIN_CONFIG = "/config/graphjin/agentic.yml";
    for (const fn of [mocks.read, mocks.check, mocks.apply, mocks.connection]) fn.mockClear();
  });
  afterEach(() => {
    delete process.env.OPENNEKO_GRAPHJIN_CONFIG;
  });

  it("is for admins only", async () => {
    mocks.admin = false;
    expect((await callRoute(GET)).status).toBe(403);
    expect((await callRoute(POST, { method: "POST", body: { action: "check", text: "a: 1", baseHash: "h1" } })).status).toBe(403);
    expect(mocks.read).not.toHaveBeenCalled();
  });

  it("returns the file with secrets hidden", async () => {
    const res = await callRoute(GET);
    expect(res).toMatchObject({ status: 200, body: { hash: "h1", hidden: 1 } });
    expect(mocks.read).toHaveBeenCalledWith("/config/graphjin/agentic.yml");
  });

  it("checks without applying, and applies with the base hash", async () => {
    const checked = await callRoute(POST, { method: "POST", body: { action: "check", text: "a: 1", baseHash: "h1" } });
    expect(checked).toMatchObject({ status: 200, body: { valid: true } });
    expect(mocks.apply).not.toHaveBeenCalled();

    const applied = await callRoute(POST, { method: "POST", body: { action: "apply", text: "a: 1", baseHash: "h1" } });
    expect(applied).toMatchObject({ status: 200, body: { applied: true, hash: "h2" } });
    expect(mocks.apply).toHaveBeenCalledWith(expect.objectContaining({ configFile: "/config/graphjin/agentic.yml", edited: "a: 1", baseHash: "h1" }));
    expect(mocks.connection).toHaveBeenCalledWith("org-1", "u1");
  });

  it("passes editor errors through with their status", async () => {
    mocks.apply.mockRejectedValueOnce(new mocks.GraphjinConfigEditorError("The config file changed since you opened it.", 409));
    const res = await callRoute(POST, { method: "POST", body: { action: "apply", text: "a: 1", baseHash: "old" } });
    expect(res).toMatchObject({ status: 409, body: { error: "The config file changed since you opened it." } });
  });

  it("rejects bad requests and deployments without a config file", async () => {
    expect((await callRoute(POST, { method: "POST", body: { action: "delete", text: "", baseHash: "" } })).status).toBe(400);
    delete process.env.OPENNEKO_GRAPHJIN_CONFIG;
    expect((await callRoute(GET)).status).toBe(404);
  });
});
