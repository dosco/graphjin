import { createMcpServer, defineMcpTool } from "../mcp-server";
import { z } from "zod";
import type { AgentEvent } from "../agent-backend";
import type { AgentControlPlane } from "../work/control-plane";
import type {
  ActionPolicyMode,
  ActionScope,
  RiskLevel,
} from "./action-store";
import { policySavedCard } from "./builder-cards";
import { POLICY_SAVE_SCHEMA } from "./fence-schemas";

export type RuleBuilderContext = {
  orgId: string;
  createdByThreadId?: string | null;
  createdByRunId?: string | null;
  emit?: (event: AgentEvent) => Promise<void> | void;
  /** In-process on the host; broker-backed inside the agent sandbox. The
   *  tool handlers run wherever the backend SDK runs, so they must never
   *  touch the DB directly. */
  controlPlane: AgentControlPlane;
};

export function buildRuleBuilderServer(ctx: RuleBuilderContext) {
  const controlPlane = ctx.controlPlane;

  const saveRuleTool = defineMcpTool(
    "save_rule",
    [
      "Create or update an approval rule. Upserts by name within the org —",
      "if a rule with the same name already exists, it is updated in place.",
      "Use this when the operator asks to gate, allow, or deny specific action",
      "kinds (e.g. 'auto-approve low-risk slack posts', 'always ask before",
      "sending email externally'). To send without approval only to approved",
      "recipients, use allowed_targets with on_miss 'next' so any other",
      "recipient falls to the rule that asks. After saving, the tool emits a",
      "confirmation card the operator can click to open the rule.",
    ].join(" "),
    POLICY_SAVE_SCHEMA.shape,
    async (args) => {
      const result = await controlPlane.upsertActionPolicyByName({
        orgId: ctx.orgId,
        name: args.name,
        description: args.description ?? "",
        appliesToKinds: args.applies_to_kinds,
        appliesToScopes: args.applies_to_scopes as ActionScope[],
        mode: args.mode as ActionPolicyMode,
        riskThresholdAutoApprove:
          (args.risk_threshold_auto_approve as RiskLevel | undefined) ?? null,
        allowedTargets: args.allowed_targets ?? null,
        deniedTargets: args.denied_targets ?? null,
        limits: args.limits,
        approverRole: args.approver_role === "admin" ? "admin" : null,
        priority: args.priority,
        enabled: args.enabled,
        createdByThreadId: ctx.createdByThreadId ?? null,
        createdByRunId: ctx.createdByRunId ?? null,
      });
      if (ctx.emit) {
        await ctx.emit({
          type: "surface",
          messages: policySavedCard({
            policy: result.policy,
            action: result.action,
          }),
        });
      }
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify({
              ok: true,
              action: result.action,
              ruleId: result.policy.id,
              name: result.policy.name,
            }),
          },
        ],
      };
    },
  );

  const listRulesTool = defineMcpTool(
    "list_rules",
    [
      "List the approval rules defined in this org so you can answer questions",
      "like 'what was that rule we set last week to auto-approve slack posts'",
      "or look up the exact name/config of a rule before updating it via",
      "`save_rule`. Returns full bodies (mode, scopes, kinds, limits), ordered",
      "by priority then name. Includes disabled rules.",
    ].join(" "),
    {
      limit: z.number().int().min(1).max(200).optional(),
    },
    async (args) => {
      const { total, policies } = await controlPlane.listActionPolicies({
        orgId: ctx.orgId,
        limit: args.limit ?? 50,
      });
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify({
              ok: true,
              total,
              returned: policies.length,
              rules: policies.map((p) => ({
                id: p.id,
                name: p.name,
                description: p.description,
                mode: p.mode,
                appliesToKinds: p.appliesToKinds,
                appliesToScopes: p.appliesToScopes,
                riskThresholdAutoApprove: p.riskThresholdAutoApprove,
                allowedTargets: p.allowedTargets,
                deniedTargets: p.deniedTargets,
                limits: p.limits,
                approverGroupId: p.approverGroupId,
                priority: p.priority,
                enabled: p.enabled,
                updatedAt: p.updatedAt,
                createdAt: p.createdAt,
              })),
            }),
          },
        ],
      };
    },
  );

  return createMcpServer({
    name: "neko_rule_builder",
    version: "1.0.0",
    tools: [saveRuleTool, listRulesTool],
  });
}
