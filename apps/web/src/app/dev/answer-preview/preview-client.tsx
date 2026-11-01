"use client";

import "@/a2ui/components";
import { useMemo, useState } from "react";
import { applyMessage, getRootComponent, setDataModelValue } from "@/a2ui/surface";
import { renderComponent } from "@/a2ui/renderer";
import type { SurfaceState } from "@/a2ui/types";
import { ANSWER_FIXTURES } from "./fixtures";

export default function AnswerPreview({ fixture }: { fixture: string }) {
  const entry = ANSWER_FIXTURES[fixture] ?? ANSWER_FIXTURES.report!;
  const surface = useMemo(() => {
    let surfaces = new Map<string, SurfaceState>();
    for (const message of entry.messages) surfaces = applyMessage(surfaces, message);
    return [...surfaces.values()][0]!;
  }, [entry]);
  const [dataModel, setDataModel] = useState(surface.dataModel);
  const [lastPrompt, setLastPrompt] = useState<string | null>(null);
  const root = getRootComponent(surface);
  const ctx = {
    surface: { ...surface, dataModel },
    onDataChange: (path: string, value: unknown) => setDataModel((current) => setDataModelValue(current, path, value)),
    onAction: (_id: string, _event: string, context?: Record<string, unknown>) => {
      setLastPrompt(typeof context?.prompt === "string" ? context.prompt : null);
    },
  };
  return (
    <main className="work-transcript" data-preview-fixture={fixture}>
      <nav className="flex flex-wrap gap-3 text-sm">
        {Object.entries(ANSWER_FIXTURES).map(([key, value]) => (
          <a key={key} href={`?f=${key}`} aria-current={key === fixture ? "page" : undefined}>{value.label}</a>
        ))}
      </nav>
      <div className="work-surface-frame">{root ? renderComponent(root, ctx) : null}</div>
      <p data-preview-last-prompt>{lastPrompt ?? "No follow-up sent"}</p>
    </main>
  );
}
