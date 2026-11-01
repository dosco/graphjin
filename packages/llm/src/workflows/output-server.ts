import { createMcpServer, defineMcpTool } from "../mcp-server";
import type { AgentEvent, OutputMood } from "../agent-backend";
import type { AgentControlPlane, Wire } from "../work/control-plane";
import {
  WORKFLOW_OUTPUT_SCHEMA,
  type WorkflowOutputPayload,
} from "./fence-schemas";
import { notifyWorkflowOutputDeliveryHook } from "./output-delivery";
import {
  emitWorkflowOutput,
  type WorkflowOutputInput,
  type WorkflowOutputRecord,
} from "./store";

export {
  setWorkflowOutputDeliveryHook,
  type WorkflowOutputDeliveryHook,
} from "./output-delivery";

export type WorkflowOutputContext = {
  orgId: string;
  workflowRunId: string;
  workRunId: string;
  emit: (event: AgentEvent) => Promise<void> | void;
  /** In-process on host; broker-backed inside the agent sandbox. */
  controlPlane?: Pick<AgentControlPlane, "emitWorkflowOutput">;
};

/**
 * Shared handler. The MCP tool and the fence-fallback path both route
 * here so persistence + the emit event happen in one place.
 */
export async function handleWorkflowOutput(
  ctx: WorkflowOutputContext,
  args: WorkflowOutputPayload,
): Promise<WorkflowOutputRecord | Wire<WorkflowOutputRecord>> {
  const input: WorkflowOutputInput = {
    orgId: ctx.orgId,
    workflowRunId: ctx.workflowRunId,
    workRunId: ctx.workRunId,
    kind: args.kind,
    title: args.title,
    body: args.body,
    payload: args.payload,
    artifactPath: args.artifactPath ?? null,
    scope: args.scope ?? null,
    topic: args.topic ?? null,
    mood: (args.mood ?? null) as OutputMood | null,
    timeWindowStart: args.timeWindowStart
      ? new Date(args.timeWindowStart)
      : null,
    timeWindowEnd: args.timeWindowEnd
      ? new Date(args.timeWindowEnd)
      : null,
    freshnessTtlSeconds: args.freshnessTtlSeconds ?? null,
  };
  let output: WorkflowOutputRecord | Wire<WorkflowOutputRecord>;
  if (ctx.controlPlane) {
    output = await ctx.controlPlane.emitWorkflowOutput(input);
  } else {
    output = await emitWorkflowOutput(input);
    notifyWorkflowOutputDeliveryHook(ctx.orgId, output);
  }
  await ctx.emit({
    type: "output_emit",
    output_id: output.id,
    kind: output.kind,
  });
  return output;
}

export function buildWorkflowOutputServer(ctx: WorkflowOutputContext) {
  const emitOutput = defineMcpTool(
    "emit",
    [
      "Persist a workflow output — the thing this run produced. Most",
      "workflow value is non-mutating, so emit outputs liberally rather",
      "than reaching for state-changing actions.",
      "",
      "Use `kind` to describe the shape (`report`, `finding`,",
      "`observation`, `recommendation`, `briefing_card_proposal`, ...).",
      "Tag every output with a `scope` (e.g. 'apac_churn', 'inventory_risk'),",
      "optionally a more specific `topic`, and a `mood` ('good', 'watch',",
      "or 'act'). Other workflows subscribe by scope/mood and humans browse",
      "by them, so honest tagging is what makes the output discoverable.",
      "",
      "Example for an observe-and-report run:",
      "  kind: 'observation', scope: 'apac_churn', mood: 'watch',",
      "  title: 'APAC churn rose 18% WoW', body: '...'",
    ].join(" "),
    WORKFLOW_OUTPUT_SCHEMA.shape,
    async (args) => {
      const output = await handleWorkflowOutput(ctx, args);
      return {
        content: [
          {
            type: "text" as const,
            text: JSON.stringify({
              ok: true,
              outputId: output.id,
              kind: output.kind,
            }),
          },
        ],
      };
    },
  );

  return createMcpServer({
    name: "neko_workflow_output",
    version: "1.0.0",
    tools: [emitOutput],
  });
}
