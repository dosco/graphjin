import { randomUUID } from "node:crypto";
import {
  createDynamicMcpServer,
  createMcpServer,
  defineMcpTool,
} from "../mcp-server";
import type { Tool } from "@modelcontextprotocol/sdk/types.js";
import { z } from "zod";
import { writeWorkSkillFiles } from "./skill-files";
import { RENDER_CARDS_DESCRIPTION } from "./render-catalog";
import {
  buildSourceFormSurface,
  SOURCE_FORM_PREFILL_SHAPE,
  type SourceFormPrefill,
} from "./source-form";
import type { AgentEvent } from "../agent-backend";
import { WORK_MEMORY_KINDS, type WorkMemoryContext } from "./memory-types";
import type { RiskLevel } from "../workflows";
import type { AgentControlPlane } from "./control-plane";
import {
  A2UI_RENDER_SERVER_NAME,
  A2UI_RENDER_TOOL_NAME,
  renderCardsInputSchema,
  validateRenderCardsInput,
} from "./a2ui-contract";
import { OPENNEKO_GRAPHJIN_MCP_SERVER_NAME } from "../graphjin/mcp-names";
import {
  applyGraphjinMcpToolPolicy,
  assertGraphjinMcpToolCallAllowed,
  type GraphjinMcpToolPolicy,
} from "./graphjin-tool-policy";

function requireControlPlane(
  controlPlane: AgentControlPlane | undefined,
): AgentControlPlane {
  if (!controlPlane) {
    throw new Error("Agent MCP server requires an explicit control plane");
  }
  return controlPlane;
}

export function buildRenderCardsServer(
  emit: (event: AgentEvent) => Promise<void> | void,
  opts: { rich?: boolean } = {},
) {
  const rich = opts.rich ?? false;
  // Raw arguments reach the handler so bridge-shaped calls can be normalized
  // before validation. The advertised schema is still the typed contract.
  return createDynamicMcpServer({
    name: A2UI_RENDER_SERVER_NAME,
    version: "1.0.0",
    listTools: async () => [{
      name: A2UI_RENDER_TOOL_NAME,
      description: RENDER_CARDS_DESCRIPTION,
      inputSchema: renderCardsInputSchema(rich) as Tool["inputSchema"],
    }],
    callTool: async ({ name, arguments: args }) => {
      if (name !== A2UI_RENDER_TOOL_NAME) {
        return { isError: true, content: [{ type: "text", text: `Unknown tool: ${name}` }] };
      }
      const validated = validateRenderCardsInput(args, undefined, { rich });
      if (!validated.success) {
        return {
          isError: true,
          content: [{
            type: "text",
            text: `Cards not shown. Fix and call again:\n${validated.issues.map((issue) => `- ${issue.message}`).join("\n")}`,
          }],
        };
      }
      await emit({ type: "surface", messages: validated.messages });
      return { content: [{ type: "text", text: "Cards shown." }] };
    },
  });
}

async function proposeAdminAction(opts: {
  controlPlane: AgentControlPlane;
  orgId: string;
  runId?: string;
  emit: (event: AgentEvent) => Promise<void> | void;
  kind:
    | "user_admin"
    | "group_admin"
    | "channel_admin"
    | "data_source_admin"
    | "source_config_admin";
  target: string;
  intent: string;
  payload: Record<string, unknown>;
}) {
  const decision = await opts.controlPlane.evaluateActionPolicy({
    orgId: opts.orgId,
    scope: "internal",
    kind: opts.kind,
    target: opts.target,
    riskLevel: "high",
  } as Parameters<AgentControlPlane["evaluateActionPolicy"]>[0]);

  if (decision.decision !== "allow" && decision.decision !== "needs_approval") {
    return {
      content: [
        {
          type: "text" as const,
          text: JSON.stringify({
            ok: false,
            decision: "denied",
            reason:
              "reason" in decision && decision.reason
                ? String(decision.reason)
                : "policy denies this change from chat",
          }),
        },
      ],
    };
  }

  const status =
    decision.decision === "allow" ? "approved" : "pending_approval";
  const request = await opts.controlPlane.createActionRequest({
    orgId: opts.orgId,
    scope: "internal",
    kind: opts.kind,
    target: opts.target,
    payload: opts.payload,
    riskLevel: "high",
    status,
    policyId: "policy" in decision && decision.policy ? decision.policy.id : null,
    summary: opts.intent,
    intent: opts.intent,
    workRunId: opts.runId ?? null,
    requestedByRunId: null,
  } as Parameters<AgentControlPlane["createActionRequest"]>[0]);

  if (status === "approved") {
    await opts.controlPlane.enqueueActionExecute({
      orgId: opts.orgId,
      actionRequestId: request.id,
    });
  }

  await opts.emit({
    type: "action_request_emit",
    action_request_id: request.id,
    kind: opts.kind,
    scope: "internal",
    decision: status === "approved" ? "auto_approved" : "pending_approval",
    summary: opts.intent,
    risk_level: "high",
  } as AgentEvent);

  return {
    content: [
      {
        type: "text" as const,
        text: JSON.stringify({
          ok: true,
          decision: status,
          action_request_id: request.id,
          note:
            status === "approved"
              ? "Queued for execution; the result lands in this run."
              : "Approval-gated: tell the operator what you proposed and end your turn — the approval card is rendered inline.",
        }),
      },
    ],
  };
}

/**
 * ADM1 — chat-first user management. list_users reads through the
 * control plane; every change is an approval-gated action request whose
 * policy demands an ADMIN approver (user_management_default, K2-enforced).
 */
export function buildUserManagerServer(opts: {
  orgId: string;
  runId?: string;
  emit: (event: AgentEvent) => Promise<void> | void;
  controlPlane?: AgentControlPlane;
}) {
  const controlPlane = requireControlPlane(opts.controlPlane);

  const listUsers = defineMcpTool(
    "list_users",
    [
      "List the org's users and synchronized SSO groups with stable IDs,",
      "membership, role, disabled state, and last login. Use before",
      "proposing any user, group, or access change.",
    ].join(" "),
    {},
    async () => ({
      content: [
        {
          type: "text" as const,
          text: JSON.stringify(await controlPlane.listUsers({ orgId: opts.orgId })),
        },
      ],
    }),
  );

  const requestChange = defineMcpTool(
    "request_user_change",
    [
      "Propose a user-management change: invite (email + role),",
      "set_role (userId + role admin|member), deactivate or reactivate",
      "(userId). NEVER applies directly — files an action request that an",
      "ADMIN must approve on the approvals surface. Tell the operator what",
      "you proposed and end your turn.",
    ].join(" "),
    {
      action: z.enum(["invite", "set_role", "deactivate", "reactivate"]),
      email: z.string().trim().email().optional(),
      userId: z.string().trim().min(1).optional(),
      role: z.enum(["admin", "member"]).optional(),
      intent: z.string().trim().min(1).max(500),
    },
    async (args) => {
      if (args.action === "invite" && (!args.email || !args.role)) {
        return {
          content: [
            {
              type: "text" as const,
              text: JSON.stringify({ ok: false, error: "invite needs email + role" }),
            },
          ],
        };
      }
      if (args.action !== "invite" && !args.userId) {
        return {
          content: [
            {
              type: "text" as const,
              text: JSON.stringify({ ok: false, error: `${args.action} needs userId` }),
            },
          ],
        };
      }
      return proposeAdminAction({
        controlPlane,
        orgId: opts.orgId,
        runId: opts.runId,
        emit: opts.emit,
        kind: "user_admin",
        target: args.email ?? args.userId ?? args.action,
        intent: args.intent,
        payload: {
          action: args.action,
          ...(args.email ? { email: args.email } : {}),
          ...(args.userId ? { userId: args.userId } : {}),
          ...(args.role ? { role: args.role } : {}),
        },
      });
    },
  );

  const listGroups = defineMcpTool(
    "list_groups",
    [
      "List OpenNeko groups with their members (and whether each membership",
      "is local or from an IdP rule), the items each group holds, IdP rules,",
      "and each group's table data access. Use before proposing any group,",
      "item grant or data access change.",
    ].join(" "),
    {},
    async () => ({
      content: [{ type: "text" as const, text: JSON.stringify(await controlPlane.listGroups({ orgId: opts.orgId })) }],
    }),
  );

  const fail = (error: string) => ({ content: [{ type: "text" as const, text: JSON.stringify({ ok: false, error }) }] });

  const requestGroupChange = defineMcpTool(
    "request_group_change",
    [
      "Propose a group change: create_group (name, description?),",
      "delete_group (groupId), add_member or remove_member (groupId + userId),",
      "add_idp_rule (ssoGroupId + groupId) or remove_idp_rule (ruleId).",
      "NEVER applies directly — files an action request an ADMIN must approve.",
      "Tell the operator what you proposed and end your turn.",
    ].join(" "),
    {
      action: z.enum(["create_group", "delete_group", "add_member", "remove_member", "add_idp_rule", "remove_idp_rule"]),
      name: z.string().trim().min(1).max(120).optional(),
      description: z.string().trim().max(500).optional(),
      groupId: z.string().trim().min(1).optional(),
      userId: z.string().trim().min(1).optional(),
      ssoGroupId: z.string().trim().min(1).optional(),
      ruleId: z.string().trim().min(1).optional(),
      intent: z.string().trim().min(1).max(500),
    },
    async (args) => {
      const needs: Record<typeof args.action, Array<keyof typeof args>> = {
        create_group: ["name"],
        delete_group: ["groupId"],
        add_member: ["groupId", "userId"],
        remove_member: ["groupId", "userId"],
        add_idp_rule: ["groupId", "ssoGroupId"],
        remove_idp_rule: ["ruleId"],
      };
      const missing = needs[args.action].filter((key) => !args[key]);
      if (missing.length) return fail(`${args.action} needs ${missing.join(" + ")}`);
      const { intent, ...payload } = args;
      return proposeAdminAction({
        controlPlane,
        orgId: opts.orgId,
        runId: opts.runId,
        emit: opts.emit,
        kind: "group_admin",
        target: args.groupId ?? args.name ?? args.ruleId ?? args.action,
        intent,
        payload,
      });
    },
  );

  const requestItemGrant = defineMcpTool(
    "request_item_grant",
    [
      "Propose granting or revoking one item for a group. itemType is one of",
      "skill, workflow, metric, dashboard,",
      "watcher, team_memory, data_source, saved_query, api_operation, action,",
      "integration, channel, pack. itemId '*' means every current and future",
      "item of the type. NEVER applies directly — an ADMIN must approve.",
    ].join(" "),
    {
      action: z.enum(["grant_item", "revoke_item"]),
      groupId: z.string().trim().min(1),
      itemType: z.enum([
        "skill", "workflow", "metric", "dashboard", "watcher",
        "team_memory", "data_source", "saved_query", "api_operation", "action", "integration", "channel", "pack",
      ]),
      itemId: z.string().trim().min(1).max(500),
      intent: z.string().trim().min(1).max(500),
    },
    async ({ intent, ...payload }) =>
      proposeAdminAction({
        controlPlane,
        orgId: opts.orgId,
        runId: opts.runId,
        emit: opts.emit,
        kind: "group_admin",
        target: `${payload.itemType}:${payload.itemId}`,
        intent,
        payload,
      }),
  );

  const rowFilterSchema: z.ZodType<unknown> = z.lazy(() =>
    z.union([
      z.object({ and: z.array(rowFilterSchema).min(1) }),
      z.object({ or: z.array(rowFilterSchema).min(1) }),
      z.object({
        column: z.string().min(1),
        op: z.enum(["eq", "neq", "gt", "gte", "lt", "lte", "in", "nin", "is_null"]),
        value: z.union([
          z.string(), z.number(), z.boolean(),
          z.array(z.union([z.string(), z.number(), z.boolean()])),
          z.object({ var: z.enum(["user_id", "user_groups", "account_id"]) }),
        ]),
      }),
    ]),
  );

  const requestDataAccessChange = defineMcpTool(
    "request_data_access_change",
    [
      "Propose which table rows and columns a group reads through GraphJin:",
      "set_table_access (groupId, source, table as name or schema.name, columns,",
      "rowFilter?) or remove_table_access (ruleId). rowFilter is a tree of",
      "{column, op, value} conditions joined with {and: []} or {or: []}; value",
      "may be {var: 'user_id'} or {var: 'user_groups'}. NEVER applies directly",
      "— an ADMIN must approve.",
    ].join(" "),
    {
      action: z.enum(["set_table_access", "remove_table_access"]),
      groupId: z.string().trim().min(1).optional(),
      source: z.string().trim().min(1).optional(),
      table: z.string().trim().min(1).optional(),
      columns: z.array(z.string().trim().min(1)).min(1).optional(),
      rowFilter: rowFilterSchema.optional(),
      ruleId: z.string().trim().min(1).optional(),
      intent: z.string().trim().min(1).max(500),
    },
    async ({ intent, ...payload }) => {
      if (payload.action === "set_table_access" && (!payload.groupId || !payload.source || !payload.table || !payload.columns)) {
        return fail("set_table_access needs groupId + source + table + columns");
      }
      if (payload.action === "remove_table_access" && !payload.ruleId) return fail("remove_table_access needs ruleId");
      return proposeAdminAction({
        controlPlane,
        orgId: opts.orgId,
        runId: opts.runId,
        emit: opts.emit,
        kind: "group_admin",
        target: payload.table ? `${payload.source}:${payload.table}` : (payload.ruleId ?? payload.action),
        intent,
        payload,
      });
    },
  );

  return createMcpServer({
    name: "neko_user_manager",
    version: "1.0.0",
    tools: [listUsers, requestChange, listGroups, requestGroupChange, requestItemGrant, requestDataAccessChange],
  });
}

/**
 * ADM5 — chat-first channel management. Reads (workspaces + identities)
 * answer "who's talking to the bot and as whom?"; changes (link/unlink/
 * block/unblock an identity) file an action request an ADMIN must
 * approve (channel_management_default, K2-enforced).
 */
export function buildChannelManagerServer(opts: {
  orgId: string;
  runId?: string;
  emit: (event: AgentEvent) => Promise<void> | void;
  controlPlane?: AgentControlPlane;
}) {
  const controlPlane = requireControlPlane(opts.controlPlane);

  const listChannels = defineMcpTool(
    "list_channels",
    [
      "List the org's channel workspaces (Slack team /",
      "Telegram bot bindings) and every channel identity seen, with link",
      "status (linked|unverified|blocked) and the app_user it acts as.",
      "Use before proposing any change, and to answer 'who can reach the",
      "agent from chat?'.",
    ].join(" "),
    {},
    async () => ({
      content: [
        {
          type: "text" as const,
          text: JSON.stringify(
            await controlPlane.listChannels({ orgId: opts.orgId }),
          ),
        },
      ],
    }),
  );

  const requestChange = defineMcpTool(
    "request_channel_change",
    [
      "Propose a channel-identity change: link (identityId + appUserId),",
      "unlink, block or unblock (identityId). NEVER applies directly —",
      "files an action request that an ADMIN must approve on the approvals",
      "surface. Tell the operator what you proposed and end your turn.",
    ].join(" "),
    {
      action: z.enum(["link", "unlink", "block", "unblock"]),
      identityId: z.string().trim().min(1),
      appUserId: z.string().trim().min(1).optional(),
      intent: z.string().trim().min(1).max(500),
    },
    async (args) => {
      if (args.action === "link" && !args.appUserId) {
        return {
          content: [
            {
              type: "text" as const,
              text: JSON.stringify({ ok: false, error: "link needs appUserId" }),
            },
          ],
        };
      }
      return proposeAdminAction({
        controlPlane,
        orgId: opts.orgId,
        runId: opts.runId,
        emit: opts.emit,
        kind: "channel_admin",
        target: args.identityId,
        intent: args.intent,
        payload: {
          action: args.action,
          identityId: args.identityId,
          ...(args.appUserId ? { appUserId: args.appUserId } : {}),
        },
      });
    },
  );

  return createMcpServer({
    name: "neko_channel_manager",
    version: "1.0.0",
    tools: [listChannels, requestChange],
  });
}

/**
 * ADM2 — chat-first data-source registry. Reads list the org's sources
 * (hostnames only — connection strings and credentials never enter
 * model context); changes file a data_source_admin action request an
 * ADMIN must approve. Registration creates a disabled placeholder; the
 * admin enters connection details + credentials in the settings form
 * (forms are for credential entry only, never the model path). The
 * GraphJin discovery itself runs through trusted control-plane methods; no
 * source credential or direct GraphJin connection enters the agent sandbox.
 */
export function buildDataSourceManagerServer(opts: {
  orgId: string;
  runId?: string;
  emit: (event: AgentEvent) => Promise<void> | void;
  controlPlane?: AgentControlPlane;
}) {
  const controlPlane = requireControlPlane(opts.controlPlane);

  const listSources = defineMcpTool(
    "list_data_sources",
    [
      "List the org's registered data sources: name, label, auth mode,",
      "default flag, enabled state and server hostname. No connection",
      "strings or credentials are ever returned. Use before proposing",
      "any change.",
    ].join(" "),
    {},
    async () => ({
      content: [
        {
          type: "text" as const,
          text: JSON.stringify(
            await controlPlane.listDataSources({ orgId: opts.orgId }),
          ),
        },
      ],
    }),
  );

  const requestChange = defineMcpTool(
    "request_data_source_change",
    [
      "Propose a data-source registry change: register (name + optional",
      "label — creates a DISABLED placeholder; the admin completes the",
      "connection in Settings, credentials never pass through chat),",
      "enable, disable, set_default, or remove (name). NEVER applies",
      "directly — files an action request that an ADMIN must approve.",
      "Tell the operator what you proposed and end your turn.",
    ].join(" "),
    {
      action: z.enum(["register", "enable", "disable", "set_default", "remove"]),
      name: z
        .string()
        .trim()
        .min(1)
        .max(64)
        .regex(/^[a-z0-9][a-z0-9-]*$/, "lowercase letters, digits, dashes"),
      label: z.string().trim().max(120).optional(),
      // OL5 source graph: what kind of system this source fronts.
      sourceKind: z.enum(["graphjin", "database", "api", "files", "code"]).optional(),
      intent: z.string().trim().min(1).max(500),
    },
    async (args) =>
      proposeAdminAction({
        controlPlane,
        orgId: opts.orgId,
        runId: opts.runId,
        emit: opts.emit,
        kind: "data_source_admin",
        target: args.name,
        intent: args.intent,
        payload: {
          action: args.action,
          name: args.name,
          ...(args.label ? { label: args.label } : {}),
          ...(args.sourceKind ? { sourceKind: args.sourceKind } : {}),
        },
      }),
  );

  return createMcpServer({
    name: "neko_data_source_manager",
    version: "1.0.0",
    tools: [listSources, requestChange],
  });
}

/**
 * OL5 — chat-first configuration of the CUSTOMER GraphJin engine
 * (sources, roles, access). describe_source_graph reads the live
 * gj_catalog; request_source_config_change files a source_config_admin
 * action an ADMIN must approve. The agent NEVER applies config (its CLI
 * guard blocks mutations) and NEVER sees a secret value — registering a
 * DB source references a secret by NAME; the worker resolves it at apply
 * time. This only ever targets the customer source, never the OpenNeko
 * internal GraphJin (the adapter asserts that boundary).
 */
export function buildSourceConfigManagerServer(opts: {
  orgId: string;
  runId?: string;
  emit: (event: AgentEvent) => Promise<void> | void;
  controlPlane?: AgentControlPlane;
}) {
  const controlPlane = requireControlPlane(opts.controlPlane);

  const describe = defineMcpTool(
    "describe_source_graph",
    [
      "Return the selected GraphJin data engine's live source graph, including",
      "its discovered databases, capabilities, and namespaces. Use the result",
      "to answer source questions and to prepare configuration requests.",
    ].join(" "),
    {},
    async () => ({
      content: [
        {
          type: "text" as const,
          text: JSON.stringify(
            await controlPlane.describeSourceGraph({
              orgId: opts.orgId,
              runId: opts.runId ?? null,
            }),
          ),
        },
      ],
    }),
  );

  const listSecretNames = defineMcpTool(
    "list_source_secret_names",
    [
      "Return stored connection-secret names for use as `secretRef` values in",
      "a register_source proposal. When the required name is absent, direct",
      "the admin to Admin > Settings > GraphJin Config to add it.",
    ].join(" "),
    {},
    async () => ({
      content: [
        {
          type: "text" as const,
          text: JSON.stringify(
            await controlPlane.listSourceSecretNames({
              orgId: opts.orgId,
              runId: opts.runId ?? null,
            }),
          ),
        },
      ],
    }),
  );

  const presentSourceForm = defineMcpTool(
    "present_source_form",
    [
      "Show the admin the source configuration form for a new or changed",
      "Database, API or Files source. Pass any values you already know; the",
      "admin completes the rest and submits it for review. The submitted values",
      "arrive in the next turn for request_source_config_change.",
    ].join(" "),
    SOURCE_FORM_PREFILL_SHAPE,
    async (args: SourceFormPrefill) => {
      await opts.emit({
        type: "surface",
        messages: buildSourceFormSurface(args, `source-form-${randomUUID()}`),
      });
      return { content: [{ type: "text" as const, text: "Form shown." }] };
    },
  );

  const askConfigAgent = defineMcpTool(
    "ask_graphjin_config_agent",
    [
      "Ask the selected GraphJin configuration agent to inspect, explain, or",
      "plan against its redacted configuration. Use this for questions about",
      "settings, roles, sources, access, security, runtime, and reload impact.",
      "For multiple data sources, pass the selected dataSourceId.",
    ].join(" "),
    {
      instruction: z.string().trim().min(1).max(8_000),
      dataSourceId: z.string().uuid().optional(),
      maxSteps: z.number().int().min(1).max(12).optional(),
    },
    async (args) => ({
      content: [
        {
          type: "text" as const,
          text: JSON.stringify(
            await controlPlane.askSourceConfigAgent({
              orgId: opts.orgId,
              runId: opts.runId ?? null,
              instruction: args.instruction,
              dataSourceId: args.dataSourceId,
              maxSteps: args.maxSteps,
            }),
          ),
        },
      ],
    }),
  );

  const importOpenApi = defineMcpTool(
    "import_openapi_spec",
    [
      "Import and validate a hosted OpenAPI 3.x YAML or JSON document for",
      "the current organization. Use this when an admin supplies an HTTPS",
      "URL in chat. The trusted host fetches it and returns a managed asset ID.",
    ].join(" "),
    {
      url: z.string().url().max(2_000),
      baseUrl: z.string().url().max(2_000).optional(),
    },
    async (args) => ({
      content: [
        {
          type: "text" as const,
          text: JSON.stringify(
            await controlPlane.importOpenApiSpec({
              orgId: opts.orgId,
              runId: opts.runId ?? null,
              url: args.url,
              baseUrl: args.baseUrl,
            }),
          ),
        },
      ],
    }),
  );

  const listOpenApi = defineMcpTool(
    "list_openapi_specs",
    "List this organization's recently imported OpenAPI asset metadata and IDs.",
    { limit: z.number().int().min(1).max(100).optional() },
    async (args) => ({
      content: [
        {
          type: "text" as const,
          text: JSON.stringify(
            await controlPlane.listOpenApiSpecs({
              orgId: opts.orgId,
              runId: opts.runId ?? null,
              limit: args.limit,
            }),
          ),
        },
      ],
    }),
  );

  const requestChange = defineMcpTool(
    "request_source_config_change",
    [
      "Create a source_config_admin proposal for admin review:",
      "- add_role { name, match }: a GraphJin role selected by a JWT match expression.",
      "- set_source_access { source, read, write, delete }: access mode per source (one of public|authenticated|account|owner|admin|blocked; write/delete also accept blocked).",
      "- enable_api_writes { source, spec, operation, allowedRoles, write?, exposeAs?, apiDelete? }: one proposal that turns on api.write, sets access.write (authenticated by default, or admin), and exposes one OpenAPI POST/PUT/PATCH/DELETE operation as a GraphQL mutation for the listed roles. Prefer this for governed writes.",
      "- set_source_capabilities { source, apiWrite?, apiDelete? } and expose_api_operation { source, spec, operation, exposeMutation, allowedRoles, exposeAs? }: adjust one write setting on an API source that already has writes enabled.",
      "Database sources stay read-only; a proposal that opens a write path on one is rejected.",
      "- register_source database { name, kind, type?, host?, port?, dbname?, user?, secretRef? }.",
      "- register_source api { name, kind, specAssetId } using an OpenAPI document already imported by URL or upload.",
      "- register_source file local { name, kind, backend, localFiles } using OpenNeko-managed isolated storage.",
      "- register_source file s3|gcs { name, kind, backend, bucket, prefix?, region?, endpoint?, publicBaseUrl?, presignTtl? } using deployment runtime credentials.",
      "File sources are provisioned read-only for authenticated organization members.",
      "After the tool returns, summarize the proposed change and its approval status.",
    ].join("\n"),
    {
      action: z.enum([
        "add_role",
        "set_source_access",
        "set_source_capabilities",
        "expose_api_operation",
        "enable_api_writes",
        "register_source",
      ]),
      // add_role
      name: z.string().trim().min(1).max(64).optional(),
      match: z.string().trim().min(1).max(500).optional(),
      // set_source_access
      source: z.string().trim().min(1).max(64).optional(),
      read: z
        .enum(["public", "authenticated", "account", "owner", "admin", "blocked"])
        .optional(),
      write: z.enum(["authenticated", "account", "owner", "admin", "blocked"]).optional(),
      delete: z.enum(["authenticated", "account", "owner", "admin", "blocked"]).optional(),
      // set_source_capabilities
      apiWrite: z.boolean().optional(),
      apiDelete: z.boolean().optional(),
      // expose_api_operation
      spec: z.string().trim().min(1).max(128).optional(),
      operation: z.string().trim().min(1).max(200).optional(),
      exposeMutation: z.boolean().optional(),
      allowedRoles: z
        .array(z.enum(["admin", "member", "service", "user"]))
        .max(4)
        .optional(),
      exposeAs: z
        .string()
        .trim()
        .regex(/^[_A-Za-z][_0-9A-Za-z]*$/, "a GraphQL field name")
        .max(120)
        .optional(),
      // register_source
      kind: z
        .preprocess(
          (value) =>
            Array.isArray(value) && value.length === 1 ? value[0] : value,
          z.enum(["database", "api", "file"]),
        )
        .optional(),
      type: z.string().trim().min(1).max(40).optional(),
      host: z.string().trim().max(255).optional(),
      port: z.number().int().min(1).max(65535).optional(),
      dbname: z.string().trim().max(128).optional(),
      user: z.string().trim().max(128).optional(),
      secretRef: z
        .string()
        .trim()
        .max(128)
        .regex(/^[A-Za-z0-9._-]*$/, "a secret name, not a value")
        .optional(),
      specAssetId: z.string().uuid().optional(),
      backend: z
        .preprocess(
          (value) =>
            Array.isArray(value) && value.length === 1 ? value[0] : value,
          z.enum(["local", "s3", "gcs"]),
        )
        .optional(),
      bucket: z.string().trim().max(255).optional(),
      prefix: z.string().trim().max(500).optional(),
      region: z.string().trim().max(100).optional(),
      endpoint: z.string().url().max(1000).optional(),
      publicBaseUrl: z.string().url().max(1000).optional(),
      presignTtl: z.string().trim().max(40).optional(),
      localFiles: z
        .object({
          sourceName: z.string().trim().min(1).max(64),
          ready: z.literal(true),
          files: z
            .array(
              z.object({
                name: z.string().trim().min(1).max(128),
                size: z.number().int().min(1).max(10 * 1024 * 1024),
                contentType: z.string().trim().min(1).max(200),
                checksumSha256: z.string().regex(/^[a-f0-9]{64}$/),
              }),
            )
            .min(1)
            .max(500),
          totalSize: z.number().int().min(1).max(100 * 1024 * 1024),
        })
        .optional(),
      dataSourceId: z.string().uuid().optional(),
      intent: z.string().trim().min(1).max(500),
    },
    async (args) => {
      // Build a credential-free payload — secretRef is a NAME only; the
      // schema regex forbids whitespace/values, and we never accept a
      // literal secret field.
      const { action, intent, dataSourceId, ...rest } = args;
      const target =
        action === "add_role"
          ? `role:${args.name ?? ""}`
          : action === "set_source_access"
            ? `access:${args.source ?? ""}`
            : action === "set_source_capabilities"
              ? `capabilities:${args.source ?? ""}`
              : action === "expose_api_operation" || action === "enable_api_writes"
                ? `operation:${args.source ?? ""}/${args.spec ?? ""}#${args.operation ?? ""}`
                : `source:${args.name ?? ""}`;
      const changePayload = {
        action,
        ...rest,
        ...(dataSourceId ? { dataSourceId } : {}),
      };
      const preview = await controlPlane.previewSourceConfigChange({
        orgId: opts.orgId,
        runId: opts.runId ?? null,
        dataSourceId,
        payload: changePayload,
      });
      if (preview.denied || preview.error || !preview.preview?.valid) {
        return {
          content: [
            {
              type: "text" as const,
              text: JSON.stringify({
                ok: false,
                stage: "graphjin_config_preview",
                ...preview,
              }),
            },
          ],
        };
      }
      return proposeAdminAction({
        controlPlane,
        orgId: opts.orgId,
        runId: opts.runId,
        emit: opts.emit,
        kind: "source_config_admin",
        target,
        intent,
        payload: {
          ...changePayload,
          ...(preview.specAsset ? { specSummary: preview.specAsset } : {}),
          preview: {
            source: preview.source,
            ...preview.preview,
          },
        },
      });
    },
  );

  return createMcpServer({
    name: "neko_source_config_manager",
    version: "1.1.0",
    tools: [
      describe,
      askConfigAgent,
      listSecretNames,
      presentSourceForm,
      importOpenApi,
      listOpenApi,
      requestChange,
    ],
  });
}

/**
 * ADM4 — chat-first audit viewer. One read-only tool returning the
 * action-request trail (with SEC5 dual identity), SEC7 behavior alerts,
 * and a 24h gateway-call summary. The control plane enforces the admin
 * gate on the requesting run's actor — a member or service run gets a
 * denial, never data.
 */
export function buildAuditViewerServer(opts: {
  orgId: string;
  runId?: string;
  controlPlane?: AgentControlPlane;
}) {
  const controlPlane = requireControlPlane(opts.controlPlane);

  const auditTrail = defineMcpTool(
    "audit_trail",
    [
      "ADMIN ONLY. The org's audit trail: recent action requests (who",
      "proposed what via which agent backend, and what happened),",
      "behavioral alerts (SEC7 thresholds), and a 24h summary of",
      "control-plane gateway calls per run. Use to answer 'what has the",
      "agent been doing?' and 'who approved that?'.",
    ].join(" "),
    {
      limit: z.number().int().min(1).max(200).optional(),
    },
    async (args) => {
      const result = await controlPlane.listAuditTrail({
        orgId: opts.orgId,
        runId: opts.runId ?? null,
        limit: args.limit,
      });
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify(
              result.denied
                ? { ok: false, error: "audit trail is admin-only" }
                : { ok: true, ...result },
            ),
          },
        ],
      };
    },
  );

  return createMcpServer({
    name: "neko_audit",
    version: "1.0.0",
    tools: [auditTrail],
  });
}

export function buildSkillBuilderServer(skillsRoot: string) {
  const createSkill = defineMcpTool(
    "create_skill",
    "Create or update an agentskills.io-style skill in Neko's shared skills directory.",
    {
      name: z.string().trim().min(1).max(64),
      description: z.string().trim().min(1).max(1024),
      body: z.string().min(1),
      license: z.string().trim().min(1).max(200).optional(),
      compatibility: z.string().trim().min(1).max(500).optional(),
      metadata: z.record(z.string().trim().min(1).max(128), z.string().max(2048)).optional(),
      allowedTools: z.string().trim().min(1).max(1000).optional(),
      files: z.array(
        z.object({
          path: z.string().trim().min(1).max(240),
          content: z.string().max(128 * 1024),
        }),
      ).max(30).optional(),
    },
    async (args) => {
      const result = await writeWorkSkillFiles(skillsRoot, args);
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify({
              ok: true,
              name: result.name,
              skillFile: `${result.skillPath}/SKILL.md`,
            }),
          },
        ],
      };
    },
  );

  return createMcpServer({
    name: "neko_skills",
    version: "1.0.0",
    tools: [createSkill],
  });
}

/**
 * Complete caller-visible GraphJin MCP surface for sandboxed agents. The
 * trusted broker forwards tools/list and tools/call with a short-lived token
 * for the bound run actor, preserving every native tool schema without
 * exposing the source URL or credential to the sandbox.
 */
export function buildGraphjinMcpServer(opts: {
  orgId: string;
  runId?: string;
  controlPlane?: AgentControlPlane;
  /** Optional per-run restriction applied identically in-process and over ACP. */
  toolPolicy?: GraphjinMcpToolPolicy;
}) {
  const controlPlane = requireControlPlane(opts.controlPlane);
  const identity = {
    orgId: opts.orgId,
    ...(opts.runId ? { runId: opts.runId } : {}),
  };
  return createDynamicMcpServer({
    name: OPENNEKO_GRAPHJIN_MCP_SERVER_NAME,
    version: "1.0.0",
    listTools: async () =>
      applyGraphjinMcpToolPolicy(
        await controlPlane.listGraphjinTools(identity),
        opts.toolPolicy,
      ),
    callTool: async (input) => {
      assertGraphjinMcpToolCallAllowed(opts.toolPolicy, input);
      return controlPlane.callGraphjinTool({
        ...identity,
        name: input.name,
        ...(input.arguments ? { arguments: input.arguments } : {}),
      });
    },
  });
}

/** @deprecated Use buildGraphjinMcpServer; retained for runtime API compatibility. */
export const buildGraphjinReadServer = buildGraphjinMcpServer;

/**
 * Read-only delegation surface for GraphJin's built-in server agent. The
 * sandbox supplies only a business instruction; the host owns source
 * selection, readiness checks, and credentials.
 */
/** The MCP _meta key a tool reports another agent's model tokens under (apps/ax-harness/internal/mcp). */
export const DELEGATED_USAGE_META_KEY = "openneko/delegated_usage";

export function buildGraphjinAgentServer(opts: {
  orgId: string;
  runId?: string;
  dataSourceId?: string;
  controlPlane?: AgentControlPlane;
}) {
  const controlPlane = requireControlPlane(opts.controlPlane);
  const ask = defineMcpTool(
    "ask",
    [
      "Delegate one read-only operational data question to GraphJin's",
      "catalog-first server agent. Include the complete business question,",
      "time window, aggregation, comparison baseline, and desired dimension",
      "in one instruction. The trusted host verifies read-only mode and keeps",
      "the source credential outside this sandbox.",
    ].join(" "),
    {
      instruction: z.string().trim().min(1).max(8_000),
      maxSteps: z.number().int().min(1).max(12).optional(),
    },
    async (args) => {
      const result = await controlPlane.askGraphjinDataAgent({
        orgId: opts.orgId,
        runId: opts.runId ?? null,
        ...(opts.dataSourceId ? { dataSourceId: opts.dataSourceId } : {}),
        instruction: args.instruction,
        ...(args.maxSteps ? { maxSteps: args.maxSteps } : {}),
      });
      // GraphJin's agent spends model tokens for this run; the harness counts
      // them against the turn's budget.
      const usage = result.response?.usage as { total_tokens?: unknown } | undefined;
      const totalTokens = typeof usage?.total_tokens === "number" ? usage.total_tokens : 0;
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify(result),
          },
        ],
        ...(totalTokens > 0
          ? { _meta: { [DELEGATED_USAGE_META_KEY]: { total_tokens: totalTokens } } }
          : {}),
      };
    },
  );

  return createMcpServer({
    name: "neko_graphjin_agent",
    version: "1.0.0",
    tools: [ask],
  });
}

// Memory surface: `search`, `save` and `update`. Reads use context search
// (matches the auto-context retrieval path: by meaning when semantic search is
// on, by keyword otherwise), writes go through the same rememberWorkMemory used
// by the `save:` chat command, which queues the row for background indexing. We deliberately don't expose
// `forget` from the agent — archival is operator-driven, not agent-driven.
export type WorkMemoryServerOptions = {
  /**
   * Expose the `save` tool. Default true. One-shot agents (e.g. the
   * metric agent) pass `false` so they get search-only — the operator
   * remains the authority on what gets persisted.
   */
  exposeSave?: boolean;
  /** Explicit in-process or broker-backed control plane. */
  controlPlane?: AgentControlPlane;
};

export function buildWorkMemoryServer(
  ctx: WorkMemoryContext,
  options: WorkMemoryServerOptions = {},
) {
  const exposeSave = options.exposeSave ?? true;
  const controlPlane = requireControlPlane(options.controlPlane);
  const search = defineMcpTool(
    "search",
    [
      "Semantic search over OpenNeko's saved memories. Returns top-N",
      "memories ranked by cosine similarity to the query. Use when the",
      "loaded memories above don't seem to cover what the operator is",
      "asking about.",
    ].join(" "),
    {
      query: z.string().min(2).max(800),
      limit: z.number().int().min(1).max(20).optional(),
    },
    async (args) => {
      const results = await controlPlane.searchWorkMemoryByContext({
        orgId: ctx.orgId,
        query: args.query,
        limit: args.limit ?? 5,
        runId: ctx.runId ?? null,
      });
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify({ ok: true, results }),
          },
        ],
      };
    },
  );

  const save = defineMcpTool(
    "save",
    [
      "Save a durable memory the operator stated explicitly. Use only when",
      "the operator says to remember/save something, corrects a recurring",
      "assumption, defines a metric/business rule, or states a stable",
      "preference. Never speculatively. Default scope=global, kind=business_rule,",
      "pinned=true.",
    ].join(" "),
    {
      text: z.string().min(5).max(2000),
      kind: z.enum(WORK_MEMORY_KINDS).optional(),
      scope: z.enum(["global", "thread"]).optional(),
      pinned: z.boolean().optional(),
    },
    async (args) => {
      const memory = await controlPlane.rememberWorkMemory({
        orgId: ctx.orgId,
        threadId: ctx.threadId ?? null,
        runId: ctx.runId ?? null,
        text: args.text,
        kind: args.kind ?? "business_rule",
        scope: args.scope ?? "global",
        pinned: args.pinned ?? true,
      });
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify({ ok: true, memoryId: memory.id }),
          },
        ],
      };
    },
  );

  const update = defineMcpTool(
    "update",
    [
      "Replace the text of a saved memory. Use when the operator corrects,",
      "extends or merges a memory that is already saved, instead of saving",
      "a second memory. Pass the memory id from the loaded list or a search",
      "result, and the whole new text.",
    ].join(" "),
    {
      memoryId: z.string().uuid(),
      text: z.string().min(5).max(2000),
    },
    async (args) => {
      try {
        const memory = await controlPlane.updateWorkMemory({
          orgId: ctx.orgId,
          runId: ctx.runId ?? null,
          threadId: ctx.threadId ?? null,
          memoryId: args.memoryId,
          text: args.text,
        });
        return {
          content: [
            {
              type: "text" as const,
              text: JSON.stringify({ ok: true, memoryId: memory.id }),
            },
          ],
        };
      } catch {
        return {
          content: [
            {
              type: "text" as const,
              text: JSON.stringify({ ok: false, error: `No saved memory ${args.memoryId} is visible to this run.` }),
            },
          ],
          isError: true,
        };
      }
    },
  );

  return createMcpServer({
    name: "neko_memory",
    version: "1.0.0",
    tools: exposeSave ? [search, save, update] : [search],
  });
}

// Search-only library surface. Whose files it searches is resolved
// server-side from the run binding — never from agent input — and GraphJin
// shows each user only their own folder, so a compromised prompt can't read
// another member's library.
export type LibraryServerOptions = {
  /** Explicit in-process or broker-backed control plane. */
  controlPlane?: AgentControlPlane;
};

export function buildLibraryServer(
  ctx: WorkMemoryContext,
  options: LibraryServerOptions = {},
) {
  const controlPlane = requireControlPlane(options.controlPlane);
  const search = defineMcpTool(
    "search",
    [
      "Search the operator's Library: the documents and spreadsheets they",
      "uploaded (policies, contracts, price lists, reports). Ranks files by",
      "content and returns each file's best matching passages with their",
      "page, slide, sheet or section, plus the SQL tables of sheets and CSV",
      "files. The files are the neko_library source of the GraphJin data",
      "tools: to read a whole file or compute from a sheet, ask those tools",
      "about the file key (for example: sum a column of a named table), and",
      "cite the file and location you used.",
    ].join(" "),
    {
      query: z.string().min(2).max(800),
      limit: z.number().int().min(1).max(20).optional(),
    },
    async (args) => {
      const results = await controlPlane.searchLibraryForRun({
        orgId: ctx.orgId,
        query: args.query,
        limit: args.limit ?? 5,
        runId: ctx.runId ?? null,
      });
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify({ ok: true, results }),
          },
        ],
      };
    },
  );

  return createMcpServer({
    name: "neko_library",
    version: "1.0.0",
    tools: [search],
  });
}

/**
 * Worker-registered governed action kinds,
 * surfaced to the agent as MCP tools. The worker passes the descriptors in
 * at turn start; this module only needs the {kind, description,
 * default_mode} triples to build tools.
 */
interface GovernedActionDescriptor {
  kind: string;
  description: string;
  /** Fixed policy scope. External is the default for compatibility. */
  scope?: "external" | "internal";
  /**
   * Seeded approval mode from the manifest; runtime policy may
   * override. Accepts either a scalar (applies to all scopes) or a
   * per-scope object for kinds whose default depends on the scope
   * they're invoked under.
   */
  default_mode?:
    | "auto"
    | "ask"
    | "deny"
    | {
        external?: "auto" | "ask" | "deny";
        internal?: "auto" | "ask" | "deny";
      };
  /** Example payload from the manifest, surfaced to the agent so it shapes the call correctly. */
  example?: Record<string, unknown>;
  /** Integration that handles the kind; the integration item gates it. */
  pluginName?: string;
}

export interface PluginActionDescriptor extends GovernedActionDescriptor {}

export interface PackActionDescriptor extends GovernedActionDescriptor {}

function modeForScope(
  default_mode: PluginActionDescriptor["default_mode"],
  scope: "external" | "internal",
): "auto" | "ask" | "deny" | undefined {
  if (default_mode === undefined) return undefined;
  if (typeof default_mode === "string") return default_mode;
  return scope === "external" ? default_mode.external : default_mode.internal;
}

function isDeniedEverywhere(
  default_mode: PluginActionDescriptor["default_mode"],
): boolean {
  if (default_mode === "deny") return true;
  if (default_mode && typeof default_mode === "object") {
    const keys = Object.keys(default_mode) as Array<"external" | "internal">;
    if (keys.length > 0 && keys.every((k) => default_mode[k] === "deny")) {
      return true;
    }
  }
  return false;
}

function needsIntentForKind(
  default_mode: PluginActionDescriptor["default_mode"],
): boolean {
  // Intent is required at schema-time whenever the kind COULD land in
  // ask-mode under any scope — so the agent has to author one
  // regardless of which scope it picks. Auto-only kinds skip it.
  if (default_mode === "ask") return true;
  if (default_mode && typeof default_mode === "object") {
    return Object.values(default_mode).some((m) => m === "ask");
  }
  return false;
}

export interface BuildPluginActionServerOptions {
  orgId: string;
  threadId: string;
  runId: string;
  descriptors: readonly PluginActionDescriptor[];
  emit: (event: AgentEvent) => Promise<void> | void;
  /** Explicit in-process or broker-backed control plane. */
  controlPlane?: AgentControlPlane;
}

export interface BuildPackActionServerOptions {
  orgId: string;
  threadId: string;
  runId: string;
  descriptors: readonly PackActionDescriptor[];
  emit: (event: AgentEvent) => Promise<void> | void;
  controlPlane?: AgentControlPlane;
}

/**
 * One MCP tool per registered governed action kind. Each tool's handler
 * routes through the action_policy engine:
 *
 *   - allow (auto_approve)  → create an approved action_request, enqueue it
 *                             for the worker, wait for execution, and return
 *                             the actual outcome inline. The agent sees the
 *                             result and keeps talking.
 *   - needs_approval (ask)  → create pending_approval action_request,
 *                             emit an action_request_emit event so the
 *                             /work UI can render an inline approval
 *                             card, return {pending_approval, id}.
 *                             The agent's run ends gracefully. Slice 5
 *                             will kick off a follow-up run with the
 *                             approval outcome.
 *   - deny / no_policy      → return {ok:false, denied} to the agent.
 *
 * Tools whose declared default_mode is "deny" are excluded from the
 * MCP surface entirely — `deny` means "this kind exists but the agent
 * never gets to call it without explicit operator opt-in."
 *
 * `intent` (the agent's single-sentence natural-language framing of
 * what it's about to do) is required only on kinds whose default_mode
 * is `ask` — it's what the approval card shows to the user. Auto kinds
 * skip it; if a runtime policy escalates auto → needs_approval and the
 * agent didn't supply intent, we synthesize a fallback from the tool
 * call.
 *
 * Returns null when no kinds are registered (or all are deny) so the
 * caller can omit the server from the MCP map.
 */
function buildGovernedActionServer(
  opts: BuildPluginActionServerOptions | BuildPackActionServerOptions,
  serverName: "neko_plugin_actions" | "neko_pack_actions",
): ReturnType<typeof createMcpServer> | null {
  const active = opts.descriptors.filter((d) => !isDeniedEverywhere(d.default_mode));
  if (active.length === 0) return null;

  const controlPlane = requireControlPlane(opts.controlPlane);

  const tools = active.map((d) => {
    const actionScope = d.scope ?? "external";
    const needsIntent = needsIntentForKind(d.default_mode);
    const baseSchema = {
      target: z
        .string()
        .optional()
        .describe(
          "Canonical resource identifier this action targets (e.g. a channel id, a record id). Optional; some kinds don't need one.",
        ),
      risk_level: z
        .enum(["low", "medium", "high", "critical"])
        .optional()
        .describe(
          "Best-effort blast-radius estimate for this specific invocation. Influences risk-threshold-based policy rules.",
        ),
      payload: z
        .record(z.string(), z.unknown())
        .default({})
        .describe(
          "Action-specific payload. See the tool description for the expected shape. When a trusted discovery tool returns a payload for this action, preserve that complete object exactly unless the operator requested specific changes.",
        ),
    };
    // `intent` lives in both schemas so handler-side narrowing is
    // trivial (one optional property). Ask-mode kinds require it
    // (min length 3); auto-mode kinds make it optional and ignore
    // it unless the runtime policy escalates the call to ask.
    const intentField = needsIntent
      ? z
          .string()
          .min(3)
          .max(500)
          .describe(
            "Single-sentence natural-language description of what you're about to do and why, written for the user reviewing the approval request. e.g. 'Send Sarah the Q2 revenue summary so she has it before tomorrow's board prep.'",
          )
      : z
          .string()
          .min(3)
          .max(500)
          .optional()
          .describe(
            "Optional one-sentence intent. Only surfaced if a runtime policy escalates this call to ask-mode.",
          );
    const schema = {
      intent: intentField,
      ...baseSchema,
    };

    const externalMode = modeForScope(d.default_mode, "external");
    const internalMode = modeForScope(d.default_mode, "internal");
    const modeHint =
      d.default_mode === undefined
        ? "Approval policy decided per-call."
        : typeof d.default_mode === "string"
          ? d.default_mode === "auto"
            ? "Auto-approved by default — runs without user confirmation. The tool waits for execution; use its returned outcome to answer the user. Operators can override via /admin/rules."
            : "Asks the user for approval before running. You MUST set `intent` to a clear one-sentence explanation."
          : [
              externalMode ? `external: ${externalMode}` : null,
              internalMode ? `internal: ${internalMode}` : null,
            ]
              .filter(Boolean)
              .join(", ") +
            (needsIntent
              ? " — set `intent` whenever the call might land in ask-mode."
              : "");

    return defineMcpTool(
      d.kind,
      `${d.description}\n\n${modeHint}`,
      schema,
      async (args) => {
        const decision = await controlPlane.evaluateActionPolicy({
          orgId: opts.orgId,
          scope: actionScope,
          kind: d.kind,
          target:
            typeof args.target === "string" && args.target.length > 0
              ? args.target
              : null,
          payload: (args.payload as Record<string, unknown> | undefined) ?? null,
          riskLevel: (args.risk_level as RiskLevel | undefined) ?? null,
        });

        if (decision.decision === "deny") {
          return {
            content: [
              {
                type: "text" as const,
                text: JSON.stringify({
                  ok: false,
                  decision: "denied",
                  reason: decision.reason,
                  policy: decision.policy.name,
                }),
              },
            ],
          };
        }
        if (decision.decision === "no_policy") {
          return {
            content: [
              {
                type: "text" as const,
                text: JSON.stringify({
                  ok: false,
                  decision: "denied",
                  reason:
                    "no policy matches this scope/kind — refuse for safety. Operator must define a policy in /admin/rules first.",
                }),
              },
            ],
          };
        }

        const intent =
          typeof args.intent === "string" && args.intent.length > 0
            ? args.intent
            : `Auto-fired ${d.kind} (no explicit intent supplied).`;
        const payload =
          (args.payload as Record<string, unknown> | undefined) ?? {};
        const target =
          typeof args.target === "string" && args.target.length > 0
            ? args.target
            : null;
        const riskLevel = (args.risk_level as RiskLevel | undefined) ?? null;

        if (decision.decision === "allow") {
          // Auto-approve path: persist as approved and enqueue the
          // action_execute pg-boss job. We deliberately do NOT call
          // executeApprovedActionRequest inline here — the action
          // adapters are registered in the worker process, not the
          // web process that hosts /work runs. Enqueuing decouples
          // the two: any process can submit the job, only the worker
          // runs it.
          const request = await controlPlane.createActionRequest({
            orgId: opts.orgId,
            scope: actionScope,
            kind: d.kind,
            target,
            payload,
            riskLevel,
            status: "approved",
            policyId: decision.policy.id,
            summary: intent,
            intent,
            workRunId: opts.runId,
            requestedByRunId: null,
          });
          await controlPlane.enqueueActionExecute({
            orgId: opts.orgId,
            actionRequestId: request.id,
          });
          await opts.emit({
            type: "action_request_emit",
            action_request_id: request.id,
            kind: d.kind,
            scope: actionScope,
            decision: "auto_approved",
            summary: intent,
            ...(riskLevel ? { risk_level: riskLevel } : {}),
          });
          const execution = await controlPlane.waitForActionExecution({
            orgId: opts.orgId,
            actionRequestId: request.id,
            timeoutMs: 45_000,
          });
          if (execution.status === "succeeded") {
            return {
              content: [
                {
                  type: "text" as const,
                  text: JSON.stringify({
                    ok: true,
                    decision: "auto_approved",
                    action_request_id: request.id,
                    policy: decision.policy.name,
                    status: "executed",
                    outcome: execution.outcome,
                    note: "Use the returned outcome to answer the user's request now.",
                  }),
                },
              ],
            };
          }
          if (execution.status === "failed") {
            return {
              content: [
                {
                  type: "text" as const,
                  text: JSON.stringify({
                    ok: false,
                    decision: "auto_approved",
                    action_request_id: request.id,
                    policy: decision.policy.name,
                    status: "failed",
                    error: execution.error,
                    note: "Tell the user the action failed. Do not claim that results are still queued.",
                  }),
                },
              ],
            };
          }
          return {
            content: [
              {
                type: "text" as const,
                text: JSON.stringify({
                  ok: true,
                  decision: "auto_approved",
                  action_request_id: request.id,
                  policy: decision.policy.name,
                  status: "running",
                  note: "Execution is still running. Do not claim completion or invent a result; tell the user it is taking longer than expected and the outcome will appear inline.",
                }),
              },
            ],
          };
        }

        // needs_approval — emit a card event, end the agent's turn with
        // a "pending" status. Slice 5 wires the approval click + the
        // follow-up run that threads the outcome back into the
        // conversation.
        const request = await controlPlane.createActionRequest({
          orgId: opts.orgId,
          scope: actionScope,
          kind: d.kind,
          target,
          payload,
          riskLevel,
          status: "pending_approval",
          policyId: decision.policy.id,
          summary: intent,
          intent,
          workRunId: opts.runId,
          requestedByRunId: null,
        });
        await opts.emit({
          type: "action_request_emit",
          action_request_id: request.id,
          kind: d.kind,
          scope: actionScope,
          decision: "pending_approval",
          intent,
          summary: intent,
          ...(riskLevel ? { risk_level: riskLevel } : {}),
        });
        return {
          content: [
            {
              type: "text" as const,
              text: JSON.stringify({
                ok: true,
                decision: "pending_approval",
                action_request_id: request.id,
                policy: decision.policy.name,
                intent,
                note: "User-approval gate. Tell the user briefly what you've requested and end your turn — the approval card is rendered inline; their decision will land in a follow-up turn.",
              }),
            },
          ],
        };
      },
    );
  });

  return createMcpServer({
    name: serverName,
    version: "1.0.0",
    tools,
  });
}

export function buildPluginActionServer(
  opts: BuildPluginActionServerOptions,
): ReturnType<typeof createMcpServer> | null {
  return buildGovernedActionServer(opts, "neko_plugin_actions");
}

export function buildPackActionServer(
  opts: BuildPackActionServerOptions,
): ReturnType<typeof createMcpServer> | null {
  return buildGovernedActionServer(opts, "neko_pack_actions");
}

/** Semantic skill search for the Ax harness's discover(); it is never a model tool. */
export function buildSkillSearchServer(
  ctx: { orgId: string; runId: string },
  options: { controlPlane?: AgentControlPlane } = {},
) {
  const controlPlane = requireControlPlane(options.controlPlane);
  const find = defineMcpTool(
    "find",
    "Find the installed skill guides that best match the work described.",
    { query: z.string().min(2).max(800), limit: z.number().int().min(1).max(10).optional() },
    async (args) => {
      const skills = await controlPlane.searchSkillsForRun({
        orgId: ctx.orgId,
        runId: ctx.runId,
        query: args.query,
        ...(args.limit ? { limit: args.limit } : {}),
      });
      return { content: [{ type: "text" as const, text: JSON.stringify({ skills }) }] };
    },
  );
  return createMcpServer({ name: "neko_guides", version: "1.0.0", tools: [find] });
}
