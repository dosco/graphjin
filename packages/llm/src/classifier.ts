/**
 * Chat-question classifier — natural language → dashboard card metadata.
 *
 * Used by web's POST /api/briefing on the chat path: classify the question
 * to derive a card title, slug, rationale, and chart hint *before*
 * enqueueing the metric_refresh job. No tools, no DB lookups — pure
 * intent classification, ~2-5s LLM call.
 */

import { ax } from "@ax-llm/ax";
import { observeSafely, type HarnessObserver } from "@neko/telemetry";
import type { AgentTokenUsage } from "./agent-backend";
import { buildLlm } from "./llm";
import { normalizeAxProgramUsage } from "./usage-normalization";

const DESCRIPTION = `You classify a user's natural-language question about their business data into a dashboard card specification. Output a kebab-case slug for internal use, a short human-readable card title (what a CXO would see as the card heading), a one-sentence rationale explaining why this matters, and the best chart type for the answer. No database lookups — just classify the intent from the question.`;

export type ClassifiedCard = {
  slug: string;
  title: string;
  why: string;
  chartHint: string;
};

export type QuestionClassificationDiagnostics = {
  durationMs: number;
  usage: AgentTokenUsage;
  provider?: string;
  model?: string;
};

export type ClassifyQuestionOptions = {
  observer?: HarnessObserver;
  operationId?: string;
  parentOperationId?: string;
  observationAttributes?: Record<string, unknown>;
  onDiagnostics?: (diagnostics: QuestionClassificationDiagnostics) => void;
};

export async function classifyQuestion(
  question: string,
  role: string,
  orgId?: string,
  options: ClassifyQuestionOptions = {},
): Promise<ClassifiedCard> {
  const llm = await buildLlm(orgId);
  const classifier = ax(
    `question:string "the user's natural language question", role:string "CXO role" -> slug:string "kebab-case identifier for internal use", title:string "short human-readable card title for the dashboard", why:string "one-sentence rationale explaining why this matters", chartHint:class "kpi, line, bar, donut, area"`,
    { description: DESCRIPTION },
  );
  const operationId = options.operationId ?? `classify:${orgId ?? "default"}`;
  const startedAt = Date.now();
  await observeSafely(options.observer, {
    kind: "model.request",
    operationId,
    ...(options.parentOperationId
      ? { parentOperationId: options.parentOperationId }
      : {}),
    attributes: {
      "openneko.model.scope": "classification",
      ...options.observationAttributes,
    },
  });
  try {
    const result = await classifier.forward(llm, { question, role });
    const programUsage = classifier.getUsage();
    const usage = normalizeAxProgramUsage(programUsage);
    const lastCall = programUsage.at(-1);
    const diagnostics: QuestionClassificationDiagnostics = {
      durationMs: Date.now() - startedAt,
      usage,
      ...(lastCall?.ai ? { provider: lastCall.ai } : {}),
      ...(lastCall?.model ? { model: lastCall.model } : {}),
    };
    options.onDiagnostics?.(diagnostics);
    await observeSafely(options.observer, {
      kind: "model.response",
      operationId,
      ...(options.parentOperationId
        ? { parentOperationId: options.parentOperationId }
        : {}),
      status: "ok",
      attributes: {
        "openneko.model.scope": "classification",
        ...(diagnostics.provider
          ? { "gen_ai.provider.name": diagnostics.provider }
          : {}),
        ...(diagnostics.model
          ? { "gen_ai.response.model": diagnostics.model }
          : {}),
        ...options.observationAttributes,
      },
      measurements: {
        ...usage,
        durationMs: diagnostics.durationMs,
      },
    });
    return {
      slug: String(result.slug ?? ""),
      title: String(result.title ?? ""),
      why: String(result.why ?? ""),
      chartHint: String(result.chartHint ?? "bar"),
    };
  } catch (cause) {
    await observeSafely(options.observer, {
      kind: "model.response",
      operationId,
      ...(options.parentOperationId
        ? { parentOperationId: options.parentOperationId }
        : {}),
      status: "error",
      errorType: cause instanceof Error ? cause.name : "unknown",
      attributes: {
        "openneko.model.scope": "classification",
        ...options.observationAttributes,
      },
      measurements: {
        durationMs: Date.now() - startedAt,
        coverage: "unavailable",
        missingReasons: ["classification failed before usage was available"],
      },
    });
    throw cause;
  }
}
