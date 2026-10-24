// Embedding computation belongs to the shared, lazily started embedding
// service (EmbeddingGemma 2). Keep the model and vector contract identical to
// the pgvector columns (work_memory.embedding, skill_embedding.embedding).
//
// Nothing here checks the admin "Semantic search" switch: callers gate on
// semanticSearchEnabled() (./semantic-search) before they embed.

export const EMBEDDING_DIM = 768;
export const EMBEDDING_MODEL = "google/embeddinggemma-2";
/** The service accepts 1–64 inputs per request. */
export const EMBEDDING_MAX_BATCH = 64;
export const EMBEDDING_MAX_CHARS = 32000;
/**
 * Interactive searches wait this long. The first call after the service idles
 * loads the model (about 10 s), so an interactive caller falls back to keyword
 * search instead of waiting; the load continues and the next call is fast.
 */
export const INTERACTIVE_EMBED_TIMEOUT_MS = 4_000;
/** Durable background jobs allow a cold model load and a full batch. */
export const BACKGROUND_EMBED_TIMEOUT_MS = 150_000;

/** query: a search query. document: content stored in the index. */
export type EmbeddingRole = "query" | "document";
/**
 * search: the service adds EmbeddingGemma's search prompts (memories).
 * none: no prompt; a title is joined as `{title}\n{text}` (skills, which
 * ranked best without prompts).
 */
export type EmbeddingTask = "search" | "none";
export type EmbeddingInput = { text: string; title?: string | null };

export type EmbedRequest = {
  role: EmbeddingRole;
  task: EmbeddingTask;
  inputs: ReadonlyArray<EmbeddingInput>;
  timeoutMs?: number;
};

/** A failed embedding call. retryable: busy (429), unavailable (503), timeout or network. */
export class EmbeddingServiceError extends Error {
  readonly status: number | null;
  readonly retryable: boolean;
  constructor(message: string, status: number | null, retryable: boolean) {
    super(message);
    this.name = "EmbeddingServiceError";
    this.status = status;
    this.retryable = retryable;
  }
}

export function isRetryableEmbeddingError(error: unknown): boolean {
  return error instanceof EmbeddingServiceError && error.retryable;
}

function embeddingServiceUrl(): string {
  const baseUrl = process.env.NEKO_EMBEDDING_URL?.trim();
  if (!baseUrl) throw new Error("NEKO_EMBEDDING_URL is not configured");
  return `${baseUrl.replace(/\/+$/, "")}/v1/embed`;
}

function normalizedInputs(inputs: ReadonlyArray<EmbeddingInput>): Array<{ text: string; title: string | null }> {
  if (inputs.length === 0) throw new Error("embedTexts: no inputs");
  if (inputs.length > EMBEDDING_MAX_BATCH) {
    throw new Error(`embedTexts: at most ${EMBEDDING_MAX_BATCH} inputs per request`);
  }
  return inputs.map((input) => {
    const text = input.text.trim();
    if (!text) throw new Error("embedText: text is empty");
    if (text.length > EMBEDDING_MAX_CHARS) {
      throw new Error(`embedText: text exceeds ${EMBEDDING_MAX_CHARS} characters`);
    }
    const title = input.title?.trim() || null;
    return { text, title };
  });
}

function validVector(value: unknown): value is number[] {
  return (
    Array.isArray(value) &&
    value.length === EMBEDDING_DIM &&
    value.every((entry) => typeof entry === "number" && Number.isFinite(entry))
  );
}

/** Embeds 1–64 inputs in one request; vectors come back normalized, in input order. */
export async function embedTexts(request: EmbedRequest): Promise<number[][]> {
  const inputs = normalizedInputs(request.inputs);
  const url = embeddingServiceUrl();
  let response: Response;
  try {
    response = await fetch(url, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ role: request.role, task: request.task, inputs }),
      signal: AbortSignal.timeout(request.timeoutMs ?? INTERACTIVE_EMBED_TIMEOUT_MS),
      redirect: "error",
    });
  } catch (error) {
    const reason = error instanceof Error ? error.message : String(error);
    throw new EmbeddingServiceError(`embedding service unreachable: ${reason}`, null, true);
  }
  if (!response.ok) {
    const body = (await response.json().catch(() => null)) as { error?: unknown } | null;
    const detail = typeof body?.error === "string" ? `: ${body.error}` : "";
    throw new EmbeddingServiceError(
      `embedding service returned HTTP ${response.status}${detail}`,
      response.status,
      response.status === 429 || response.status === 503,
    );
  }
  const body = (await response.json().catch(() => null)) as
    | { model?: unknown; dimensions?: unknown; vectors?: unknown }
    | null;
  if (
    body?.model !== EMBEDDING_MODEL ||
    body?.dimensions !== EMBEDDING_DIM ||
    !Array.isArray(body?.vectors) ||
    body.vectors.length !== inputs.length ||
    !body.vectors.every(validVector)
  ) {
    throw new EmbeddingServiceError("embedding service returned an incompatible vector", response.status, false);
  }
  return body.vectors as number[][];
}

/** Embeds one input. */
export async function embedText(
  text: string,
  options: { role: EmbeddingRole; task: EmbeddingTask; title?: string | null; timeoutMs?: number },
): Promise<number[]> {
  const [vector] = await embedTexts({
    role: options.role,
    task: options.task,
    inputs: [{ text, title: options.title ?? null }],
    timeoutMs: options.timeoutMs,
  });
  return vector!;
}

// ── Per-caller conventions ──────────────────────────────────────────────
// Memories use EmbeddingGemma's search prompts; skills use none, which ranked
// best for short name + description documents.

/** Memory texts for the index (role document, task search). */
export function embedMemoryDocuments(texts: ReadonlyArray<string>, timeoutMs = BACKGROUND_EMBED_TIMEOUT_MS): Promise<number[][]> {
  return embedTexts({
    role: "document",
    task: "search",
    inputs: texts.map((text) => ({ text: text.slice(0, EMBEDDING_MAX_CHARS) })),
    timeoutMs,
  });
}

/** A memory search query (role query, task search). */
export function embedMemoryQuery(query: string, timeoutMs = INTERACTIVE_EMBED_TIMEOUT_MS): Promise<number[]> {
  return embedText(query, { role: "query", task: "search", timeoutMs });
}

/** Longer guide text blurs the ranking; a skill is indexed by name and description. */
export const SKILL_DESCRIPTION_MAX_CHARS = 4000;

/** The text a skill is indexed under: its name, then its description. */
export function skillDocument(skill: { name: string; description: string }): string {
  return [skill.name, skill.description].filter(Boolean).join("\n");
}

/**
 * The embedding input for a stored skill document: the name as the title and
 * the description as the text, so the service joins `{name}\n{description}`.
 * A skill with no description embeds its name alone.
 */
export function skillEmbeddingInput(name: string, document: string): EmbeddingInput {
  const description = document.startsWith(`${name}\n`) ? document.slice(name.length + 1).trim() : document.trim();
  if (!description || description === name) return { text: name, title: null };
  return { text: description.slice(0, SKILL_DESCRIPTION_MAX_CHARS), title: name };
}

/** Skill documents for the index (role document, task none). */
export function embedSkillDocuments(
  skills: ReadonlyArray<{ name: string; document: string }>,
  timeoutMs = BACKGROUND_EMBED_TIMEOUT_MS,
): Promise<number[][]> {
  return embedTexts({
    role: "document",
    task: "none",
    inputs: skills.map((skill) => skillEmbeddingInput(skill.name, skill.document)),
    timeoutMs,
  });
}

/** A skill search query (role query, task none). */
export function embedSkillQuery(query: string, timeoutMs = INTERACTIVE_EMBED_TIMEOUT_MS): Promise<number[]> {
  return embedText(query, { role: "query", task: "none", timeoutMs });
}

// Format a JS number[] as the literal pgvector accepts when bound as text.
// pgvector wants `[0.1,0.2,...]` (square brackets, comma-separated, no spaces).
export function vectorLiteral(vec: number[]): string {
  return `[${vec.join(",")}]`;
}
