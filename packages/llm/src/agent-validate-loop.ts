import type { AgentBackend } from "./agent-backend";
import { detectAgentExecutionError } from "./agent-error";

type AgentRunOptions = Parameters<AgentBackend["run"]>[0];

export type ValidatedAgentTurnOptions<T> = {
  backend: AgentBackend;
  run: AgentRunOptions;
  /** Parse + validate the agent's final text; throw with a precise,
   *  agent-readable message when the output is unusable. */
  validate: (finalText: string) => T;
  /** Total attempts including the first (default 3). */
  maxAttempts?: number;
  /** Log prefix, e.g. "metric-agent org=… slug=…". */
  label: string;
};

/**
 * GJ2 — the iterative-agent loop. A job agent's reply that fails
 * validation no longer fails the whole job: the validation error (and the
 * tail of the rejected reply) is fed back to the agent for a corrective
 * attempt, bounded by maxAttempts. Backend failures and upstream provider
 * outages still throw immediately — retrying those wastes spend.
 */
export async function runValidatedAgentTurn<T>(
  opts: ValidatedAgentTurnOptions<T>,
): Promise<{ value: T; finalText: string; attempts: number }> {
  const maxAttempts = Math.max(1, opts.maxAttempts ?? 3);
  let lastError = "";
  let lastOutput = "";

  for (let attempt = 1; attempt <= maxAttempts; attempt++) {
    const prompt =
      attempt === 1
        ? opts.run.prompt
        : correctionPrompt(opts.run.prompt, lastOutput, lastError);
    const result = await opts.backend.run({ ...opts.run, prompt });
    if (result.status !== "completed") {
      throw new Error(
        result.error ?? `${opts.backend.id} returned status=${result.status}`,
      );
    }
    const executionError = detectAgentExecutionError(result.finalText);
    if (executionError) throw executionError;
    try {
      return {
        value: opts.validate(result.finalText),
        finalText: result.finalText,
        attempts: attempt,
      };
    } catch (err) {
      lastError = err instanceof Error ? err.message : String(err);
      lastOutput = result.finalText;
      console.warn(
        `[${opts.label}] attempt ${attempt}/${maxAttempts} failed validation: ${lastError}`,
      );
    }
  }

  throw new Error(
    `[${opts.label}] output failed validation after ${maxAttempts} attempt(s): ${lastError}`,
  );
}

function correctionPrompt(
  base: string,
  rejectedOutput: string,
  error: string,
): string {
  return [
    base,
    "",
    "<previous-attempt-rejected>",
    "Your previous reply was rejected by the output validator:",
    error,
    "",
    "Tail of the rejected reply:",
    rejectedOutput.slice(-2_000),
    "</previous-attempt-rejected>",
    "",
    "Redo the task and reply in EXACTLY the required output format —",
    "no surrounding prose, no apologies.",
  ].join("\n");
}
