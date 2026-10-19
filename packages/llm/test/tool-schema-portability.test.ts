import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { describe, expect, it } from "vitest";
import type { NekoMcpServer } from "../src/mcp-server";
import { portableSchemaIssues } from "../src/tool-schema-portability";
import { buildAskUserQuestionServer } from "../src/work/interaction-server";
import * as tools from "../src/work/tools";

// Tools that predate the portability rule. Each loses structure on some
// providers. Remove an entry when its schema is fixed.
const KNOWN_UNPORTABLE = new Set([
  "neko_user_manager.request_data_access_change",
  "neko_source_config_manager.request_source_config_change",
  "neko_records.find_records",
  "neko_skills.create_skill",
]);

function staticServers(): NekoMcpServer[] {
  const controlPlane = {} as never;
  const emit = () => {};
  const base = { orgId: "org", runId: "run", emit, controlPlane };
  const memory = { orgId: "org", runId: "run" } as never;
  return [
    tools.buildRenderCardsServer(emit),
    tools.buildUserManagerServer(base),
    tools.buildChannelManagerServer(base),
    tools.buildDataSourceManagerServer(base),
    tools.buildSourceConfigManagerServer(base),
    tools.buildAuditViewerServer(base),
    tools.buildGraphjinAgentServer(base),
    tools.buildRecordsReadServer(base),
    tools.buildWorkMemoryServer(memory, { controlPlane }),
    tools.buildLibraryServer(memory, { controlPlane }),
    tools.buildSkillBuilderServer("/tmp/openneko-portability-skills"),
    buildAskUserQuestionServer({ runId: "run", wantsCards: true, emit }),
  ];
}

describe("tool schema portability", () => {
  it("advertises every tool with structure that all model providers keep", async () => {
    const unportable: Record<string, string[]> = {};
    for (const server of staticServers()) {
      const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
      await (server as Extract<NekoMcpServer, { type: "sdk" }>).instance.connect(serverTransport);
      const client = new Client({ name: "portability-test", version: "1.0.0" });
      await client.connect(clientTransport);
      for (const tool of (await client.listTools()).tools) {
        const issues = portableSchemaIssues(tool.inputSchema);
        const name = `${server.name}.${tool.name}`;
        if (issues.length > 0 && !KNOWN_UNPORTABLE.has(name)) unportable[name] = issues;
        if (issues.length === 0) expect(KNOWN_UNPORTABLE.has(name), `${name} is portable now; remove it from the list`).toBe(false);
      }
      await client.close();
    }
    expect(unportable).toEqual({});
  });

  it("reports the constructs that providers drop or reject", () => {
    expect(portableSchemaIssues({
      type: "object",
      properties: {
        open: { type: "object", additionalProperties: {} },
        union: { anyOf: [{ type: "string" }, { type: "number" }] },
        fixed: { type: "string", const: "v1" },
        list: { type: "array" },
        mixed: { type: ["string", "null"] },
      },
    })).toEqual([
      "$.open: object declares no properties",
      "$.open: allows undeclared properties",
      "$.union: uses anyOf",
      "$.fixed: uses const",
      "$.list: array declares no items",
      "$.mixed: uses a type list",
    ]);
  });
});
