import type { RenderContext } from "./renderer";

/** The follow-up question for one item, or null when the answer has no drill-down. */
export function drillPrompt(template: unknown, label: string): string | null {
  if (typeof template !== "string" || !template.includes("{label}")) return null;
  return template.replaceAll("{label}", label);
}

/** Put the drill-down question for one item in the composer. The reader sends it. */
export function drillInto(ctx: RenderContext, componentId: string, template: unknown, label: string): void {
  const prompt = drillPrompt(template, label);
  if (prompt) ctx.onAction?.(componentId, "drill", { prompt });
}
