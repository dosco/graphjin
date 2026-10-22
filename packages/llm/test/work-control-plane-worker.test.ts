import { describe, expect, it, vi } from "vitest";
import { createActionRequestViaWorker } from "../src/work/control-plane";

describe("worker-owned action request preflight", () => {
  const input = {
    orgId: "org-test",
    scope: "internal" as const,
    kind: "user_admin",
    payload: { email: "new@example.com" },
    status: "pending_approval" as const,
  };

  it("returns the prepared worker request id and post-preflight status", async () => {
    const fetchImpl = vi.fn(async () =>
      new Response(JSON.stringify({ id: "action-prepared", status: "approved" }), {
        status: 200,
        headers: { "content-type": "application/json" },
      }),
    );

    await expect(
      createActionRequestViaWorker("http://worker:4100/", input, fetchImpl),
    ).resolves.toEqual({ id: "action-prepared", status: "approved" });
    expect(fetchImpl).toHaveBeenCalledWith(
      "http://worker:4100/admin/action-requests/create",
      expect.objectContaining({
        method: "POST",
        body: JSON.stringify(input),
      }),
    );
  });

  it("surfaces worker preflight failures", async () => {
    const fetchImpl = vi.fn(async () =>
      new Response(JSON.stringify({ error: "catalog unavailable" }), {
        status: 500,
        headers: { "content-type": "application/json" },
      }),
    );

    await expect(
      createActionRequestViaWorker("http://worker:4100", input, fetchImpl),
    ).rejects.toThrow("catalog unavailable");
  });
});
