import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { describe, expect, it, vi } from "vitest";
import {
  ASK_USER_TOOL_NAME,
  buildAskUserQuestionServer,
} from "../src/work/interaction-server";

describe("Work-chat AskUserQuestion MCP server", () => {
  it("emits a deterministic A2UI form followed by needs_input", async () => {
    const emit = vi.fn(async () => {});
    const server = buildAskUserQuestionServer({
      runId: "run-1",
      wantsCards: true,
      emit,
    });
    const [clientTransport, serverTransport] =
      InMemoryTransport.createLinkedPair();
    await server.instance.connect(serverTransport);
    const client = new Client({ name: "ask-user-test", version: "1.0.0" });
    await client.connect(clientTransport);
    try {
      const result = await client.callTool({
        name: ASK_USER_TOOL_NAME,
        arguments: {
          reason: "The destination changes freight and delivery timing.",
          questions: [
            {
              header: "Destination",
              question: "Where should the order be delivered?",
            },
            {
              header: "Terms",
              question: "Which shipping terms should I use?",
              options: [
                { label: "EXW", description: "Buyer arranges freight" },
                { label: "CIF", description: "Seller includes freight" },
              ],
            },
          ],
        },
      });

      expect(result.isError).not.toBe(true);
      expect(emit).toHaveBeenCalledTimes(2);
      const surface = emit.mock.calls[0]?.[0];
      expect(surface).toMatchObject({
        type: "surface",
        messages: [
          {
            version: "v1.0",
            createSurface: {
              surfaceId: "clarification-run-1",
              catalogId: "urn:openneko:catalog:work:v2",
              dataModel: { answers: { q1: "", q2: [] } },
            },
          },
        ],
      });
      const components = (
        surface as {
          messages: Array<{
            createSurface: { components: Array<Record<string, unknown>> };
          }>;
        }
      ).messages[0].createSurface.components;
      expect(components).toEqual(
        expect.arrayContaining([
          expect.objectContaining({ id: "q1-input", component: "TextField" }),
          expect.objectContaining({ id: "q2-input", component: "ChoicePicker" }),
          expect.objectContaining({ id: "submit", component: "Button" }),
        ]),
      );
      expect(emit.mock.calls[1]?.[0]).toMatchObject({
        type: "needs_input",
        surfaceId: "clarification-run-1",
        questions: [
          { id: "q1", question: "Where should the order be delivered?" },
          { id: "q2", question: "Which shipping terms should I use?" },
        ],
      });
    } finally {
      await client.close();
    }
  });

  it("rejects empty or over-broad question batches", async () => {
    const server = buildAskUserQuestionServer({
      runId: "run-2",
      wantsCards: false,
      emit: async () => {},
    });
    const [clientTransport, serverTransport] =
      InMemoryTransport.createLinkedPair();
    await server.instance.connect(serverTransport);
    const client = new Client({ name: "ask-user-test", version: "1.0.0" });
    await client.connect(clientTransport);
    try {
      const empty = await client.callTool({
        name: ASK_USER_TOOL_NAME,
        arguments: { questions: [] },
      });
      expect(empty.isError).toBe(true);
      const tooMany = await client.callTool({
        name: ASK_USER_TOOL_NAME,
        arguments: {
          questions: ["one", "two", "three", "four"].map((question) => ({
            question,
          })),
        },
      });
      expect(tooMany.isError).toBe(true);
    } finally {
      await client.close();
    }
  });
});
