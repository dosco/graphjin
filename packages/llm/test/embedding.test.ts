import { afterEach, beforeEach, expect, it, vi } from "vitest";
import {
  EmbeddingServiceError,
  embedMemoryDocuments,
  embedMemoryQuery,
  embedSkillDocuments,
  embedSkillQuery,
  embedTexts,
  isRetryableEmbeddingError,
  skillDocument,
  skillEmbeddingInput,
} from "../src/embedding";

const unit = (i: number) => Array.from({ length: 768 }, (_, j) => (j === i ? 1 : 0));
const reply = (count: number) => ({
  model: "google/embeddinggemma-2",
  dimensions: 768,
  vectors: Array.from({ length: count }, (_, i) => unit(i)),
});

let fetcher: ReturnType<typeof vi.fn>;
beforeEach(() => {
  vi.stubEnv("NEKO_EMBEDDING_URL", "http://embedding:5003/");
  fetcher = vi.fn(async (_url: string, init: RequestInit) =>
    Response.json(reply(JSON.parse(String(init.body)).inputs.length)),
  );
  vi.stubGlobal("fetch", fetcher);
});
afterEach(() => {
  vi.unstubAllEnvs();
  vi.unstubAllGlobals();
});

const sent = (call = 0) => ({
  url: fetcher.mock.calls[call]![0],
  body: JSON.parse(String((fetcher.mock.calls[call]![1] as RequestInit).body)),
});

it("posts to /v1/embed and returns the vectors in input order", async () => {
  const vectors = await embedTexts({ role: "document", task: "search", inputs: [{ text: " a " }, { text: "b", title: "T" }] });
  expect(vectors).toEqual([unit(0), unit(1)]);
  expect(sent()).toEqual({
    url: "http://embedding:5003/v1/embed",
    body: { role: "document", task: "search", inputs: [{ text: "a", title: null }, { text: "b", title: "T" }] },
  });
});

it("sends each caller's role and task", async () => {
  await embedMemoryDocuments(["Refunds are netted", "Reports go out Monday"]);
  expect(sent(0).body).toEqual({
    role: "document",
    task: "search",
    inputs: [{ text: "Refunds are netted", title: null }, { text: "Reports go out Monday", title: null }],
  });
  await embedMemoryQuery("how are refunds handled");
  expect(sent(1).body).toEqual({ role: "query", task: "search", inputs: [{ text: "how are refunds handled", title: null }] });
  await embedSkillDocuments([
    { name: "xlsx", document: skillDocument({ name: "xlsx", description: "Spreadsheet files" }) },
    { name: "bare", document: skillDocument({ name: "bare", description: "" }) },
  ]);
  expect(sent(2).body).toEqual({
    role: "document",
    task: "none",
    inputs: [{ text: "Spreadsheet files", title: "xlsx" }, { text: "bare", title: null }],
  });
  await embedSkillQuery("make an excel file");
  expect(sent(3).body).toEqual({ role: "query", task: "none", inputs: [{ text: "make an excel file", title: null }] });
});

it("rejects an incompatible response", async () => {
  const good = reply(1);
  for (const bad of [
    { ...good, model: "Xenova/all-MiniLM-L6-v2" },
    { ...good, dimensions: 384 },
    { ...good, vectors: [Array(384).fill(0)] },
    { ...good, vectors: [Array(768).fill("1")] },
    { ...good, vectors: [unit(0), unit(1)] },
    { ...good, vectors: [[...unit(0).slice(1), Number.NaN]] },
  ]) {
    fetcher.mockResolvedValueOnce(Response.json(bad));
    await expect(embedMemoryQuery("hello")).rejects.toThrow(/incompatible vector/);
  }
});

it("marks busy and unavailable responses and network failures retryable", async () => {
  for (const [status, retryable] of [[429, true], [503, true], [400, false], [413, false]] as const) {
    fetcher.mockResolvedValueOnce(Response.json({ error: "nope" }, { status }));
    const error = await embedMemoryQuery("hello").catch((e) => e);
    expect(error).toBeInstanceOf(EmbeddingServiceError);
    expect(error.message).toContain(`HTTP ${status}: nope`);
    expect(isRetryableEmbeddingError(error)).toBe(retryable);
  }
  fetcher.mockRejectedValueOnce(new DOMException("timed out", "TimeoutError"));
  expect(isRetryableEmbeddingError(await embedMemoryQuery("hello").catch((e) => e))).toBe(true);
});

it("rejects invalid input and missing configuration before calling the service", async () => {
  await expect(embedMemoryQuery(" ")).rejects.toThrow(/empty/);
  await expect(embedMemoryQuery("x".repeat(32001))).rejects.toThrow(/32000/);
  await expect(embedTexts({ role: "query", task: "none", inputs: [] })).rejects.toThrow(/no inputs/);
  await expect(
    embedTexts({ role: "document", task: "none", inputs: Array.from({ length: 65 }, () => ({ text: "a" })) }),
  ).rejects.toThrow(/at most 64/);
  vi.stubEnv("NEKO_EMBEDDING_URL", "");
  await expect(embedMemoryQuery("hello")).rejects.toThrow(/not configured/);
  expect(fetcher).not.toHaveBeenCalled();
});

it("truncates long memory documents to the 32000-character limit", async () => {
  await embedMemoryDocuments(["y".repeat(40000)]);
  expect(sent().body.inputs[0].text).toHaveLength(32000);
});

it("splits a stored skill document into title and text", () => {
  expect(skillEmbeddingInput("docx", "docx\nWord documents")).toEqual({ text: "Word documents", title: "docx" });
  expect(skillEmbeddingInput("docx", "docx")).toEqual({ text: "docx", title: null });
});
