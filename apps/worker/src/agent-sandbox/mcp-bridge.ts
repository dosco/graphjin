import { Client } from "@modelcontextprotocol/sdk/client/index.js";
import { InMemoryTransport } from "@modelcontextprotocol/sdk/inMemory.js";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import type { Transport } from "@modelcontextprotocol/sdk/shared/transport.js";
import {
  CallToolRequestSchema,
  ListToolsRequestSchema,
} from "@modelcontextprotocol/sdk/types.js";
import type { AgentEvent } from "@neko/llm";
import {
  buildAskUserQuestionServer,
  buildAuditViewerServer,
  buildChannelManagerServer,
  buildDataSourceManagerServer,
  buildGraphjinAgentServer,
  buildGraphjinMcpServer,
  buildLibraryServer,
  buildPluginActionServer,
  buildPackActionServer,
  buildPluginManagerServer,
  buildRecordsReadServer,
  buildRenderCardsServer,
  buildSkillBuilderServer,
  buildSourceConfigManagerServer,
  buildUserManagerServer,
  buildWorkMemoryServer,
  buildBotServer,
  buildSkillSearchServer,
  buildRuleBuilderServer,
  buildWorkflowActionServer,
  buildWorkflowBuilderServer,
  buildWorkflowOutputServer,
  GRAPHJIN_TOOL_POLICY_ENV,
  parseGraphjinMcpToolPolicy,
  type GraphjinMcpToolPolicy,
  type PluginActionDescriptor,
  type PackActionDescriptor,
} from "@neko/llm/sandbox-runtime";
import { BrokerControlPlane, postAgentEvents } from "./broker-client";

// The broker waits up to 180s for GraphJin tools such as the GraphJin agent.
const TOOL_CALL_TIMEOUT_MS = 180_000;

/**
 * Stdio MCP server for ACP backends (hermes). The agent process can't hand
 * its in-process SDK server instances across a process boundary, so hermes'
 * `session/new` launches THIS script once for all named servers. The bridge
 * rebuilds each logical server in-process and exposes a single multiplexed
 * stdio endpoint bound to the broker control plane. Keeping one Node process
 * instead of one per logical MCP
 * server is material: a normal chat turn mounts roughly ten servers.
 *
 * argv[2] = comma-separated server names (agent-core's mcpServers map keys). Per-run context
 * arrives via env (agent-core's mcpBridgeEnv); broker coords are copied into
 * this clean bridge-child env and are not exposed to the model process.
 */

function requireEnv(name: string): string {
  const value = process.env[name];
  if (!value) throw new Error(`mcp-bridge: missing required env ${name}`);
  return value;
}

export type BridgeServerContext = {
  runKind: "work" | "workflow" | "agent-job";
  orgId: string;
  threadId: string;
  runId: string;
  skillsRoot: string;
  pluginActions: PluginActionDescriptor[];
  packActions: PackActionDescriptor[];
  controlPlane: BrokerControlPlane;
  brokerUrl?: string;
  brokerToken?: string;
  workflowRunId?: string;
  triggeredByObservationId?: string | null;
  recordScope?: { appId: string; objectApiName: string };
  wantsCards?: boolean;
  cardSchema?: string;
  graphjinToolPolicy?: GraphjinMcpToolPolicy;
};

export function buildBridgeServer(
  name: string,
  ctx: BridgeServerContext,
): { instance: { connect: (t: Transport) => Promise<void> } } {
  const { orgId, threadId, runId, skillsRoot, pluginActions, controlPlane } = ctx;
  const emit =
    ctx.brokerUrl && ctx.brokerToken
      ? (event: AgentEvent) =>
          postAgentEvents(ctx.brokerUrl!, ctx.brokerToken!, [event])
      : () => {};
  const common = { orgId, runId, emit, controlPlane };
  switch (name) {
    case "neko_interaction":
      return buildAskUserQuestionServer({
        runId,
        wantsCards: ctx.wantsCards ?? false,
        emit,
      });
    case "neko_ui":
      return buildRenderCardsServer(emit, { rich: ctx.cardSchema === "rich" });
    case "neko_graphjin":
      return buildGraphjinMcpServer({
        orgId,
        ...(ctx.runKind === "agent-job" ? {} : { runId }),
        controlPlane,
        ...(ctx.graphjinToolPolicy
          ? { toolPolicy: ctx.graphjinToolPolicy }
          : {}),
      });
    case "neko_graphjin_agent":
      return buildGraphjinAgentServer({ orgId, runId, controlPlane });
    case "neko_skills":
      return buildSkillBuilderServer(skillsRoot);
    case "neko_memory":
      return buildWorkMemoryServer(
        { orgId, threadId, runId },
        {
          controlPlane,
          exposeSave: process.env.OPENNEKO_MCP_MEMORY_READ_ONLY !== "1",
        },
      );
    case "neko_guides":
      return buildSkillSearchServer({ orgId, runId }, { controlPlane });
    case "neko_bot":
      return buildBotServer({ orgId, threadId }, { controlPlane });
    case "neko_library":
      return buildLibraryServer({ orgId, threadId, runId }, { controlPlane });
    case "neko_records":
      return buildRecordsReadServer({
        orgId,
        runId,
        controlPlane,
        ...(ctx.recordScope ? { scope: ctx.recordScope } : {}),
      });
    case "neko_workflow_builder":
      return buildWorkflowBuilderServer({
        orgId,
        createdByThreadId: threadId,
        createdByRunId: runId,
        emit,
        controlPlane,
      });
    case "neko_workflow_output":
      if (!ctx.workflowRunId) {
        throw new Error("mcp-bridge: neko_workflow_output requires workflowRunId");
      }
      return buildWorkflowOutputServer({
        orgId,
        workflowRunId: ctx.workflowRunId,
        workRunId: runId,
        emit,
        controlPlane,
      });
    case "neko_action":
      if (!ctx.workflowRunId) {
        throw new Error("mcp-bridge: neko_action requires workflowRunId");
      }
      return buildWorkflowActionServer({
        orgId,
        workflowRunId: ctx.workflowRunId,
        workRunId: runId,
        triggeredByObservationId: ctx.triggeredByObservationId ?? null,
        emit,
        controlPlane,
      });
    case "neko_rule_builder":
      return buildRuleBuilderServer({
        orgId,
        createdByThreadId: threadId,
        createdByRunId: runId,
        emit,
        controlPlane,
      });
    case "neko_plugin_manager":
      return buildPluginManagerServer(common);
    case "neko_user_manager":
      return buildUserManagerServer(common);
    case "neko_channel_manager":
      return buildChannelManagerServer(common);
    case "neko_data_source_manager":
      return buildDataSourceManagerServer(common);
    case "neko_source_config_manager":
      return buildSourceConfigManagerServer(common);
    case "neko_audit":
      return buildAuditViewerServer({ orgId, runId, controlPlane });
    case "neko_plugin_actions": {
      const server = buildPluginActionServer({
        orgId,
        threadId,
        runId,
        descriptors: pluginActions,
        emit,
        controlPlane,
      });
      if (!server) throw new Error("mcp-bridge: no plugin actions in this run");
      return server;
    }
    case "neko_pack_actions": {
      const server = buildPackActionServer({
        orgId,
        threadId,
        runId,
        descriptors: ctx.packActions,
        emit,
        controlPlane,
      });
      if (!server) throw new Error("mcp-bridge: no pack actions in this run");
      return server;
    }
    default:
      throw new Error(`mcp-bridge: unknown server ${name}`);
  }
}

/**
 * Hermes exposes a tool as `mcp_<server>_<tool>`. Mounting one physical MCP
 * server called `neko` and publishing `<logical-server>_<tool>` therefore
 * preserves the exact model-facing names used before multiplexing:
 *
 *   neko_memory + search -> mcp_neko_memory_search
 */
export function multiplexedToolName(serverName: string, toolName: string): string {
  if (!serverName.startsWith("neko_") || !toolName) {
    throw new Error(`mcp-bridge: cannot multiplex ${serverName}/${toolName}`);
  }
  return `${serverName.slice("neko_".length)}_${toolName}`;
}

/** Build one public MCP server backed by multiple in-process logical servers. */
export async function buildMultiplexedBridgeServer(
  names: readonly string[],
  ctx: BridgeServerContext,
): Promise<{ instance: Server }> {
  if (names.length === 0) throw new Error("mcp-bridge: no servers to multiplex");

  // Logical servers connect in memory at startup. Their tool catalogs resolve
  // on Hermes' first tools/list, so a logical server whose catalog comes from
  // a remote service (neko_graphjin relays GraphJin's live tools/list) cannot
  // take the other servers down with it.
  const logicalClients: Array<{ name: string; client: Client }> = [];
  for (const name of names) {
    const logical = buildBridgeServer(name, ctx);
    const [clientTransport, serverTransport] = InMemoryTransport.createLinkedPair();
    await logical.instance.connect(serverTransport);
    const client = new Client({
      name: `openneko-mcp-multiplexer-${name}`,
      version: "1.0.0",
    });
    await client.connect(clientTransport);
    logicalClients.push({ name, client });
  }

  const routes = new Map<
    string,
    { client: Client; originalToolName: string }
  >();
  const listPublicTools = async (): Promise<
    Array<Record<string, unknown> & { name: string }>
  > => {
    const publicTools: Array<Record<string, unknown> & { name: string }> = [];
    const nextRoutes = new Map<
      string,
      { client: Client; originalToolName: string }
    >();
    for (const { name, client } of logicalClients) {
      let listed: Awaited<ReturnType<Client["listTools"]>>;
      try {
        listed = await client.listTools();
      } catch (err) {
        console.error(
          `[mcp-bridge] ${name} tools/list failed; its tools are unavailable until the next tools/list: ${
            err instanceof Error ? err.message : String(err)
          }`,
        );
        continue;
      }
      for (const tool of listed.tools) {
        const publicName = multiplexedToolName(name, tool.name);
        if (nextRoutes.has(publicName)) {
          throw new Error(`mcp-bridge: duplicate multiplexed tool ${publicName}`);
        }
        nextRoutes.set(publicName, { client, originalToolName: tool.name });
        publicTools.push({ ...tool, name: publicName });
      }
    }
    routes.clear();
    for (const [publicName, route] of nextRoutes) routes.set(publicName, route);
    return publicTools;
  };

  const instance = new Server(
    { name: "openneko-mcp-multiplexer", version: "1.0.0" },
    { capabilities: { tools: {} } },
  );
  instance.setRequestHandler(ListToolsRequestSchema, async () => ({
    tools: await listPublicTools(),
  }));
  instance.setRequestHandler(CallToolRequestSchema, async (request) => {
    let route = routes.get(request.params.name);
    if (!route) {
      await listPublicTools();
      route = routes.get(request.params.name);
    }
    if (!route) throw new Error(`mcp-bridge: unknown tool ${request.params.name}`);
    return route.client.callTool(
      { name: route.originalToolName, arguments: request.params.arguments },
      undefined,
      { timeout: TOOL_CALL_TIMEOUT_MS },
    );
  });
  return { instance };
}

/**
 * The egress proxy admits a freshly spawned process with a lag — its first
 * dials get ECONNREFUSED even with an allow rule in place. Poll until the
 * broker answers anything at all (any HTTP status counts) before serving, so
 * the first real tool call rides a warmed path.
 */
async function warmUpBroker(baseUrl: string, name: string): Promise<void> {
  const trail: string[] = [];
  const deadline = Date.now() + 8_000;
  let attempts = 0;
  for (;;) {
    attempts += 1;
    try {
      const res = await fetch(new URL("/v1/memory/search", baseUrl), {
        method: "POST",
        body: "{}",
      });
      trail.push(`attempt ${attempts}: HTTP ${res.status}`);
      break;
    } catch (err) {
      const cause = (err as { cause?: { code?: string } }).cause?.code;
      trail.push(`attempt ${attempts}: ${cause ?? (err as Error).message}`);
      if (Date.now() > deadline) break; // serve anyway; per-call retries remain
      await new Promise((r) => setTimeout(r, 250));
    }
  }
  // Startup diagnostics, readable mid-run via `sandbox exec` — bridge stderr
  // is swallowed by hermes, so a file is the only visible channel.
  try {
    const envPick = Object.fromEntries(
      Object.entries(process.env)
        .filter(([k]) => /OPENNEKO_|PROXY|proxy|NODE_USE/.test(k) && !/TOKEN|SECRET|KEY/.test(k)),
    );
    const { writeFileSync } = await import("node:fs");
    writeFileSync(
      `/tmp/bridge-${name}.log`,
      JSON.stringify({ baseUrl, trail, env: envPick }, null, 1),
      { mode: 0o600 },
    );
  } catch {
    /* diagnostics only */
  }
}

async function main(): Promise<void> {
  const names = [...new Set((process.argv[2] ?? "").split(",").filter(Boolean))];
  if (names.length === 0) throw new Error("mcp-bridge: missing server-name argument");
  const brokerUrl = requireEnv("OPENNEKO_BROKER_URL");
  const brokerToken = requireEnv("OPENNEKO_BROKER_TOKEN");
  await warmUpBroker(brokerUrl, "multiplexed");
  const controlPlane = new BrokerControlPlane(
    brokerUrl,
    brokerToken,
  );
  const server = await buildMultiplexedBridgeServer(names, {
    runKind:
      process.env.OPENNEKO_MCP_MODE === "workflow"
        ? "workflow"
        : process.env.OPENNEKO_MCP_MODE === "agent-job"
          ? "agent-job"
          : "work",
    orgId: requireEnv("OPENNEKO_MCP_ORG_ID"),
    threadId: requireEnv("OPENNEKO_MCP_THREAD_ID"),
    runId: requireEnv("OPENNEKO_MCP_RUN_ID"),
    skillsRoot: requireEnv("OPENNEKO_MCP_SKILLS_ROOT"),
    pluginActions: JSON.parse(
      process.env.OPENNEKO_MCP_PLUGIN_ACTIONS ?? "[]",
    ) as PluginActionDescriptor[],
    packActions: JSON.parse(
      process.env.OPENNEKO_MCP_PACK_ACTIONS ?? "[]",
    ) as PackActionDescriptor[],
    controlPlane,
    brokerUrl,
    brokerToken,
    workflowRunId: process.env.OPENNEKO_MCP_WORKFLOW_RUN_ID,
    triggeredByObservationId:
      process.env.OPENNEKO_MCP_TRIGGERED_BY_OBSERVATION_ID ?? null,
    recordScope: process.env.OPENNEKO_MCP_RECORD_SCOPE
      ? (JSON.parse(process.env.OPENNEKO_MCP_RECORD_SCOPE) as {
          appId: string;
          objectApiName: string;
        })
      : undefined,
    wantsCards: process.env.OPENNEKO_MCP_WANTS_CARDS === "1",
    cardSchema: process.env.OPENNEKO_MCP_CARD_SCHEMA,
    graphjinToolPolicy: parseGraphjinMcpToolPolicy(
      process.env[GRAPHJIN_TOOL_POLICY_ENV],
    ),
  });
  await server.instance.connect(new StdioServerTransport());
}

const invokedDirectly =
  process.argv[1]?.endsWith("mcp-bridge.ts") ||
  process.argv[1]?.endsWith("mcp-bridge.js");
if (invokedDirectly) {
  main().catch(async (err: unknown) => {
    const message = err instanceof Error ? err.message : String(err);
    console.error("[mcp-bridge] fatal:", message);
    // Hermes stores MCP child stderr inside the ephemeral sandbox. Persist a
    // sanitized startup failure through the normal run event sink as well, so
    // a lost tool surface is diagnosable after teardown instead of silently
    // degrading the agent to filesystem/terminal tools.
    const brokerUrl = process.env.OPENNEKO_BROKER_URL;
    const brokerToken = process.env.OPENNEKO_BROKER_TOKEN;
    if (brokerUrl && brokerToken) {
      try {
        await postAgentEvents(brokerUrl, brokerToken, [
          {
            type: "error",
            message: `Agent tool bridge failed to start: ${message}`,
          },
        ]);
      } catch {
        // The original stderr message remains the last-resort diagnostic when
        // the broker itself is unreachable.
      }
    }
    process.exit(1);
  });
}
