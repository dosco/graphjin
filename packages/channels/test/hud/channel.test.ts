import { afterEach, describe, expect, it, vi } from "vitest";
import { HUD_CHANNEL, HUD_PROFILE, deliverHud, signHudBody } from "../../src/index.js";

const env = { HUD_CHANNEL_SECRET: "test-secret", HUD_CHANNEL_WORKSPACE: "test-pack" };

describe("HUD channel", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("declares the generic visual channel contract", () => {
    expect(HUD_CHANNEL.id).toBe("hud");
    expect(HUD_CHANNEL.providerLabel).toBe("HUD surface");
    expect(HUD_CHANNEL.profile).toEqual(HUD_PROFILE);
    expect(HUD_CHANNEL.settings.find((s) => s.key === "HUD_CHANNEL_SECRET")?.autogenerate).toBe(true);
  });

  it("projects and signs an outbound delivery", async () => {
    const request = vi.fn(async (_url: string | URL | Request, init?: RequestInit) => {
      const body = String(init?.body);
      const headers = init?.headers as Record<string, string>;
      expect(headers["x-openneko-hud-signature"]).toBe(`sha256=${signHudBody(body, "test-secret")}`);
      const envelope = JSON.parse(body) as {
        schema: string;
        workspace: string;
        projections: Array<Record<string, unknown>>;
      };
      expect(envelope.schema).toBe("openneko.hud.delivery.v1");
      expect(envelope.workspace).toBe("test-pack");
      expect(envelope.projections[0]).toMatchObject({
        id: "finding-1",
        type: "finding",
        title: "Transfer rate degraded",
        anchor: { kind: "asset", id: "atlas-dawn" },
      });
      return new Response("{}", { status: 202 });
    });

    const result = await deliverHud(
      {
        recipient: { kind: "hud", workspace: "test-pack" },
        env,
        events: [{
          kind: "inform",
          id: "finding-1",
          mood: "watch",
          title: "Transfer rate degraded",
          body: "Rate is below plan.",
          evidence: [{ label: "Asset", ref: "asset:atlas-dawn" }],
        }],
      },
      {
        fetch: request as typeof fetch,
        now: () => new Date("2026-08-08T10:30:00.000Z"),
        randomId: () => "delivery-1",
        channelUrl: "http://example.test",
      },
    );

    expect(result).toEqual({ delivered: true, ref: "delivery-1" });
    expect(request).toHaveBeenCalledOnce();
  });

  it("normalizes decisions and utterances without domain assumptions", () => {
    const decision = HUD_CHANNEL.parseInbound(
      { kind: "decision", decisionRef: "request-7", choice: "approve", operatorId: "op-4", operatorName: "Control room" },
      env,
    );
    expect(decision).toMatchObject({
      intents: [{ kind: "decision", decisionRef: "request-7", choice: "approve" }],
      recipient: { kind: "hud", workspace: "test-pack" },
      sender: { id: "op-4", displayName: "Control room", workspaceId: "test-pack" },
    });

    const utterance = HUD_CHANNEL.parseInbound({ kind: "utterance", text: "Focus the active incident" }, env);
    expect(utterance).toMatchObject({ intents: [{ kind: "utterance", text: "Focus the active incident" }] });
  });

  it("verifies inbound HMAC signatures case-insensitively and fails closed without a secret", () => {
    const body = JSON.stringify({ kind: "decision", decisionRef: "r1", choice: "reject" });
    const verify = HUD_CHANNEL.verifyInbound!;
    const signed = { "X-OpenNeko-HUD-Signature": `sha256=${signHudBody(body, "test-secret")}` };
    expect(verify(signed, body, env)).toBe(true);
    expect(verify({ "x-openneko-hud-signature": "sha256=" + "0".repeat(64) }, body, env)).toBe(false);
    expect(verify(signed, body, {})).toBe(false);
  });

  it("rejects delivery when the secret is absent", async () => {
    await expect(deliverHud(
      {
        recipient: { kind: "hud" },
        env: {},
        events: [{ kind: "converse", id: "m1", role: "assistant", text: "hello" }],
      },
      { fetch: vi.fn() as unknown as typeof fetch },
    )).rejects.toThrow("HUD_CHANNEL_SECRET is required");
  });
});
