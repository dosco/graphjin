import type { AxProgramUsage } from "@ax-llm/ax";
import type { AgentTokenUsage } from "./agent-backend";

function addOptional(
  left: number | undefined,
  right: number | undefined,
): number | undefined {
  if (left === undefined && right === undefined) return undefined;
  return (left ?? 0) + (right ?? 0);
}

/** Normalize every provider call made by an Ax program, including retries. */
export function normalizeAxProgramUsage(
  usages: readonly AxProgramUsage[],
): AgentTokenUsage {
  const tokenUsages = usages.flatMap((usage) =>
    usage.tokens ? [usage.tokens] : [],
  );
  if (tokenUsages.length === 0) {
    return {
      coverage: "unavailable",
      missingReasons: ["Ax program returned no token usage"],
    };
  }
  const complete = tokenUsages.length === usages.length;
  const sum = (select: (usage: (typeof tokenUsages)[number]) => number) =>
    tokenUsages.reduce((total, usage) => total + select(usage), 0);
  const optionalSum = (
    select: (usage: (typeof tokenUsages)[number]) => number | undefined,
  ): number | undefined => {
    const values = tokenUsages
      .map(select)
      .filter((value): value is number => value !== undefined);
    return values.length > 0
      ? values.reduce((total, value) => total + value, 0)
      : undefined;
  };
  return {
    inputTokens: sum((usage) => usage.promptTokens),
    outputTokens: sum((usage) => usage.completionTokens),
    cacheReadTokens: optionalSum((usage) => usage.cacheReadTokens),
    cacheWriteTokens: optionalSum((usage) => usage.cacheCreationTokens),
    reasoningTokens: optionalSum(
      (usage) => usage.reasoningTokens ?? usage.thoughtsTokens,
    ),
    totalTokens: sum((usage) => usage.totalTokens),
    coverage: complete ? "complete" : "partial",
    ...(!complete
      ? { missingReasons: ["Some Ax program calls omitted token usage"] }
      : {}),
  };
}

/** Add token/cost scopes without turning a missing scope into a zero. */
export function combineAgentTokenUsage(
  ...usages: readonly AgentTokenUsage[]
): AgentTokenUsage {
  const available = usages.some((usage) =>
    [usage.inputTokens, usage.outputTokens, usage.totalTokens].some(
      (value) => value !== undefined,
    ),
  );
  const missingReasons = usages.flatMap((usage) => usage.missingReasons ?? []);
  const combined = usages.reduce<AgentTokenUsage>(
    (total, usage) => ({
      inputTokens: addOptional(total.inputTokens, usage.inputTokens),
      outputTokens: addOptional(total.outputTokens, usage.outputTokens),
      cacheReadTokens: addOptional(
        total.cacheReadTokens,
        usage.cacheReadTokens,
      ),
      cacheWriteTokens: addOptional(
        total.cacheWriteTokens,
        usage.cacheWriteTokens,
      ),
      reasoningTokens: addOptional(
        total.reasoningTokens,
        usage.reasoningTokens,
      ),
      totalTokens: addOptional(total.totalTokens, usage.totalTokens),
      estimatedCostUsd: addOptional(
        total.estimatedCostUsd,
        usage.estimatedCostUsd,
      ),
      billedCostUsd: addOptional(total.billedCostUsd, usage.billedCostUsd),
      ...(usage.currency ?? total.currency
        ? { currency: usage.currency ?? total.currency }
        : {}),
      ...(usage.pricingCatalogVersion ?? total.pricingCatalogVersion
        ? {
            pricingCatalogVersion:
              usage.pricingCatalogVersion ?? total.pricingCatalogVersion,
          }
        : {}),
      ...(total.costStatus === "unknown" || usage.costStatus === "unknown"
        ? { costStatus: "unknown" as const }
        : usage.costStatus ?? total.costStatus
          ? { costStatus: usage.costStatus ?? total.costStatus }
          : {}),
      coverage: "unavailable",
    }),
    { coverage: "unavailable" },
  );
  return {
    ...combined,
    coverage: !available
      ? "unavailable"
      : usages.every((usage) => usage.coverage === "complete")
        ? "complete"
        : "partial",
    ...(missingReasons.length > 0
      ? { missingReasons: [...new Set(missingReasons)] }
      : {}),
  };
}

/** Locate normalized inner-model usage in GraphJin MCP response envelopes. */
export function normalizeGraphjinAgentUsage(value: unknown):
  | { usage: AgentTokenUsage; provider?: string; model?: string; modelCalls?: number }
  | undefined {
  const queue: Array<{ value: unknown; depth: number }> = [{ value, depth: 0 }];
  const seen = new Set<object>();
  let provider: string | undefined;
  let model: string | undefined;
  while (queue.length > 0) {
    const current = queue.shift()!;
    if (current.depth > 8 || current.value == null) continue;
    if (typeof current.value === "string") {
      const text = current.value.trim();
      if (text.startsWith("{") || text.startsWith("[")) {
        try {
          queue.push({ value: JSON.parse(text), depth: current.depth + 1 });
        } catch {
          // MCP renderings may contain prose; only structured JSON is useful.
        }
      }
      continue;
    }
    if (typeof current.value !== "object") continue;
    if (seen.has(current.value)) continue;
    seen.add(current.value);
    let record = current.value as Record<string, unknown>;
    // GraphJin reports one usage entry per model call.
    const calls = [record.actor, record.responder]
      .flatMap((entries) => (Array.isArray(entries) ? entries : []))
      .filter((entry): entry is Record<string, unknown> => Boolean(entry) && typeof entry === "object");
    const modelCalls = calls.length || undefined;
    if (modelCalls) {
      const sum = (key: string) => calls.reduce((total, entry) => total + (Number(entry[key]) || 0), 0);
      record = {
        prompt_tokens: sum("prompt_tokens"),
        completion_tokens: sum("completion_tokens"),
        total_tokens: sum("total_tokens"),
        cached_tokens: sum("cached_tokens"),
      };
    }
    if (!provider && typeof record.provider === "string") provider = record.provider;
    if (!model && typeof record.model === "string") model = record.model;
    const number = (...keys: string[]): number | undefined => {
      for (const key of keys) {
        const parsed = Number(record[key]);
        if (Number.isFinite(parsed) && parsed >= 0) return parsed;
      }
      return undefined;
    };
    const inputTokens = number("input_tokens", "inputTokens", "prompt_tokens");
    const outputTokens = number(
      "output_tokens",
      "outputTokens",
      "completion_tokens",
    );
    const totalTokens = number("total_tokens", "totalTokens");
    const cacheReadTokens = number(
      "cache_read_tokens",
      "cacheReadTokens",
      "cached_tokens",
    );
    const cacheWriteTokens = number("cache_write_tokens", "cacheWriteTokens");
    const billedCostUsd = number("cost_usd", "costUsd", "billed_cost_usd");
    if (
      inputTokens !== undefined ||
      outputTokens !== undefined ||
      totalTokens !== undefined ||
      billedCostUsd !== undefined
    ) {
      const complete = inputTokens !== undefined && outputTokens !== undefined;
      return {
        usage: {
          ...(inputTokens !== undefined ? { inputTokens } : {}),
          ...(outputTokens !== undefined ? { outputTokens } : {}),
          ...(cacheReadTokens !== undefined ? { cacheReadTokens } : {}),
          ...(cacheWriteTokens !== undefined ? { cacheWriteTokens } : {}),
          ...(totalTokens !== undefined
            ? { totalTokens }
            : complete
              ? { totalTokens: inputTokens + outputTokens }
              : {}),
          ...(billedCostUsd !== undefined
            ? { billedCostUsd, currency: "USD" }
            : {}),
          coverage: complete ? "complete" : "partial",
          ...(!complete
            ? {
                missingReasons: [
                  "GraphJin usage omitted input or output token counts",
                ],
              }
            : {}),
        },
        ...(provider ? { provider } : {}),
        ...(model ? { model } : {}),
        ...(modelCalls ? { modelCalls } : {}),
      };
    }
    for (const nested of Object.values(record)) {
      queue.push({ value: nested, depth: current.depth + 1 });
    }
  }
  return undefined;
}

/** The GraphJin agent response status (answered, blocked, error, …) inside a tool result. */
export function graphjinAgentResponseStatus(value: unknown): string | undefined {
  const queue: Array<{ value: unknown; depth: number }> = [{ value, depth: 0 }];
  while (queue.length > 0) {
    const current = queue.shift()!;
    if (current.depth > 8 || current.value == null) continue;
    if (typeof current.value === "string") {
      const text = current.value.trim();
      if (text.startsWith("{") || text.startsWith("[")) {
        try {
          queue.push({ value: JSON.parse(text), depth: current.depth + 1 });
        } catch {
          // Prose rendering.
        }
      }
      continue;
    }
    if (typeof current.value !== "object") continue;
    const record = current.value as Record<string, unknown>;
    if (record.response && typeof record.response === "object") {
      const status = (record.response as Record<string, unknown>).status;
      if (typeof status === "string") return status;
    }
    if (typeof record.error === "string" && !record.response) return "error";
    for (const nested of Object.values(record)) queue.push({ value: nested, depth: current.depth + 1 });
  }
  return undefined;
}
