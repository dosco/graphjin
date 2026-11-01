import { existsSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";

export interface AgentRuntimeContract {
  mcpBridgePath: string;
}

interface ConfigureAgentRuntimeOptions {
  entryUrl?: string;
  env?: NodeJS.ProcessEnv;
  pathExists?: (candidate: string) => boolean;
}

export function resolveMcpBridgePath(
  entryUrl = import.meta.url,
  pathExists: (candidate: string) => boolean = existsSync,
): string {
  const entryDir = dirname(fileURLToPath(entryUrl));
  const candidates = [
    // Standalone agent image: agent-entry.js and mcp-bridge.js are siblings.
    resolve(entryDir, "mcp-bridge.js"),
    // Compiled worker layout.
    resolve(entryDir, "..", "..", "dist", "agent-sandbox", "mcp-bridge.js"),
    // Source/tsx development layout.
    resolve(entryDir, "mcp-bridge.ts"),
  ];
  return candidates.find(pathExists) ?? candidates[0]!;
}

export function configureAgentRuntime(
  options: ConfigureAgentRuntimeOptions = {},
): AgentRuntimeContract {
  const env = options.env ?? process.env;
  const pathExists = options.pathExists ?? existsSync;
  const configuredBridge = env.OPENNEKO_MCP_BRIDGE?.trim();
  const mcpBridgePath = configuredBridge
    ? resolve(configuredBridge)
    : resolveMcpBridgePath(options.entryUrl, pathExists);
  if (!pathExists(mcpBridgePath)) {
    throw new Error(
      `agent runtime contract invalid: MCP bridge not found at ${mcpBridgePath}`,
    );
  }
  env.OPENNEKO_MCP_BRIDGE = mcpBridgePath;
  return { mcpBridgePath };
}
