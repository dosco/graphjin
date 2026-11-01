import { afterEach, describe, expect, it, vi } from "vitest";
import { buildLlm } from "../src/llm";

afterEach(() => vi.unstubAllGlobals());

describe("Anthropic request temperature", () => {
  it.each([
    ["claude-sonnet-5-5", {}, undefined],
    ["claude-sonnet-5-5", { temperature: 0 }, undefined],
    ["claude-sonnet-5", { temperature: 0.5 }, 0.5],
  ])("uses temperature only when positive for %s", async (model, config, expected) => {
    let requestBody: Record<string, unknown> | undefined;
    vi.stubGlobal("fetch", vi.fn(async (_url: string, init: RequestInit) => {
      requestBody = JSON.parse(String(init.body));
      return new Response(JSON.stringify({
        id: "msg_test",
        type: "message",
        role: "assistant",
        content: [{ type: "text", text: "READY" }],
        model,
        stop_reason: "end_turn",
        stop_sequence: null,
        usage: { input_tokens: 1, output_tokens: 1 },
      }), { status: 200, headers: { "content-type": "application/json" } });
    }));

    const llm = await buildLlm(undefined, {
      scope: "primary",
      provider: "anthropic",
      model,
      enabled: true,
      config,
      secrets: { apiKey: "test-key" },
    });
    await llm.chat({ chatPrompt: [{ role: "user", content: "Reply with READY only." }] });

    expect(requestBody?.model).toBe(model);
    if (expected === undefined) {
      expect(requestBody).not.toHaveProperty("temperature");
    } else {
      expect(requestBody?.temperature).toBe(expected);
    }
  });
});
