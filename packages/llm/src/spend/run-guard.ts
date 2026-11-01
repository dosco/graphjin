import { pool } from "@neko/db";
import type { AgentEvent } from "../agent-backend";
import { priceUsage } from "./ledger";
import { microsToUsd } from "./limits";

export class SpendCapExceeded extends Error {
  readonly code = "spend_limit";
  constructor(
    readonly capUsd: number,
    readonly spentUsd: number,
  ) {
    super(`The run exceeded its $${capUsd.toFixed(2)} spend cap ($${spentUsd.toFixed(2)} spent).`);
    this.name = "SpendCapExceeded";
  }
}

export function spendCapFromSignal(signal: AbortSignal | undefined): SpendCapExceeded | null {
  return signal?.aborted && signal.reason instanceof SpendCapExceeded ? signal.reason : null;
}

export type RunSpendGuard = {
  emit: (event: AgentEvent) => Promise<void>;
  signal: AbortSignal;
  exceeded: () => SpendCapExceeded | null;
  dispose: () => void;
};

function pricedMicros(usage: Parameters<typeof priceUsage>[0]): number | null {
  const hasPrice =
    usage.costStatus !== "unknown" &&
    (typeof usage.billedCostUsd === "number" || typeof usage.estimatedCostUsd === "number");
  return hasPrice ? priceUsage(usage, 0).costMicros : null;
}

/**
 * Stops a run at the next agent event once its spend passes the reserved cap.
 * Completed turns are priced as the ledger prices them. A tool call carries the
 * running turn's cost so far, so a long turn stops at its next tool call.
 */
export async function createRunSpendGuard(input: {
  runId: string;
  emit: (event: AgentEvent) => Promise<void>;
  signal?: AbortSignal;
  finishRun?: (runId: string, message: string) => Promise<void>;
}): Promise<RunSpendGuard> {
  const controller = new AbortController();
  const forward = () => controller.abort(input.signal?.reason);
  if (input.signal?.aborted) forward();
  else input.signal?.addEventListener("abort", forward, { once: true });
  const dispose = () => input.signal?.removeEventListener("abort", forward);

  const { rows } = await pool().query<{ reserved_micros: string }>(
    "select reserved_micros from spend_reservation where work_run_id = $1",
    [input.runId],
  );
  const cap = rows[0] ? Number(rows[0].reserved_micros) : null;
  if (cap === null) {
    return { emit: input.emit, signal: controller.signal, exceeded: () => null, dispose };
  }

  let completed = 0;
  let exceeded: SpendCapExceeded | null = null;
  const finishRun =
    input.finishRun ??
    (async (runId: string, message: string) => {
      const { finishWorkRun } = await import("../work/store");
      await finishWorkRun(runId, "failed", message);
    });

  const emit = async (event: AgentEvent): Promise<void> => {
    let current = completed;
    if (event.type === "usage" && event.source === "outer") {
      completed += priceUsage(event.usage, cap).costMicros;
      current = completed;
    } else if (event.type === "tool_start" && event.usageSnapshot) {
      current = completed + (pricedMicros(event.usageSnapshot) ?? 0);
    }
    await input.emit(event);
    if (exceeded || current <= cap) return;
    exceeded = new SpendCapExceeded(microsToUsd(cap), microsToUsd(current));
    await input.emit({ type: "error", message: exceeded.message });
    await finishRun(input.runId, exceeded.message);
    controller.abort(exceeded);
  };

  return { emit, signal: controller.signal, exceeded: () => exceeded, dispose };
}
