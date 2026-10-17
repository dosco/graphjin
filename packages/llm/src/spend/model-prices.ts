import axModelCatalog from "./ax-model-catalog.json";

/**
 * Token prices come from the model catalog of the pinned Ax Go module, which
 * ax-model-catalog.json holds (`pnpm --filter @neko/llm catalog:ax`). An
 * override, such as a negotiated or promotional rate, wins over the catalog.
 */
type CatalogPrice = {
  promptTokenCostPer1M: number;
  completionTokenCostPer1M: number;
  cacheReadTokenCostPer1M?: number;
  cacheWriteTokenCostPer1M?: number;
  longContextThreshold?: number;
  longContextPromptTokenCostPer1M?: number;
  longContextCompletionTokenCostPer1M?: number;
  longContextCacheReadTokenCostPer1M?: number;
  longContextCacheWriteTokenCostPer1M?: number;
};

const CATALOG = axModelCatalog as { version: string; providers: Record<string, Record<string, CatalogPrice>> };

export const PRICE_OVERRIDES_VERSION = "overrides-2026-10";

/** The pricing version for prices from an Ax catalog of the given module version. */
export function axCatalogPricingVersion(moduleVersion: string): string {
  return `ax-go@${moduleVersion}`;
}

export const MODEL_PRICES_VERSION = `${axCatalogPricingVersion(CATALOG.version)}+${PRICE_OVERRIDES_VERSION}`;

type Override = { provider: string; models: string[]; from?: string; price: CatalogPrice };

const flash = (input: number, output: number, cacheRead: number): CatalogPrice => ({
  promptTokenCostPer1M: input,
  completionTokenCostPer1M: output,
  cacheReadTokenCostPer1M: cacheRead,
});

// Later entries for the same model take effect from their `from` date.
const OVERRIDES: Override[] = [
  { provider: "google-gemini", models: ["gemini-3.7-flash", "gemini-3.8-flash"], from: "2026-01-01T00:00:00Z", price: flash(0.75, 3.75, 0.075) },
  { provider: "google-gemini", models: ["gemini-3.7-flash", "gemini-3.8-flash"], from: "2027-01-01T00:00:00Z", price: flash(1.5, 7.5, 0.15) },
];

/** The catalog providers that price a route provider, as the Ax Harness maps them. */
function catalogProviders(provider: string): string[] {
  const name = provider.toLowerCase();
  if (name === "openai-compatible" || name === "azure-openai") return ["openai"];
  if (name === "googlegeminiai") return ["google-gemini"];
  if (name === "vertex-ai") return ["google-gemini", "anthropic"];
  return [name];
}

function overrideFor(provider: string, model: string, at: Date): CatalogPrice | undefined {
  const providers = catalogProviders(provider);
  return OVERRIDES.filter(
    (entry) => providers.includes(entry.provider) && entry.models.includes(model) && (!entry.from || at >= new Date(entry.from)),
  ).at(-1)?.price;
}

export function hasPriceOverride(provider: string, model: string, at = new Date()): boolean {
  return overrideFor(provider, model, at) !== undefined;
}

function catalogPrice(provider: string, model: string): CatalogPrice | undefined {
  for (const name of catalogProviders(provider)) {
    const price = Object.hasOwn(CATALOG.providers, name) && Object.hasOwn(CATALOG.providers[name]!, model)
      ? CATALOG.providers[name]![model]
      : undefined;
    if (price) return price;
  }
  return undefined;
}

export type TokenUsageForPrice = {
  inputTokens?: number;
  outputTokens?: number;
  cacheReadTokens?: number;
  cacheWriteTokens?: number;
  totalTokens?: number;
  /** Model calls the usage sums; the long-context tier applies per call. */
  calls?: number;
};

export type TokenCost = { usd: number; costSource: "price_override" | "ax_model_catalog"; pricingCatalogVersion: string };

/**
 * The cost of the tokens, or undefined for a model with no override and no
 * catalog price. Input excludes cache reads and writes; tokens the total holds
 * beyond the named counts are reasoning, billed as output. Above the model's
 * long-context threshold, the average call uses the long-context rates.
 */
export function estimateTokenCost(provider: string, model: string, usage: TokenUsageForPrice, at = new Date()): TokenCost | undefined {
  const override = overrideFor(provider, model, at);
  const price = override ?? catalogPrice(provider, model);
  if (!price) return undefined;
  const input = usage.inputTokens ?? 0;
  const output = usage.outputTokens ?? 0;
  const cacheRead = usage.cacheReadTokens ?? 0;
  const cacheWrite = usage.cacheWriteTokens ?? 0;
  const extra = Math.max(0, (usage.totalTokens ?? 0) - input - output - cacheRead - cacheWrite);
  const promptRate = price.promptTokenCostPer1M;
  const completionRate = price.completionTokenCostPer1M;
  const readRate = price.cacheReadTokenCostPer1M ?? promptRate;
  const writeRate = price.cacheWriteTokenCostPer1M ?? promptRate;
  const threshold = price.longContextThreshold ?? 0;
  const long = threshold > 0 && (input + cacheRead + cacheWrite) / Math.max(1, usage.calls ?? 1) > threshold;
  const rates = long
    ? {
        prompt: price.longContextPromptTokenCostPer1M ?? promptRate,
        completion: price.longContextCompletionTokenCostPer1M ?? completionRate,
        read: price.longContextCacheReadTokenCostPer1M ?? readRate,
        write: price.longContextCacheWriteTokenCostPer1M ?? writeRate,
      }
    : { prompt: promptRate, completion: completionRate, read: readRate, write: writeRate };
  const usd =
    (input * rates.prompt + cacheRead * rates.read + cacheWrite * rates.write + (output + extra) * rates.completion) / 1_000_000;
  return { usd, costSource: override ? "price_override" : "ax_model_catalog", pricingCatalogVersion: MODEL_PRICES_VERSION };
}

export function estimateTokenCostUsd(provider: string, model: string, usage: TokenUsageForPrice, at = new Date()): number | undefined {
  return estimateTokenCost(provider, model, usage, at)?.usd;
}
