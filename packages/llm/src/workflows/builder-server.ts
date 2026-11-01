import { createMcpServer, defineMcpTool } from "../mcp-server";
import { z } from "zod";
import type { AgentEvent } from "../agent-backend";
import type { AgentControlPlane } from "../work/control-plane";
import {
  subscriptionSavedCard,
  workflowDeletedCard,
  workflowSavedCard,
} from "./builder-cards";
import { WORKFLOW_SAVE_SCHEMA } from "./fence-schemas";
import { parseCompiledWorkflowBatchContract } from "./api-contract";

export type WorkflowBuilderContext = {
  orgId: string;
  createdByThreadId?: string | null;
  createdByRunId?: string | null;
  emit?: (event: AgentEvent) => Promise<void> | void;
  /** In-process on the host; broker-backed inside the agent sandbox. The
   *  tool handlers run wherever the backend SDK runs, so they must never
   *  touch the DB directly. */
  controlPlane: AgentControlPlane;
};

export function buildWorkflowBuilderServer(ctx: WorkflowBuilderContext) {
  const controlPlane = ctx.controlPlane;

  const createWorkflowTool = defineMcpTool(
    "create_workflow",
    [
      "Create or update a workflow. Upserts by name within the org — if a",
      "workflow with the same name already exists, it is updated in place.",
      "Use this when the operator asks to set up, modify, or rename a task or",
      "pipeline. Pass `triggers.cron` to run it on a schedule, or",
      "pass `triggers.watch` for a periodic scalar or aggregate condition.",
      "For a watch, first execute the GraphJin query successfully, then copy",
      "that exact query and derive `value_path` from its response data using",
      "dot-separated segments (including numeric array indexes such as `.0.`).",
      "Do not use bracket notation. `triggers.when` is instead for matching",
      "changed source rows and requires table/primary-key semantics.",
      "pass `batch` to define the workflow's own record field and CSV output",
      "columns for batch API execution. Batch support belongs to the workflow",
      "and does not require a skill (a workflow may use zero, one, or many).",
      "Pass `triggers.when` to fire it when a row in the operator's data source",
      "matches a filter (e.g. 'when stock dips below reorder point') — for the",
      "latter, introspect the schema first with the GraphJin MCP",
      "(`list_tables`, `describe_table`) to confirm the table, columns, and",
      "primary key. After saving, narrate the change in a sentence; the tool",
      "also emits a confirmation card the operator can click.",
    ].join(" "),
    WORKFLOW_SAVE_SCHEMA.shape,
    async (args) => {
      const result = await controlPlane.saveWorkflowWithTrigger({
        orgId: ctx.orgId,
        name: args.name,
        description: args.description,
        goal: args.goal,
        systemPromptOverlay: args.systemPromptOverlay,
        steps: args.steps,
        batch: args.batch,
        triggers: args.triggers,
        createdByThreadId: ctx.createdByThreadId ?? null,
        createdByRunId: ctx.createdByRunId ?? null,
      });
      if (ctx.emit) {
        await ctx.emit({
          type: "surface",
          messages: workflowSavedCard({
            workflow: result.workflow,
            action: result.action,
          }),
        });
        if (result.subscription) {
          await ctx.emit({
            type: "surface",
            messages: subscriptionSavedCard({
              subscription: result.subscription,
              workflowName: result.workflow.name,
            }),
          });
        }
      }
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify({
              ok: !result.triggerError,
              action: result.action,
              workflowId: result.workflow.id,
              name: result.workflow.name,
              ...(result.subscription
                ? { triggerId: result.subscription.id }
                : {}),
              ...(result.triggerError
                ? {
                    triggerError: result.triggerError,
                    hint: "The workflow was saved but the data trigger was not wired. Fix the trigger and call create_workflow again with the same name.",
                  }
                : {}),
            }),
          },
        ],
        ...(result.triggerError ? { isError: true } : {}),
      };
    },
  );

  const listWorkflowsTool = defineMcpTool(
    "list_workflows",
    [
      "List the workflows defined in this org so you can answer questions",
      "like 'what was the workflow we set up last week to summarize sales' or",
      "look up the exact name/steps of a workflow before updating it via",
      "`create_workflow`. Returns full bodies (steps, cron, data trigger,",
      "description, system prompt overlay), ordered by most recently updated.",
    ].join(" "),
    {
      limit: z.number().int().min(1).max(200).optional(),
    },
    async (args) => {
      const { total, workflows } = await controlPlane.listWorkflowsWithTriggers(
        {
          orgId: ctx.orgId,
          limit: args.limit ?? 50,
          runId: ctx.createdByRunId ?? null,
        },
      );
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify({
              ok: true,
              total,
              returned: workflows.length,
              workflows: workflows.map((w) => {
                const batch = parseCompiledWorkflowBatchContract(w.outputContract);
                return {
                  id: w.id,
                  name: w.name,
                  description: w.description,
                  enabled: w.enabled,
                  status: w.status,
                  goal: w.goal,
                  systemPromptOverlay: w.systemPromptOverlay,
                  steps: w.steps,
                  batch: batch
                    ? {
                        recordsField: batch.recordsField,
                        columns: batch.columns,
                      }
                    : null,
                  cron: w.cron,
                  cronTimezone: w.cronTimezone,
                  cronEnabled: w.cronEnabled,
                  when: w.when,
                  updatedAt: w.updatedAt,
                  createdAt: w.createdAt,
                };
              }),
            }),
          },
        ],
      };
    },
  );

  const deleteWorkflowTool = defineMcpTool(
    "delete_workflow",
    [
      "Permanently delete a workflow by its id. This also drops its triggers,",
      "run history, outputs, and proposed/executed actions (hard cascade — no",
      "undo). Because it is destructive, you MUST confirm with the operator in",
      "the thread BEFORE calling this — name the workflow you're about to",
      "remove and wait for an explicit yes. Resolve the id first with",
      "`list_workflows` (match on the name the operator referenced or the id",
      "carried by an @mention); never guess. On success the tool emits a",
      "confirmation card.",
    ].join(" "),
    {
      workflowId: z
        .string()
        .min(1)
        .describe("The id of the workflow to delete (from list_workflows)."),
    },
    async (args) => {
      const result = await controlPlane.deleteWorkflow({
        orgId: ctx.orgId,
        workflowId: args.workflowId,
        runId: ctx.createdByRunId ?? null,
      });
      if (!result.found) {
        return {
          content: [
            {
              type: "text" as const,
              text: JSON.stringify({
                ok: false,
                error: "not_found",
                hint: "No workflow with that id in this org. Call list_workflows to get the current ids.",
              }),
            },
          ],
          isError: true,
        };
      }
      if (ctx.emit && result.name) {
        await ctx.emit({
          type: "surface",
          messages: workflowDeletedCard({
            id: args.workflowId,
            name: result.name,
          }),
        });
      }
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify({
              ok: true,
              deleted: { id: args.workflowId, name: result.name },
            }),
          },
        ],
      };
    },
  );

  return createMcpServer({
    name: "neko_workflow_builder",
    version: "1.0.0",
    tools: [createWorkflowTool, listWorkflowsTool, deleteWorkflowTool],
  });
}
