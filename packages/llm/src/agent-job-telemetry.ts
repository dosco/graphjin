import { errorCodeOf, observeSafely, type HarnessObserver } from "@neko/telemetry";
import type { AgentBackend, AgentRunResult } from "./agent-backend";

/**
 * The run.start/run.end pair for a one-shot agent job. A failure carries the
 * backend's own code when a wrapped backend reported one.
 */
export async function observeAgentJob<T>(
  input: {
    observer?: HarnessObserver;
    operationId: string;
    productPath: string;
    attributes?: Record<string, unknown>;
  },
  run: (observed: (backend: AgentBackend) => AgentBackend) => Promise<T>,
): Promise<T> {
  const { observer, operationId } = input;
  const startedAt = Date.now();
  let backendAttributes: Record<string, string> = {};
  let failure: Pick<AgentRunResult, "errorCode" | "timedOut"> | undefined;
  const observed = (backend: AgentBackend): AgentBackend => {
    backendAttributes = {
      "openneko.backend": backend.id,
      ...(backend.model ? { "gen_ai.request.model": backend.model } : {}),
    };
    return {
      id: backend.id,
      capabilities: backend.capabilities,
      configuredIdentity: backend.configuredIdentity,
      model: backend.model,
      async run(options) {
        const result = await backend.run(options);
        failure = result.status === "failed"
          ? { errorCode: result.errorCode, timedOut: result.timedOut }
          : undefined;
        return result;
      },
    };
  };
  await observeSafely(observer, {
    kind: "run.start",
    timestamp: new Date(startedAt).toISOString(),
    operationId,
    attributes: {
      "openneko.run.kind": "production",
      "openneko.product.path": input.productPath,
      ...input.attributes,
    },
  });
  try {
    const value = await run(observed);
    await observeSafely(observer, {
      kind: "run.end",
      operationId,
      status: "ok",
      attributes: { ...backendAttributes, "openneko.outcome": "completed" },
      measurements: { durationMs: Date.now() - startedAt, coverage: "unavailable" },
    });
    return value;
  } catch (cause) {
    const errorCode = failure?.errorCode ?? errorCodeOf(cause);
    const detail = {
      errorType: failure?.errorCode ?? (cause instanceof Error ? cause.name : "unknown"),
      ...(errorCode ? { errorCode } : {}),
      errorMessage: cause instanceof Error ? cause.message : String(cause),
    };
    await observeSafely(observer, {
      kind: "error",
      operationId: `${operationId}:error`,
      parentOperationId: operationId,
      status: "error",
      ...detail,
    });
    await observeSafely(observer, {
      kind: "run.end",
      operationId,
      status: "error",
      ...detail,
      attributes: {
        ...backendAttributes,
        "openneko.outcome": "failed",
        ...(failure?.timedOut ? { "openneko.timed_out": true } : {}),
      },
      measurements: { durationMs: Date.now() - startedAt, coverage: "unavailable" },
    });
    throw cause;
  }
}
