"use client";

import { MessageSquare } from "lucide-react";
import { Button } from "@/components/ui/button";
import { cn } from "@/lib/cn";
import { drillInto, drillPrompt } from "./drill";
import type { RenderContext } from "./renderer";

type AskProps = { ctx: RenderContext; componentId: string; template: unknown; label: string };

/** The drill-down question for one item. A click puts it in the composer. */
export function AskChip({ ctx, componentId, template, label, className }: AskProps & { className?: string }) {
  const prompt = drillPrompt(template, label);
  if (!prompt) return null;
  return (
    <Button
      size="sm"
      className={cn("answer-ask", className)}
      title="Put this question in the composer"
      onClick={() => drillInto(ctx, componentId, template, label)}
    >
      <MessageSquare aria-hidden="true" strokeWidth={2.2} />
      {prompt}
    </Button>
  );
}

/** The item a chart, map, or diagram reader picked, with its question. */
export function AskCard({ ctx, componentId, template, label, value }: AskProps & { value?: string }) {
  if (!drillPrompt(template, label)) return null;
  return (
    <div className="answer-ask-card" role="status">
      <span className="answer-ask-card-label">{label}</span>
      {value ? <span className="answer-ask-card-value">{value}</span> : null}
      <AskChip ctx={ctx} componentId={componentId} template={template} label={label} />
    </div>
  );
}
