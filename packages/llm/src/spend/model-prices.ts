/**
 * Published token prices, in USD per million tokens. Hermes carries the same
 * snapshot in scripts/patches/hermes-acp-cost.patch; change both together.
 */
export const MODEL_PRICES_VERSION = "official-docs-2026-10";

type TokenPrices = { input: number; output: number; cacheRead: number; cacheWrite?: number };

const GEMINI_FLASH: ReadonlyArray<{ from: string; prices: TokenPrices }> = [
  { from: "2026-01-01T00:00:00Z", prices: { input: 0.75, output: 3.75, cacheRead: 0.075 } },
  { from: "2027-01-01T00:00:00Z", prices: { input: 1.5, output: 7.5, cacheRead: 0.15 } },
];
const CLAUDE_SONNET_5_5: TokenPrices = { input: 2, output: 10, cacheRead: 0.2, cacheWrite: 2.5 };

function pricesFor(provider: string, model: string, at: Date): TokenPrices | undefined {
  if (provider === "google-gemini" && (model === "gemini-3.7-flash" || model === "gemini-3.8-flash")) {
    return [...GEMINI_FLASH].reverse().find((entry) => at >= new Date(entry.from))?.prices;
  }
  if (provider === "anthropic" && model === "claude-sonnet-5-5") return CLAUDE_SONNET_5_5;
  return undefined;
}

/**
 * The cost of one run's tokens, or undefined for a model without published
 * prices. Input excludes cache reads and writes; tokens the total holds beyond
 * the named counts are reasoning, billed as output.
 */
export function estimateTokenCostUsd(
  provider: string,
  model: string,
  usage: { inputTokens?: number; outputTokens?: number; cacheReadTokens?: number; cacheWriteTokens?: number; totalTokens?: number },
  at = new Date(),
): number | undefined {
  const prices = pricesFor(provider, model, at);
  if (!prices) return undefined;
  const input = usage.inputTokens ?? 0;
  const output = usage.outputTokens ?? 0;
  const cacheRead = usage.cacheReadTokens ?? 0;
  const cacheWrite = usage.cacheWriteTokens ?? 0;
  const extra = Math.max(0, (usage.totalTokens ?? 0) - input - output - cacheRead - cacheWrite);
  return (
    input * prices.input +
    cacheRead * prices.cacheRead +
    cacheWrite * (prices.cacheWrite ?? prices.input) +
    (output + extra) * prices.output
  ) / 1_000_000;
}
