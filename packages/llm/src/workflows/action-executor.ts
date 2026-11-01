import {
  finishActionExecution,
  getActionRequest,
  markActionRequestExecuted,
  markActionRequestFailed,
  recordActionExecution,
  type ActionRequestRecord,
} from "./action-store";

export type ActionExecutionInput = {
  request: ActionRequestRecord;
};

export type ActionExecutionOutcome = {
  status?: "succeeded" | "reconcile_required" | "partially_applied";
  externalRef?: string | null;
  result?: Record<string, unknown> | null;
  commandOrOperation?: string | null;
  changesetId?: string | null;
};

export type ActionAdapter = (
  input: ActionExecutionInput,
) => Promise<ActionExecutionOutcome>;

export type ActionAdapterResolver = (
  request: ActionRequestRecord,
) => Promise<ActionAdapter | null>;

const adapters = new Map<string, ActionAdapter>();
let fallbackAdapterResolver: ActionAdapterResolver | null = null;

/** Register an executor for a specific action kind. Test-overridable. */
export function registerActionAdapter(
  kind: string,
  adapter: ActionAdapter,
): void {
  adapters.set(kind, adapter);
}

/** Register one resolver for action kinds supplied by installed packs. */
export function registerFallbackActionAdapterResolver(resolver: ActionAdapterResolver): () => void {
  fallbackAdapterResolver = resolver;
  return () => {
    if (fallbackAdapterResolver === resolver) fallbackAdapterResolver = null;
  };
}

export function getRegisteredActionKinds(): string[] {
  return Array.from(adapters.keys());
}

export class ActionRequestNotApprovedError extends Error {
  constructor(public readonly status: string) {
    super(`action_request status=${status}; expected approved`);
    this.name = "ActionRequestNotApprovedError";
  }
}

/**
 * Adapter-side signal that execution may be retried without terminally
 * failing the action request. The current execution attempt is still logged
 * as failed; the approved request remains eligible for the queue retry.
 */
export class RetryableActionAdapterError extends Error {
  readonly code = "action_adapter_retryable";

  constructor(message: string, options: { cause?: unknown } = {}) {
    super(message, options);
    this.name = "RetryableActionAdapterError";
  }
}

/**
 * Execute an approved action_request. Writes an action_execution row,
 * runs the registered adapter, updates
 * the execution + request status, and returns the final execution
 * record. Throws if the request isn't approved.
 */
export async function executeApprovedActionRequest(
  orgId: string,
  actionRequestId: string,
): Promise<{
  ok: boolean;
  error?: string;
  outcome?: ActionExecutionOutcome;
}> {
  const request = await getActionRequest(orgId, actionRequestId);
  if (!request) {
    throw new Error(`action_request ${actionRequestId} not found`);
  }
  if (request.status !== "approved") {
    throw new ActionRequestNotApprovedError(request.status);
  }

  const exec = await recordActionExecution({
    orgId,
    actionRequestId: request.id,
    executor: request.kind,
    payload: request.payload,
  });

  try {
    const adapter = adapters.get(request.kind) ?? await fallbackAdapterResolver?.(request) ?? undefined;
    if (!adapter) throw new Error(`no adapter registered for kind "${request.kind}"`);
    const outcome = await adapter({ request });
    const status = outcome.status ?? "succeeded";
    await finishActionExecution({
      id: exec.id,
      status,
      result: outcome.result ?? null,
      externalRef: outcome.externalRef ?? null,
      changesetId: outcome.changesetId ?? null,
      commandOrOperation: outcome.commandOrOperation ?? null,
    });
    await markActionRequestExecuted(request.id);
    return { ok: status === "succeeded", outcome };
  } catch (err) {
    const msg = err instanceof Error ? err.message : String(err);
    await finishActionExecution({
      id: exec.id,
      status: "failed",
      error: msg,
    });
    if (err instanceof RetryableActionAdapterError) throw err;
    await markActionRequestFailed(request.id, msg);
    return { ok: false, error: msg };
  }
}
