import { describe, expect, it, vi } from "vitest";
import { runValidatedAgentTurn } from "../src/agent-validate-loop";
import type { AgentBackend } from "../src/agent-backend";

function fakeBackend(replies: string[]): {
  backend: AgentBackend;
  prompts: string[];
} {
  const prompts: string[] = [];
  let i = 0;
  const backend = {
    id: "fake",
    capabilities: { mcpTools: false },
    run: vi.fn(async (opts: { prompt: string }) => {
      prompts.push(opts.prompt);
      const finalText = replies[Math.min(i, replies.length - 1)]!;
      i++;
      return { status: "completed" as const, finalText };
    }),
  } as unknown as AgentBackend;
  return { backend, prompts };
}

const runOpts = { prompt: "BASE PROMPT", orgId: "o1", workspace: {} } as never;

describe("runValidatedAgentTurn (GJ2)", () => {
  it("returns on first valid output", async () => {
    const { backend } = fakeBackend(['{"ok":true}']);
    const out = await runValidatedAgentTurn({
      backend,
      run: runOpts,
      label: "t",
      validate: (t) => JSON.parse(t),
    });
    expect(out.value).toEqual({ ok: true });
    expect(out.attempts).toBe(1);
  });

  it("feeds the validation error back and recovers", async () => {
    const { backend, prompts } = fakeBackend(["not json", '{"ok":true}']);
    const out = await runValidatedAgentTurn({
      backend,
      run: runOpts,
      label: "t",
      validate: (t) => JSON.parse(t),
    });
    expect(out.attempts).toBe(2);
    expect(prompts[1]).toContain("BASE PROMPT");
    expect(prompts[1]).toContain("previous-attempt-rejected");
    expect(prompts[1]).toContain("not json");
  });

  it("gives up after maxAttempts with the last error", async () => {
    const { backend } = fakeBackend(["bad"]);
    await expect(
      runValidatedAgentTurn({
        backend,
        run: runOpts,
        label: "t",
        maxAttempts: 2,
        validate: (t) => JSON.parse(t),
      }),
    ).rejects.toThrow(/failed validation after 2 attempt/);
    expect((backend.run as ReturnType<typeof vi.fn>).mock.calls).toHaveLength(2);
  });

  it("throws immediately on backend failure (no retry)", async () => {
    const backend = {
      id: "fake",
      capabilities: { mcpTools: false },
      run: vi.fn(async () => ({ status: "error" as const, finalText: "", error: "boom" })),
    } as unknown as AgentBackend;
    await expect(
      runValidatedAgentTurn({
        backend,
        run: runOpts,
        label: "t",
        validate: (t) => t,
      }),
    ).rejects.toThrow("boom");
    expect((backend.run as ReturnType<typeof vi.fn>).mock.calls).toHaveLength(1);
  });

  it("reports GraphJin authorization failures as data-source errors", async () => {
    const { backend } = fakeBackend([
      "GraphJin query failed: unauthorized: table productcategory is blocked for role anon",
    ]);

    await expect(
      runValidatedAgentTurn({
        backend,
        run: runOpts,
        label: "t",
        validate: (t) => JSON.parse(t),
      }),
    ).rejects.toThrow("Data source authorization failed");
    expect((backend.run as ReturnType<typeof vi.fn>).mock.calls).toHaveLength(1);
  });
});
