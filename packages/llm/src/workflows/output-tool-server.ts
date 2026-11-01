import type { AgentEvent, OutputMood } from "../agent-backend";
import { createMcpServer, defineMcpTool } from "../mcp-server";
import type { AgentControlPlane } from "../work/control-plane";
import {
  WORKFLOW_OUTPUT_SCHEMA,
  type WorkflowOutputPayload,
} from "./fence-schemas";

export type WorkflowOutputToolContext = {
  orgId: string;
  workflowRunId: string;
  workRunId: string;
  emit: (event: AgentEvent) => Promise<void> | void;
  controlPlane: Pick<AgentControlPlane, "emitWorkflowOutput">;
};

export function buildWorkflowOutputServer(ctx: WorkflowOutputToolContext) {
  const emitOutput = defineMcpTool(
    "emit",
    [
      "Persist a workflow output — the thing this run produced.",
      "Use a descriptive kind and tag it with a scope, optional topic,",
      "and an honest mood ('good', 'watch', or 'act') so people and",
      "downstream workflows can discover it.",
    ].join(" "),
    WORKFLOW_OUTPUT_SCHEMA.shape,
    async (args: WorkflowOutputPayload) => {
      const output = await ctx.controlPlane.emitWorkflowOutput({
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
        timeWindowEnd: args.timeWindowEnd ? new Date(args.timeWindowEnd) : null,
        freshnessTtlSeconds: args.freshnessTtlSeconds ?? null,
      });
      await ctx.emit({
        type: "output_emit",
        output_id: output.id,
        kind: output.kind,
      });
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
