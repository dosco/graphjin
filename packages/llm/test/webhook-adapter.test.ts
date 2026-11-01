import { afterEach, describe, expect, it, vi } from "vitest";
import {
  webhookAdapter,
  WebhookAdapterError,
} from "../src/workflows/adapters/webhook";
import type { ActionRequestRecord } from "../src/workflows/action-store";

function fakeRequest(
  payload: Record<string, unknown>,
  target: string | null = null,
): ActionRequestRecord {
  return {
    id: "req-1",
    orgId: "org-1",
    workflowRunId: null,
    triggeredByObservationId: null,
    policyId: null,
    scope: "external",
    kind: "send_webhook",
    target,
    payload,
    riskLevel: "medium",
    status: "approved",
    summary: "test",
    requestedByRunId: null,
    approvedByUserId: null,
    approvedAt: new Date(),
    rejectionReason: null,
    createdAt: new Date(),
    updatedAt: new Date(),
  };
}

describe("webhookAdapter", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("POSTs JSON body by default and captures status + body in result", async () => {
    const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response('{"ok":true}', {
        status: 200,
        headers: { "Content-Type": "application/json", "x-request-id": "rid-42" },
      }),
    );
    const outcome = await webhookAdapter({
      request: fakeRequest({
        url: "https://hooks.example.com/abc",
        body: { hello: "world" },
      }),
    });
    expect(spy).toHaveBeenCalledTimes(1);
    const [url, init] = spy.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("https://hooks.example.com/abc");
    expect(init.method).toBe("POST");
    expect((init.headers as Record<string, string>)["Content-Type"]).toBe(
      "application/json",
    );
    expect(init.body).toBe('{"hello":"world"}');
    expect(outcome.externalRef).toBe("rid-42");
    expect((outcome.result as { status: number }).status).toBe(200);
    expect((outcome.result as { body: string }).body).toContain("ok");
  });

  it("falls back to request.target when payload.url is missing", async () => {
    const spy = vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(null, { status: 204 }),
    );
    await webhookAdapter({
      request: fakeRequest({}, "https://hooks.example.com/from-target"),
    });
    expect(spy.mock.calls[0][0]).toBe("https://hooks.example.com/from-target");
  });

  it("throws WebhookAdapterError when neither payload.url nor target is set", async () => {
    await expect(
      webhookAdapter({ request: fakeRequest({}) }),
    ).rejects.toBeInstanceOf(WebhookAdapterError);
  });

  it("throws on non-2xx responses with status + body excerpt", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response("nope", { status: 500, statusText: "Internal Server Error" }),
    );
    await expect(
      webhookAdapter({
        request: fakeRequest({ url: "https://hooks.example.com/fail" }),
      }),
    ).rejects.toThrow(/500.*nope/);
  });

  it("rejects disallowed HTTP methods", async () => {
    vi.spyOn(globalThis, "fetch").mockResolvedValue(new Response(""));
    await expect(
      webhookAdapter({
        request: fakeRequest({
          url: "https://hooks.example.com/x",
          method: "TRACE",
        }),
      }),
    ).rejects.toThrow(/method "TRACE"/);
  });

  it("translates timeout into a clear error message", async () => {
    vi.spyOn(globalThis, "fetch").mockImplementation(
      ({} as unknown as typeof fetch) ||
        (async (_input, init) => {
          await new Promise((_resolve, reject) => {
            init?.signal?.addEventListener("abort", () => {
              const err = new Error("aborted");
              err.name = "AbortError";
              reject(err);
            });
          });
          return new Response("");
        }),
    );
    // Cleaner: mock fetch to reject with AbortError immediately when signal aborts.
    (globalThis.fetch as ReturnType<typeof vi.fn>).mockImplementation(
      async (_input: unknown, init: RequestInit) => {
        return await new Promise((_resolve, reject) => {
          init.signal?.addEventListener("abort", () => {
            const err = new Error("aborted");
            err.name = "AbortError";
            reject(err);
          });
        });
      },
    );
    await expect(
      webhookAdapter({
        request: fakeRequest({
          url: "https://hooks.example.com/slow",
          timeout_ms: 30,
        }),
      }),
    ).rejects.toThrow(/timed out after 30ms/);
  });

  it("caps captured body at 4KB", async () => {
    const huge = "x".repeat(10_000);
    vi.spyOn(globalThis, "fetch").mockResolvedValue(
      new Response(huge, { status: 200 }),
    );
    const outcome = await webhookAdapter({
      request: fakeRequest({ url: "https://hooks.example.com/big" }),
    });
    const body = (outcome.result as { body: string }).body;
    expect(body.length).toBeLessThan(huge.length);
    expect(body).toContain("truncated");
  });
});
