import { startupElapsedMs } from "@neko/telemetry/startup";
import type { HarnessObserver } from "@neko/telemetry";
import { observeSafely } from "@neko/telemetry";
import type { AgentEvent } from "../agent-backend";
import { normalizeGraphjinAgentUsage } from "../usage-normalization";

type ObservationStatus = "ok" | "error";
type FailureDetail = { errorType?: string; errorCode?: string; errorMessage?: string };

function failureDetail(result: FailureDetail): FailureDetail {
  return {
    ...(result.errorType ? { errorType: result.errorType } : {}),
    ...(result.errorCode ? { errorCode: result.errorCode } : {}),
    ...(result.errorMessage ? { errorMessage: result.errorMessage } : {}),
  };
}

function byteLength(value: unknown): number {
  if (value === undefined) return 0;
  try {
    return Math.min(
      Buffer.byteLength(
        typeof value === "string" ? value : JSON.stringify(value),
        "utf8",
      ),
      1024 * 1024 * 1024,
    );
  } catch {
    return 0;
  }
}

function isGraphjinAgentTool(name: string): boolean {
  return name.toLocaleLowerCase().includes("neko_graphjin_agent");
}

/**
 * One metadata-only instrumentation layer for Ask and workflow agent loops.
 * It consumes AgentEvent shapes but never forwards their content-bearing
 * fields to the observer.
 */
export function createAgentEventTelemetry(input: {
  observer?: HarnessObserver;
  operationId: string;
}) {
  const stageOperationId = `${input.operationId}:agent`;
  const modelOperationId = `${input.operationId}:model:1`;
  const toolStarts = new Map<string, { name: string; startedAt: number }>();
  let agentStartedAt = Date.now();
  let firstOutputObserved = false;
  let stageOpen = false;
  let modelOpen = false;
  let retries = 0;
  let outerUsage: Extract<AgentEvent, { type: "usage" }> | undefined;

  const observe = async (
    observation: Parameters<HarnessObserver["observe"]>[0],
  ): Promise<void> => observeSafely(input.observer, observation);

  const observeEvent = async (event: AgentEvent): Promise<void> => {
    if (
      !firstOutputObserved &&
      ((event.type === "message" && event.role === "assistant") ||
        event.type === "surface")
    ) {
      firstOutputObserved = true;
      const firstOutputMs = Date.now() - agentStartedAt;
      await observe({
        kind: "model.first_chunk",
        attributes: { "openneko.timing.basis": "agent_first_output", "openneko.timing.provider_ttft": false },
        operationId: modelOperationId,
        parentOperationId: stageOperationId,
        measurements: { firstOutputMs, coverage: "unavailable" },
      });
      await observe({
        kind: "run.first_output",
        operationId: input.operationId,
        measurements: { firstOutputMs: startupElapsedMs() ?? firstOutputMs, coverage: "unavailable" },
      });
    }
    if (event.type === "tool_start") {
      toolStarts.set(event.id, { name: event.name, startedAt: Date.now() });
      await observe({
        kind: "tool.start",
        operationId: `${input.operationId}:tool:${event.id}`,
        parentOperationId: modelOperationId,
        attributes: { "gen_ai.tool.name": event.name },
        measurements: {
          inputBytes: byteLength(event.input),
          coverage: "unavailable",
        },
      });
      if (isGraphjinAgentTool(event.name)) {
        await observe({
          kind: "delegation.start",
          operationId: `${input.operationId}:delegation:${event.id}`,
          parentOperationId: modelOperationId,
          attributes: { "openneko.delegation.target": "graphjin-agent" },
        });
        await observe({
          kind: "model.request",
          operationId: `${input.operationId}:inner-model:${event.id}`,
          parentOperationId: `${input.operationId}:delegation:${event.id}`,
          attributes: { "openneko.model.scope": "inner" },
        });
      }
      return;
    }
    if (event.type === "tool_end") {
      const started = toolStarts.get(event.id);
      toolStarts.delete(event.id);
      await observe({
        kind: "tool.end",
        operationId: `${input.operationId}:tool:${event.id}`,
        parentOperationId: modelOperationId,
        status: event.error ? "error" : "ok",
        ...(event.error ? { errorType: "tool_error" } : {}),
        attributes: { "gen_ai.tool.name": started?.name ?? "unknown" },
        measurements: {
          ...(started ? { durationMs: Date.now() - started.startedAt } : {}),
          outputBytes: byteLength(event.result ?? event.error),
          coverage: "unavailable",
        },
      });
      if (started && isGraphjinAgentTool(started.name)) {
        const inner = normalizeGraphjinAgentUsage(event.result);
        await observe({
          kind: "model.response",
          operationId: `${input.operationId}:inner-model:${event.id}`,
          parentOperationId: `${input.operationId}:delegation:${event.id}`,
          status: event.error ? "error" : "ok",
          ...(event.error ? { errorType: "graphjin_agent_error" } : {}),
          attributes: {
            "openneko.model.scope": "inner",
            ...(inner?.provider
              ? { "gen_ai.provider.name": inner.provider }
              : {}),
            ...(inner?.model ? { "gen_ai.response.model": inner.model } : {}),
          },
          measurements: inner?.usage ?? {
            coverage: "unavailable",
            missingReasons: ["GraphJin agent response omitted normalized usage"],
          },
        });
        await observe({
          kind: "delegation.end",
          operationId: `${input.operationId}:delegation:${event.id}`,
          parentOperationId: modelOperationId,
          status: event.error ? "error" : "ok",
          ...(event.error ? { errorType: "graphjin_agent_error" } : {}),
          attributes: { "openneko.delegation.target": "graphjin-agent" },
        });
      }
      return;
    }
    if (event.type === "usage" && event.source === "outer") {
      outerUsage = event;
      return;
    }
    if (event.type === "retry") {
      await observe({
        kind: "retry",
        operationId: `${input.operationId}:retry:${++retries}`,
        parentOperationId: modelOperationId,
        attributes: { "openneko.retry.reason": event.reason },
      });
      return;
    }
    if (event.type === "action_request_emit") {
      await observe({
        kind: "policy.decision",
        operationId: `${input.operationId}:policy:${event.action_request_id}`,
        parentOperationId: input.operationId,
        status: "ok",
        attributes: {
          "openneko.policy.decision": event.decision,
          "openneko.action.kind": event.kind,
          "openneko.action.scope": event.scope,
        },
      });
      return;
    }
    if (event.type === "action_request_result") {
      await observe({
        kind: "approval.decision",
        operationId: `${input.operationId}:action:${event.action_request_id}`,
        parentOperationId: input.operationId,
        status: event.status === "failed" || event.status === "partially_applied" || event.status === "reconcile_required" ? "error" : "ok",
        attributes: {
          "openneko.action.kind": event.kind,
          "openneko.action.status": event.status,
        },
      });
    }
  };

  const startAgent = async (metadata: {
    backend: string;
    model?: string;
    inputBytes?: number;
  }): Promise<void> => {
    agentStartedAt = Date.now();
    await observe({
      kind: "stage.start",
      operationId: stageOperationId,
      parentOperationId: input.operationId,
      attributes: { "openneko.stage": "agent" },
    });
    stageOpen = true;
    await observe({
      kind: "model.request",
      operationId: modelOperationId,
      parentOperationId: stageOperationId,
      attributes: {
        "openneko.model.scope": "outer",
        "openneko.backend": metadata.backend,
        ...(metadata.model ? { "gen_ai.request.model": metadata.model } : {}),
      },
      ...(metadata.inputBytes !== undefined
        ? {
            measurements: {
              inputBytes: Math.max(0, metadata.inputBytes),
              coverage: "unavailable" as const,
            },
          }
        : {}),
    });
    modelOpen = true;
  };

  const finishAgent = async (result: FailureDetail & {
    status: ObservationStatus;
    timedOut?: boolean;
    degraded?: boolean;
    outputBytes?: number;
  }): Promise<void> => {
    await observe({
      kind: "model.response",
      operationId: modelOperationId,
      parentOperationId: stageOperationId,
      status: result.status,
      ...(result.errorType ? { errorType: result.errorType } : {}),
      attributes: {
        "openneko.model.scope": "outer",
        ...(outerUsage?.provider
          ? { "gen_ai.provider.name": outerUsage.provider }
          : {}),
        ...(outerUsage?.model
          ? { "gen_ai.response.model": outerUsage.model }
          : {}),
      },
      measurements: {
        ...(outerUsage?.usage ?? {
          coverage: "unavailable" as const,
          missingReasons: ["backend emitted no normalized usage"],
        }),
        ...(result.outputBytes !== undefined
          ? { outputBytes: Math.max(0, result.outputBytes) }
          : {}),
      },
    });
    modelOpen = false;
    await observe({
      kind: "stage.end",
      operationId: stageOperationId,
      parentOperationId: input.operationId,
      status: result.status,
      ...failureDetail(result),
      attributes: {
        "openneko.stage": "agent",
        ...(result.timedOut ? { "openneko.timed_out": true } : {}),
        ...(result.degraded ? { "openneko.outcome": "degraded" } : {}),
      },
      measurements: {
        durationMs: Date.now() - agentStartedAt,
        coverage: "unavailable",
      },
    });
    stageOpen = false;
  };

  const closeOpen = async (result: FailureDetail & {
    status: ObservationStatus;
    outcome: string;
    usageMissingReason: string;
  }): Promise<void> => {
    for (const [toolId, tool] of toolStarts) {
      if (isGraphjinAgentTool(tool.name)) {
        await observe({
          kind: "model.response",
          operationId: `${input.operationId}:inner-model:${toolId}`,
          parentOperationId: `${input.operationId}:delegation:${toolId}`,
          status: result.status,
          ...(result.errorType ? { errorType: result.errorType } : {}),
          measurements: {
            coverage: "unavailable",
            missingReasons: [result.usageMissingReason],
          },
        });
        await observe({
          kind: "delegation.end",
          operationId: `${input.operationId}:delegation:${toolId}`,
          parentOperationId: modelOperationId,
          status: result.status,
          ...(result.errorType ? { errorType: result.errorType } : {}),
          attributes: { "openneko.delegation.target": "graphjin-agent" },
        });
      }
      await observe({
        kind: "tool.end",
        operationId: `${input.operationId}:tool:${toolId}`,
        parentOperationId: modelOperationId,
        status: result.status,
        ...(result.errorType ? { errorType: result.errorType } : {}),
        attributes: { "gen_ai.tool.name": tool.name },
        measurements: {
          durationMs: Date.now() - tool.startedAt,
          coverage: "unavailable",
        },
      });
    }
    toolStarts.clear();
    if (modelOpen) {
      await observe({
        kind: "model.response",
        operationId: modelOperationId,
        parentOperationId: stageOperationId,
        status: result.status,
        ...(result.errorType ? { errorType: result.errorType } : {}),
        attributes: { "openneko.outcome": result.outcome },
        measurements: outerUsage?.usage ?? {
          coverage: "unavailable",
          missingReasons: [result.usageMissingReason],
        },
      });
      modelOpen = false;
    }
    if (stageOpen) {
      await observe({
        kind: "stage.end",
        operationId: stageOperationId,
        parentOperationId: input.operationId,
        status: result.status,
        ...failureDetail(result),
        attributes: {
          "openneko.stage": "agent",
          "openneko.outcome": result.outcome,
        },
        measurements: {
          durationMs: Date.now() - agentStartedAt,
          coverage: "unavailable",
        },
      });
      stageOpen = false;
    }
    if (result.status === "error") {
      await observe({
        kind: "error",
        operationId: `${input.operationId}:error`,
        parentOperationId: input.operationId,
        status: "error",
        ...failureDetail(result),
        attributes: { "openneko.outcome": result.outcome },
      });
    }
  };

  return { observeEvent, startAgent, finishAgent, closeOpen };
}
