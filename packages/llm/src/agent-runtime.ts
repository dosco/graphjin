import type {
  AgentBackend,
  AgentBackendId,
} from "./agent-backend";
import { AxBackend, type AxBackendConfig } from "./agent-backends/ax";

export type { AxBackendConfig };
export { registerServeToolBridge, serveSocket, type ServeToolBridge } from "./agent-backends/harness-turn";

/** Pure runtime factory for the agent runner process. */
export function makeAgentBackend(cfg: {
  id: AgentBackendId;
  ax?: AxBackendConfig;
}): AgentBackend {
  if (cfg.id === "ax") return new AxBackend(cfg.ax);
  throw new Error(`Unsupported agent backend: ${String(cfg.id)}`);
}
