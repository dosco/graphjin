import type { AgentSurfaceMessage } from "../agent-backend";
import type { ActionPolicyRecord } from "./action-store";
import type { SubscriptionRecord, WorkflowRecord } from "./store";
import type { SourceChangeFilter } from "./subscription-query";

function confirmationCard(args: {
  surfaceId: string;
  label: string;
  title: string;
  body: string;
}): AgentSurfaceMessage[] {
  return [
    {
      version: "v1.0",
      createSurface: {
        surfaceId: args.surfaceId,
        catalogId: "urn:app:catalog:briefing:v1",
      },
    },
    {
      version: "v1.0",
      updateComponents: {
        surfaceId: args.surfaceId,
        components: [
          {
            id: "root",
            component: "Confirmation",
            label: args.label,
            title: args.title,
            children: ["body"],
          },
          { id: "body", component: "Markdown", text: args.body },
        ],
      },
    },
  ];
}

export function workflowSavedCard(args: {
  workflow: Pick<
    WorkflowRecord,
    "id" | "name" | "description" | "cron" | "cronTimezone" | "steps"
  >;
  action: "created" | "updated";
}): AgentSurfaceMessage[] {
  const verb = args.action === "created" ? "Created" : "Updated";
  const cron = args.workflow.cron
    ? ` (cron \`${args.workflow.cron}\` ${args.workflow.cronTimezone})`
    : "";
  return confirmationCard({
    surfaceId: `workflow-save-${args.workflow.id}`,
    label: `${verb} workflow`,
    title: args.workflow.name,
    body: [
      `**${args.workflow.name}** — ${args.workflow.steps.length} step(s)${cron}.`,
      "",
      args.workflow.description || "_No description._",
      "",
      `[Open detail](/workflows?id=${args.workflow.id})`,
    ].join("\n"),
  });
}

export function workflowDeletedCard(args: {
  id: string;
  name: string;
}): AgentSurfaceMessage[] {
  return confirmationCard({
    surfaceId: `workflow-delete-${args.id}`,
    label: "Deleted workflow",
    title: args.name,
    body: [
      `**${args.name}** is gone, along with its triggers, run history, and proposed actions.`,
      "",
      "[Open workflows](/workflows)",
    ].join("\n"),
  });
}

export function subscriptionSavedCard(args: {
  subscription: Pick<
    SubscriptionRecord,
    "id" | "workflowId" | "filter" | "idempotencyKeyTemplate"
  >;
  workflowName: string;
}): AgentSurfaceMessage[] {
  const filter = args.subscription.filter as Partial<SourceChangeFilter>;
  const table = typeof filter.table === "string" ? filter.table : "?";
  const pkList = Array.isArray(filter.primary_key)
    ? filter.primary_key.join(", ")
    : "?";
  const whereSummary =
    filter.where && Object.keys(filter.where).length > 0
      ? "`" +
        Object.keys(filter.where as Record<string, unknown>).join("`, `") +
        "`"
      : "_(no filter)_";
  return confirmationCard({
    surfaceId: `subscription-save-${args.subscription.id}`,
    label: "Trigger added",
    title: args.workflowName,
    body: [
      `**${args.workflowName}** will fire on changes to \`${table}\` (pk: \`${pkList}\`).`,
      "",
      `**Filter columns:** ${whereSummary}`,
      args.subscription.idempotencyKeyTemplate
        ? `**Idempotency key:** \`${args.subscription.idempotencyKeyTemplate}\``
        : null,
      "",
      `[Open detail](/workflows?id=${args.subscription.workflowId})`,
    ]
      .filter((s) => s !== null)
      .join("\n"),
  });
}

export function policySavedCard(args: {
  policy: Pick<
    ActionPolicyRecord,
    | "id"
    | "name"
    | "description"
    | "mode"
    | "appliesToKinds"
    | "appliesToScopes"
    | "priority"
  >;
  action: "created" | "updated";
}): AgentSurfaceMessage[] {
  const verb = args.action === "created" ? "Created" : "Updated";
  const scopes = args.policy.appliesToScopes.join(", ") || "—";
  const kinds =
    args.policy.appliesToKinds.length > 0
      ? args.policy.appliesToKinds.map((k) => `\`${k}\``).join(", ")
      : "_(all kinds)_";
  return confirmationCard({
    surfaceId: `policy-save-${args.policy.id}`,
    label: `${verb} rule`,
    title: args.policy.name,
    body: [
      `**Mode:** \`${args.policy.mode}\` • **Scopes:** ${scopes} • **Priority:** ${args.policy.priority}`,
      "",
      `**Kinds:** ${kinds}`,
      "",
      args.policy.description || "_No description._",
      "",
      `[Open detail](/admin/rules/${args.policy.id})`,
    ].join("\n"),
  });
}
