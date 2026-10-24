import { McpServer } from "@modelcontextprotocol/sdk/server/mcp.js";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import type { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
  type CallToolResult,
  type Tool,
} from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";

type ToolShape = z.ZodRawShape;

export type NekoMcpToolDefinition<Schema extends ToolShape = ToolShape> = {
  name: string;
  description: string;
  inputSchema: Schema;
  handler: (
    args: z.infer<z.ZodObject<Schema>>,
    extra: unknown,
  ) => Promise<CallToolResult>;
};

export type NekoMcpServer = {
  type: "sdk";
  name: string;
  instance: { connect(transport: Transport): Promise<void> };
};

/**
 * Define an OpenNeko MCP tool without coupling its schema or handler to an
 * agent vendor SDK. The same definition is mounted by the backend through the
 * sandbox's stdio MCP bridge.
 */
export function defineMcpTool<Schema extends ToolShape>(
  name: string,
  description: string,
  inputSchema: Schema,
  handler: NekoMcpToolDefinition<Schema>["handler"],
): NekoMcpToolDefinition<Schema> {
  return { name, description, inputSchema, handler };
}

/** Build a standard MCP server around OpenNeko's logical tool definitions. */
export function createMcpServer(opts: {
  name: string;
  version?: string;
  tools?: Array<NekoMcpToolDefinition<any>>;
}): NekoMcpServer {
  const instance = new McpServer({
    name: opts.name,
    version: opts.version ?? "1.0.0",
  });
  for (const definition of opts.tools ?? []) {
    instance.registerTool(
      definition.name,
      {
        description: definition.description,
        inputSchema: definition.inputSchema,
      },
      (args: Record<string, unknown>, extra: unknown) =>
        definition.handler(args, extra),
    );
  }
  return { type: "sdk", name: opts.name, instance };
}

/**
 * Build an MCP server whose caller-visible tool catalog comes from another
 * trusted service. Tool descriptors and calls pass through without flattening
 * their JSON schemas into a generic catch-all tool.
 */
export function createDynamicMcpServer(opts: {
  name: string;
  version?: string;
  listTools: () => Promise<Tool[]>;
  callTool: (input: {
    name: string;
    arguments?: Record<string, unknown>;
  }) => Promise<CallToolResult>;
}): NekoMcpServer {
  const instance = new Server(
    { name: opts.name, version: opts.version ?? "1.0.0" },
    { capabilities: { tools: {} } },
  );
  instance.setRequestHandler(ListToolsRequestSchema, async () => ({
    tools: await opts.listTools(),
  }));
  instance.setRequestHandler(CallToolRequestSchema, async (request) =>
    opts.callTool({
      name: request.params.name,
      ...(request.params.arguments
        ? { arguments: request.params.arguments }
        : {}),
    }),
  );
  return { type: "sdk", name: opts.name, instance };
}
