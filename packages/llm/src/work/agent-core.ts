import type {
  AgentBackend,
  AgentEvent,
  AgentNativeDelegationPolicy,
  AgentRunResult,
  AgentWorkspace,
} from "../agent-backend";
import type { AllowedLibrary } from "../library/staging";
import { buildWorkflowBuilderServer } from "../workflows/builder-server";
import { buildRuleBuilderServer } from "../workflows/rule-builder-server";
import type { AgentControlPlane } from "./control-plane";
import {
  GRAPHJIN_TOOL_POLICY_ENV,
  serializeGraphjinMcpToolPolicy,
  type GraphjinDataPath,
  type GraphjinMcpToolPolicy,
} from "./graphjin-tool-policy";
import { buildAskUserQuestionServer } from "./interaction-server";
import {
  parseAppWorkContext,
  parseRecordWorkContext,
  type WorkDataSurface,
} from "./data-surface";
import {
  buildPluginActionServer,
  buildPackActionServer,
  buildPluginManagerServer,
  buildAuditViewerServer,
  buildChannelManagerServer,
  buildDataSourceManagerServer,
  buildGraphjinAgentServer,
  buildGraphjinMcpServer,
  buildSourceConfigManagerServer,
  buildUserManagerServer,
  buildLibraryServer,
  buildRenderCardsServer,
  buildRecordsReadServer,
  buildSkillBuilderServer,
  buildWorkMemoryServer,
  buildBotServer,
  buildSkillSearchServer,
  type PluginActionDescriptor,
  type PackActionDescriptor,
} from "./tools";

export interface RunAgentBackendInput {
  /** Hermes backend. In the sandbox it is reconstructed from config. */
  backend: AgentBackend;
  prompt: string;
  userMessage: string;
  orgId: string;
  threadId: string;
  runId: string;
  workspace: AgentWorkspace;
  backendState?: Record<string, unknown>;
  /** Trusted host-only reuse boundary. Missing revision disables assigned reuse.
   * Future RBAC must change this revision when ANY effective grant changes. */
  sandboxUser?: { principalId: string; authorizationRevision: string };
  /** Skill names the run's actor holds. Undefined means every skill. */
  allowedSkills?: readonly string[];
  /** Team library files the actor holds. Undefined means the whole team library. */
  allowedLibrary?: AllowedLibrary;
  pluginActions: readonly PluginActionDescriptor[];
  /** Installed pack actions, discovered independently of plugins. */
  packActions?: readonly PackActionDescriptor[];
  /** Mount the GraphJin source-config MCP server for this admin run. */
  sourceConfigEnabled?: boolean;
  /** Mount the bot server: this chat belongs to one of the operator's bots. */
  botEnabled?: boolean;
  /** Selects the isolated data plane for this turn. */
  dataSurface?: WorkDataSurface;
  /** Optional backend-neutral restriction for this run's GraphJin MCP. */
  graphjinToolPolicy?: GraphjinMcpToolPolicy;
  /** "agent" when the org opted in to GraphJin's server-side agent. */
  graphjinDataPath?: GraphjinDataPath;
  /** Optional per-run restriction on the backend's own sub-agent primitive. */
  nativeDelegation?: AgentNativeDelegationPolicy;
  /** In-process on the host; broker-backed inside the agent sandbox. */
  controlPlane: AgentControlPlane;
  /** Whether this channel renders a2ui cards (web). Default true. Gates the
   * brokered neko_ui render server. */
  wantsCards?: boolean;
  /** The answer card schema: "rich" on the web, "core" on other channels. */
  cardSchema?: "rich" | "core";
  emit: (event: AgentEvent) => Promise<void>;
  signal?: AbortSignal;
}

/**
 * The agent loop that is sandbox-runnable: build the MCP tool servers and run
 * the backend. Its ONLY control-plane touchpoint is `controlPlane` (the
 * broker-backed impl inside the sandbox), so nothing here needs the DB. The
 * DB-bound prologue (load bundle/knowledge/memory/skills + build the prompt)
 * and epilogue (fence handling + persistence) stay on the host in runChatTurn.
 *
 * Hermes mounts these logical servers through the broker-backed stdio bridge.
 */
export async function runAgentBackend(
  input: RunAgentBackendInput,
): Promise<AgentRunResult> {
  const {
    backend,
    prompt,
    userMessage,
    orgId,
    threadId,
    runId,
    workspace,
    backendState,
    pluginActions,
    packActions = [],
    sourceConfigEnabled = false,
    botEnabled = false,
    dataSurface = "customer",
    graphjinToolPolicy,
    graphjinDataPath = "direct",
    nativeDelegation,
    controlPlane,
    wantsCards = true,
    cardSchema = "core",
    emit,
    signal,
  } = input;

  const mcp = backend.capabilities.mcpTools;
  const recordsOnly = dataSurface === "records";
  const appContext = recordsOnly
    ? parseAppWorkContext(backendState?.appContext)
    : null;
  const recordContext = recordsOnly
    ? parseRecordWorkContext(backendState?.recordContext)
    : null;
  // Legacy record-context threads retain their original exact-object scope.
  // App-owned chat uses the actor's normal records grants across apps.
  const recordScope = !appContext && recordContext
    ? {
        appId: recordContext.appId,
        objectApiName: recordContext.objectApiName,
      }
    : undefined;
  const pluginActionServer = mcp && !recordsOnly
    ? buildPluginActionServer({
        orgId,
        threadId,
        runId,
        descriptors: pluginActions,
        emit,
        controlPlane,
      })
    : null;
  const packActionServer = mcp && !recordsOnly
    ? buildPackActionServer({
        orgId,
        threadId,
        runId,
        descriptors: packActions,
        emit,
        controlPlane,
      })
    : null;

  const mcpServers = mcp
    ? recordsOnly
      ? {
          neko_interaction: buildAskUserQuestionServer({
            runId,
            wantsCards,
            emit,
          }),
          neko_records: buildRecordsReadServer({
            orgId,
            runId,
            controlPlane,
            ...(recordScope ? { scope: recordScope } : {}),
          }),
        }
      : {
        // Work chat is interactive. Material ambiguity pauses the turn and
        // returns control to the operator through a deterministic form.
        neko_interaction: buildAskUserQuestionServer({
          runId,
          wantsCards,
          emit,
        }),
        // GraphJin is brokered through the trusted host. No binary, source
        // URL, or credential enters the box.
        ...(graphjinDataPath === "agent"
          ? { neko_graphjin_agent: buildGraphjinAgentServer({ orgId, runId, controlPlane }) }
          : {
              neko_graphjin: buildGraphjinMcpServer({
                orgId,
                runId,
                controlPlane,
                ...(graphjinToolPolicy ? { toolPolicy: graphjinToolPolicy } : {}),
              }),
            }),
        // Rendering is per-channel: the card server only ships to web turns.
        ...(wantsCards ? { neko_ui: buildRenderCardsServer(emit, { rich: cardSchema === "rich" }) } : {}),
        neko_skills: buildSkillBuilderServer(workspace.skillsRoot),
        neko_memory: buildWorkMemoryServer({ orgId, threadId, runId }, { controlPlane }),
        ...(botEnabled ? { neko_bot: buildBotServer({ orgId, threadId }, { controlPlane }) } : {}),
        // Ax's discover() reaches the semantic skill index through this server.
        ...(backend.id === "ax" ? { neko_guides: buildSkillSearchServer({ orgId, runId }, { controlPlane }) } : {}),
        neko_library: buildLibraryServer({ orgId, threadId, runId }, { controlPlane }),
        neko_records: buildRecordsReadServer({ orgId, runId, controlPlane }),
        neko_workflow_builder: buildWorkflowBuilderServer({
          orgId,
          createdByThreadId: threadId,
          createdByRunId: runId,
          emit,
          controlPlane,
        }),
        neko_rule_builder: buildRuleBuilderServer({
          orgId,
          createdByThreadId: threadId,
          createdByRunId: runId,
          emit,
          controlPlane,
        }),
        neko_plugin_manager: buildPluginManagerServer({
          orgId,
          runId,
          emit,
          controlPlane,
        }),
        neko_user_manager: buildUserManagerServer({
          orgId,
          runId,
          emit,
          controlPlane,
        }),
        neko_channel_manager: buildChannelManagerServer({
          orgId,
          runId,
          emit,
          controlPlane,
        }),
        neko_data_source_manager: buildDataSourceManagerServer({
          orgId,
          runId,
          emit,
          controlPlane,
        }),
        ...(sourceConfigEnabled
          ? {
              neko_source_config_manager: buildSourceConfigManagerServer({
                orgId,
                runId,
                emit,
                controlPlane,
              }),
            }
          : {}),
        neko_audit: buildAuditViewerServer({ orgId, runId, controlPlane }),
        ...(pluginActionServer ? { neko_plugin_actions: pluginActionServer } : {}),
        ...(packActionServer ? { neko_pack_actions: packActionServer } : {}),
      }
    : undefined;

  return backend.run({
    prompt,
    userMessage,
    orgId,
    workspace,
    backendState,
    onEvent: emit,
    mcpServers,
    // ACP backends mount the same servers as stdio bridge children — ship the
    // per-run context the bridge needs to rebuild them (broker coords ride the
    // process env; see apps/worker/src/agent-sandbox/mcp-bridge.ts).
    mcpBridgeEnv: mcp
      ? {
          OPENNEKO_MCP_MODE: "work",
          OPENNEKO_MCP_ORG_ID: orgId,
          OPENNEKO_MCP_THREAD_ID: threadId,
          OPENNEKO_MCP_RUN_ID: runId,
          OPENNEKO_MCP_SKILLS_ROOT: workspace.skillsRoot,
          OPENNEKO_MCP_WANTS_CARDS: wantsCards ? "1" : "0",
          OPENNEKO_MCP_CARD_SCHEMA: cardSchema,
          ...(graphjinToolPolicy
            ? {
                [GRAPHJIN_TOOL_POLICY_ENV]:
                  serializeGraphjinMcpToolPolicy(graphjinToolPolicy),
              }
            : {}),
          OPENNEKO_MCP_PLUGIN_ACTIONS: JSON.stringify(
            recordsOnly ? [] : (pluginActions ?? []),
          ),
          OPENNEKO_MCP_PACK_ACTIONS: JSON.stringify(
            recordsOnly ? [] : packActions,
          ),
          ...(recordScope
            ? { OPENNEKO_MCP_RECORD_SCOPE: JSON.stringify(recordScope) }
            : {}),
        }
      : undefined,
    ...(nativeDelegation ? { nativeDelegation } : {}),
    wantsCards,
    tag: `work ${runId}`,
    signal,
  });
}
