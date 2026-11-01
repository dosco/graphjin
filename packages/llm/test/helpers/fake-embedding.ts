// A stand-in for the embedding service in tests: keeps the real module
// (constants, skillDocument, skillEmbeddingInput, vectorLiteral) and replaces
// every call that would reach the service with a deterministic vector.
//
//   vi.mock("../../src/embedding", async (importOriginal) => {
//     const { fakeEmbedding } = await import("../helpers/fake-embedding");
//     return fakeEmbedding(await importOriginal());
//   });

import { vi } from "vitest";
import type * as Embedding from "../../src/embedding";

export const FAKE_EMBEDDING_DIM = 768;

/** A per-text vector from a hash of the text, so equal texts embed equally. */
export function hashVector(text: string): number[] {
  const seed = text.split("").reduce((acc, c) => (acc * 31 + c.charCodeAt(0)) >>> 0, 7);
  return Array.from({ length: FAKE_EMBEDDING_DIM }, (_, i) => Math.sin(seed + i) * 0.1);
}

export function fakeEmbedding(original: unknown, vectorFor: (text: string) => number[] = hashVector) {
  const real = original as typeof Embedding;
  const skillText = (skill: { name: string; document: string }) => {
    const input = real.skillEmbeddingInput(skill.name, skill.document);
    return input.title ? `${input.title}\n${input.text}` : input.text;
  };
  return {
    ...real,
    embedTexts: vi.fn(async (request: Embedding.EmbedRequest) =>
      request.inputs.map((input) => vectorFor(input.title ? `${input.title}\n${input.text}` : input.text)),
    ),
    embedText: vi.fn(async (text: string) => vectorFor(text)),
    embedMemoryQuery: vi.fn(async (query: string) => vectorFor(query)),
    embedMemoryDocuments: vi.fn(async (texts: string[]) => texts.map(vectorFor)),
    embedSkillQuery: vi.fn(async (query: string) => vectorFor(query)),
    embedSkillDocuments: vi.fn(async (skills: Array<{ name: string; document: string }>) =>
      skills.map((skill) => vectorFor(skillText(skill))),
    ),
  };
}

/**
 * Runs the embedding job until the backlog is empty. Another test file may
 * hold the job's cross-worker lock for a moment, so it retries briefly.
 */
export async function drainEmbeddingBacklog(): Promise<void> {
  const { runEmbeddingIndexJob } = await import("../../src/embedding-jobs");
  for (let attempt = 0; attempt < 100; attempt++) {
    const result = await runEmbeddingIndexJob();
    if (!result.remaining) return;
    await new Promise((resolve) => setTimeout(resolve, 100));
  }
  throw new Error("the embedding backlog did not drain");
}
