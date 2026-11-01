import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { describe, expect, it, vi } from "vitest";
import { buildRenderCardsServer, buildSourceConfigManagerServer } from "../src/work/tools";
import type { NekoMcpServer } from "../src/mcp-server";

async function withServer(
  server: NekoMcpServer,
  run: (client: Client) => Promise<void>,
) {
  const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
  await (server as Extract<NekoMcpServer, { type: "sdk" }>).instance.connect(serverTransport);
  const client = new Client({ name: "render-server-test", version: "1.0.0" });
  await client.connect(clientTransport);
  try {
    await run(client);
  } finally {
    await client.close();
  }
}

const text = (result: Awaited<ReturnType<Client["callTool"]>>) =>
  (result.content as Array<{ text: string }>)[0]?.text;

describe("brokered neko_ui render MCP server", () => {
  it("emits one Answer surface for a valid answer", async () => {
    const emit = vi.fn(async () => {});
    await withServer(buildRenderCardsServer(emit), async (client) => {
      const listed = await client.listTools();
      expect(listed.tools.map((tool) => tool.name)).toEqual(["render_cards"]);
      const result = await client.callTool({
        name: "render_cards",
        arguments: { title: "Orders", keyFigures: ["Orders: 42"] },
      });
      expect(result.isError).toBeFalsy();
      expect(text(result)).toBe("Cards shown.");
      expect(emit).toHaveBeenCalledTimes(1);
      expect(emit.mock.calls[0]?.[0]).toMatchObject({
        type: "surface",
        messages: [{
          version: "v1.0",
          createSurface: {
            surfaceId: expect.stringMatching(/^answer-/),
            components: [
              { id: "root", component: "Answer", title: "Orders", children: ["keyFigures"] },
              { id: "keyFigures", component: "KeyFigures", items: [{ label: "Orders", value: "42" }] },
            ],
          },
        }],
      });
    });
  });

  it("returns one corrective line per problem and emits nothing", async () => {
    const emit = vi.fn(async () => {});
    await withServer(buildRenderCardsServer(emit), async (client) => {
      const result = await client.callTool({
        name: "render_cards",
        arguments: { title: "Orders", table: "A | B\n1" },
      });
      expect(result.isError).toBe(true);
      expect(text(result)).toBe(
        "Cards not shown. Fix and call again:\n- table row 1 needs 2 cells, one per column.",
      );
      expect(emit).not.toHaveBeenCalled();
    });
  });

  it("rejects the previous message envelope before any surface is emitted", async () => {
    const emit = vi.fn(async () => {});
    await withServer(buildRenderCardsServer(emit), async (client) => {
      const result = await client.callTool({
        name: "render_cards",
        arguments: { messages: [{ version: "v1.0", createSurface: "{}" }] },
      });
      expect(result.isError).toBe(true);
      expect(emit).not.toHaveBeenCalled();
    });
  });
});

describe("present_source_form", () => {
  it("emits the canonical source form with the agent's prefill", async () => {
    const emit = vi.fn(async () => {});
    const server = buildSourceConfigManagerServer({
      orgId: "org",
      runId: "run",
      emit,
      controlPlane: {} as never,
    });
    await withServer(server, async (client) => {
      const result = await client.callTool({
        name: "present_source_form",
        arguments: { name: "warehouse", kind: "database", host: "db.internal", secretRef: "warehouse-password" },
      });
      expect(text(result)).toBe("Form shown.");
    });
    const surface = (emit.mock.calls[0]?.[0] as { messages: Array<{ createSurface: Record<string, unknown> }> })
      .messages[0]!.createSurface;
    expect(surface.surfaceId).toMatch(/^source-form-/);
    expect(surface.dataModel).toMatchObject({
      form: {
        name: "warehouse",
        kind: ["database"],
        host: "db.internal",
        port: 5432,
        secretRef: "warehouse-password",
        backend: ["local"],
      },
    });
    const components = surface.components as Array<{ id: string; component: string }>;
    expect(components.find((component) => component.id === "root")).toMatchObject({ component: "Answer" });
    expect(components.filter((component) => component.component === "Button").map((component) => component.id))
      .toEqual(["submitDatabase", "submitApi", "submitLocalFile", "submitCloudFile"]);
  });
});
