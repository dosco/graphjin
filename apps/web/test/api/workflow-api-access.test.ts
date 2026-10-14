import { beforeEach, describe, expect, it, vi } from "vitest";
import { NextResponse } from "next/server";

const mocks = vi.hoisted(() => {
  class TestWorkflowApiError extends Error {
    constructor(
      readonly code: string,
      message: string,
      readonly status: number,
    ) {
      super(message);
    }
  }
  return {
    TestWorkflowApiError,
    requireAdmin: vi.fn(),
    getOrgId: vi.fn(),
    getAccess: vi.fn(),
    enable: vi.fn(),
    rotate: vi.fn(),
    disable: vi.fn(),
    updateLimits: vi.fn(),
  };
});

vi.mock("@/lib/admin-auth", () => ({
  requireAdminActor: mocks.requireAdmin,
  isDenied: (value: unknown) => value instanceof Response,
}));
vi.mock("@/lib/db", () => ({ getOrgId: mocks.getOrgId }));
vi.mock("@neko/llm/workflows", () => ({
  WorkflowApiError: mocks.TestWorkflowApiError,
  getWorkflowApiAccess: mocks.getAccess,
  getLastWorkflowApiRequest: async () => null,
  enableWorkflowApiAccess: mocks.enable,
  rotateWorkflowApiToken: mocks.rotate,
  disableWorkflowApiAccess: mocks.disable,
  updateWorkflowApiLimits: mocks.updateLimits,
}));

import {
  DELETE,
  GET,
  PATCH,
  POST,
} from "@/app/api/workflows/[workflowId]/api-access/route";

const context = { params: Promise.resolve({ workflowId: "workflow-a" }) };
const access = {
  workflowId: "workflow-a",
  enabled: false,
  tokenPrefix: null,
  tokenCreatedAt: null,
  tokenRotatedAt: null,
  lastUsedAt: null,
  limits: { batchMaxRecords: 1000 },
  batch: { available: false, recordsField: null, columns: [] },
  createdAt: null,
  updatedAt: null,
};

beforeEach(() => {
  vi.clearAllMocks();
  mocks.requireAdmin.mockResolvedValue({ userId: "admin-1", role: "admin" });
  mocks.getOrgId.mockResolvedValue("org-a");
  mocks.getAccess.mockResolvedValue(access);
  mocks.enable.mockResolvedValue({
    access: { ...access, enabled: true, tokenPrefix: "onk_wf_deadbeefdead" },
    token: "onk_wf_deadbeefdead_one-time-secret",
  });
  mocks.rotate.mockResolvedValue({
    access: { ...access, enabled: true, tokenPrefix: "onk_wf_feedfacefeed" },
    token: "onk_wf_feedfacefeed_new-one-time-secret",
  });
  mocks.disable.mockResolvedValue(access);
  mocks.updateLimits.mockResolvedValue({
    ...access,
    limits: { batchMaxRecords: 250 },
  });
});

describe("workflow API access administration", () => {
  it("keeps reads and lifecycle writes behind the administrator gate", async () => {
    mocks.requireAdmin.mockResolvedValueOnce(
      NextResponse.json({ error: "admin only" }, { status: 403 }),
    );
    const response = (await GET(
      new Request("http://localhost"),
      context,
    ))!;

    expect(response.status).toBe(403);
    expect(mocks.getAccess).not.toHaveBeenCalled();
    expect(mocks.getOrgId).not.toHaveBeenCalled();
  });

  it("never returns a recoverable plaintext token from GET", async () => {
    const response = (await GET(
      new Request("http://localhost"),
      context,
    ))!;
    const body = await response.json();

    expect(response.status).toBe(200);
    expect(body.access).toEqual(access);
    expect(JSON.stringify(body)).not.toContain("one-time-secret");
  });

  it("reveals plaintext only in the enable response and binds the admin actor", async () => {
    const response = (await POST(
      new Request("http://localhost", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ action: "enable" }),
      }),
      context,
    ))!;
    const body = await response.json();

    expect(response.status).toBe(201);
    expect(body.token).toBe("onk_wf_deadbeefdead_one-time-secret");
    expect(mocks.enable).toHaveBeenCalledWith({
      orgId: "org-a",
      workflowId: "workflow-a",
      actor: { userId: "admin-1", role: "admin" },
    });
  });

  it("routes rotation, disablement, and limit updates independently", async () => {
    const rotateResponse = (await POST(
      new Request("http://localhost", {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ action: "rotate" }),
      }),
      context,
    ))!;
    const patchResponse = (await PATCH(
      new Request("http://localhost", {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ limits: { batchMaxRecords: 250 } }),
      }),
      context,
    ))!;
    const deleteResponse = (await DELETE(
      new Request("http://localhost"),
      context,
    ))!;

    expect(rotateResponse.status).toBe(200);
    expect(patchResponse.status).toBe(200);
    expect(deleteResponse.status).toBe(200);
    expect(mocks.rotate).toHaveBeenCalledTimes(1);
    expect(mocks.updateLimits).toHaveBeenCalledWith(
      expect.objectContaining({ limits: { batchMaxRecords: 250 } }),
    );
    expect(mocks.disable).toHaveBeenCalledWith(
      expect.objectContaining({ orgId: "org-a", workflowId: "workflow-a" }),
    );
  });
});
