import { beforeEach, expect, it, vi } from "vitest";

vi.mock("../src/workflows/action-store", () => ({
  getActionRequest: vi.fn(),
  markActionRequestFailed: vi.fn(),
  markActionRequestExecuted: vi.fn(),
  recordActionExecution: vi.fn().mockResolvedValue({ id: "execution" }),
  finishActionExecution: vi.fn(),
}));

import * as store from "../src/workflows/action-store";
import { executeApprovedActionRequest, registerActionAdapter, registerFallbackActionAdapterResolver } from "../src/workflows/action-executor";

beforeEach(() => vi.clearAllMocks());

function approved(kind: string) {
  vi.mocked(store.getActionRequest).mockResolvedValue({
    id: "request", orgId: "org", kind, status: "approved", payload: {},
  } as Awaited<ReturnType<typeof store.getActionRequest>>);
}

it("records unknown kinds as failed executions without reporting success", async () => {
  approved("missing_connector");
  await expect(executeApprovedActionRequest("org", "request")).resolves.toEqual({
    ok: false, error: 'no adapter registered for kind "missing_connector"',
  });
  expect(store.markActionRequestFailed).toHaveBeenCalledWith("request", 'no adapter registered for kind "missing_connector"');
  expect(store.recordActionExecution).toHaveBeenCalledOnce();
  expect(store.finishActionExecution).toHaveBeenCalledWith(expect.objectContaining({ status: "failed", error: 'no adapter registered for kind "missing_connector"' }));
  expect(store.markActionRequestExecuted).not.toHaveBeenCalled();
});

it.each(["reconcile_required", "partially_applied"] as const)("persists the %s adapter outcome without claiming success", async (status) => {
  approved(`test_${status}`);
  registerActionAdapter(`test_${status}`, async () => ({ status, result: { status } }));
  await expect(executeApprovedActionRequest("org", "request")).resolves.toMatchObject({
    ok: false,
    outcome: { status },
  });
  expect(store.finishActionExecution).toHaveBeenCalledWith(expect.objectContaining({ status }));
  expect(store.markActionRequestExecuted).toHaveBeenCalledWith("request");
  expect(store.markActionRequestFailed).not.toHaveBeenCalled();
});

it("executes a registered adapter and persists its actual outcome", async () => {
  approved("test_real_adapter");
  const adapter = vi.fn().mockResolvedValue({ externalRef: "provider-receipt", result: { delivered: true } });
  registerActionAdapter("test_real_adapter", adapter);
  await expect(executeApprovedActionRequest("org", "request")).resolves.toMatchObject({ ok: true, outcome: { externalRef: "provider-receipt" } });
  expect(adapter).toHaveBeenCalledOnce();
  expect(store.finishActionExecution).toHaveBeenCalledWith(expect.objectContaining({ status: "succeeded", externalRef: "provider-receipt" }));
  expect(store.markActionRequestExecuted).toHaveBeenCalledWith("request");
});

it("uses the pack fallback only when no exact adapter is registered", async () => {
  approved("installed_pack_action");
  const fallback = vi.fn().mockResolvedValue({ result: { changed: true } });
  const resolver = vi.fn().mockResolvedValue(fallback);
  const unregister = registerFallbackActionAdapterResolver(resolver);
  try {
    await expect(executeApprovedActionRequest("org", "request")).resolves.toMatchObject({
      ok: true,
      outcome: { result: { changed: true } },
    });
    expect(resolver).toHaveBeenCalledOnce();
    expect(fallback).toHaveBeenCalledOnce();
  } finally {
    unregister();
  }
});
