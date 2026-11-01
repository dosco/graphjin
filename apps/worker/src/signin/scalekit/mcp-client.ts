// Streamable-HTTP MCP client for the Scalekit workspace server. The SSO setup
// flow opens a session with the deployment's OAuth access token and calls one
// workspace tool (list environments, generate the admin portal link, …).

import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { StreamableHTTPClientTransport } from "@modelcontextprotocol/sdk/client/streamableHttp.js";

export interface McpToolResult {
  content: Array<{ type: string; text?: string; [k: string]: unknown }>;
  isError?: boolean;
  [k: string]: unknown;
}

export interface McpToolClient {
  callTool(name: string, args: Record<string, unknown>): Promise<McpToolResult>;
  close(): Promise<void>;
}

export async function createMcpClient(options: {
  url: string;
  accessToken: string;
}): Promise<McpToolClient> {
  const transport = new StreamableHTTPClientTransport(new URL(options.url), {
    requestInit: {
      headers: { Authorization: `Bearer ${options.accessToken}` },
    },
  });
  const client = new Client({
    name: "openneko-scalekit",
    version: "1.0.0",
  });
  await client.connect(transport);
  return {
    async callTool(name, args) {
      const res = await client.callTool({ name, arguments: args });
      return res as unknown as McpToolResult;
    },
    async close() {
      try {
        await client.close();
      } catch {
        // best-effort
      }
    },
  };
}

export function joinTextContent(result: McpToolResult): string {
  return result.content
    .filter((c) => c.type === "text" && typeof c.text === "string")
    .map((c) => c.text ?? "")
    .join("\n\n");
}
