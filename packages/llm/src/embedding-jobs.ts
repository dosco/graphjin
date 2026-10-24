import type pg from 'pg';
import { pool } from '@neko/db';
import { boss, QUEUE } from '@neko/db/jobs';
import {
  EMBEDDING_MAX_BATCH,
  embedMemoryDocuments,
  embedSkillDocuments,
  isRetryableEmbeddingError,
  vectorLiteral,
} from './embedding';
import { semanticSearchEnabled } from './semantic-search';

/**
 * The payload is informational: every job drains the shared backlog. Jobs
 * queued by an older OpenNeko carry { kind, orgId, id } and are handled the
 * same way.
 */
export type EmbeddingIndexPayload = { reason?: string; kind?: string; orgId?: string; id?: string } | null | undefined;

/** Rows with a null vector are the durable backlog, written atomically with content. */
const MEMORY_ELIGIBLE = `embedding IS NULL AND archived_at IS NULL AND NOT suppressed AND btrim(text) <> ''
  AND (expires_at IS NULL OR expires_at > now())`;
const SKILL_ELIGIBLE = 'embedding IS NULL';

/** One job embeds for at most this long; the minute sweep queues the next. */
export const EMBEDDING_JOB_BUDGET_MS = 5 * 60_000;
/** Waits between attempts when the service is busy (429) or loading/unavailable (503). */
export const EMBEDDING_BACKOFF_MS = [2_000, 4_000, 8_000, 16_000, 30_000];

async function hasBacklog(client: pg.PoolClient | pg.Pool): Promise<boolean> {
  const { rows } = await client.query(`SELECT
    EXISTS (SELECT 1 FROM work_memory WHERE ${MEMORY_ELIGIBLE}) OR
    EXISTS (SELECT 1 FROM skill_embedding WHERE ${SKILL_ELIGIBLE}) AS pending`);
  return rows[0]?.pending === true;
}

/** How many memories and skills wait for a vector. */
export async function embeddingBacklogCount(client: pg.PoolClient | pg.Pool): Promise<number> {
  const { rows } = await client.query(`SELECT
    (SELECT count(*) FROM work_memory WHERE ${MEMORY_ELIGIBLE}) +
    (SELECT count(*) FROM skill_embedding WHERE ${SKILL_ELIGIBLE}) AS waiting`);
  return Number(rows[0]?.waiting ?? 0);
}

/**
 * Queue one backlog job unless one is already queued or running. pg-boss
 * standard queues do not deduplicate by singleton key, so an advisory lock
 * serializes the check and the send.
 */
export async function requestEmbeddingBackfill(reason = 'sweep'): Promise<boolean> {
  const b = await boss();
  await b.createQueue(QUEUE.EMBEDDING_INDEX);
  const client = await pool().connect();
  try {
    await client.query('BEGIN');
    const lock = await client.query("SELECT pg_try_advisory_xact_lock(hashtext('embedding-dispatch')) AS acquired");
    if (!lock.rows[0].acquired) return false;
    const pending = await client.query(
      "SELECT 1 FROM pgboss.job WHERE name = $1 AND state <= 'active' LIMIT 1",
      [QUEUE.EMBEDDING_INDEX],
    );
    if (pending.rows.length) return false;
    await b.send(QUEUE.EMBEDDING_INDEX, { reason }, {
      singletonKey: 'backlog', retryLimit: 8, retryDelay: 30, retryBackoff: true,
      expireInSeconds: 15 * 60,
      db: { executeSql: (text, values) => client.query(text, values) },
    });
    await client.query('COMMIT');
    return true;
  } finally {
    await client.query('ROLLBACK').catch(() => {});
    client.release();
  }
}

/**
 * The minute sweep. With semantic search off it schedules nothing and the
 * backlog (rows with a null vector) stays for when it is turned on.
 */
export async function dispatchEmbeddingJobs(): Promise<void> {
  if (!(await semanticSearchEnabled())) return;
  if (!(await hasBacklog(pool()))) return;
  await requestEmbeddingBackfill('sweep');
}

export type EmbeddingJobDeps = {
  enabled: () => Promise<boolean>;
  embedMemories: (texts: string[]) => Promise<number[][]>;
  embedSkills: (skills: Array<{ name: string; document: string }>) => Promise<number[][]>;
  sleep: (ms: number) => Promise<void>;
  now: () => number;
  batchSize: number;
  budgetMs: number;
};

const defaultDeps: EmbeddingJobDeps = {
  enabled: () => semanticSearchEnabled(),
  embedMemories: (texts) => embedMemoryDocuments(texts),
  embedSkills: (skills) => embedSkillDocuments(skills),
  sleep: (ms) => new Promise((resolve) => setTimeout(resolve, ms)),
  now: () => Date.now(),
  batchSize: EMBEDDING_MAX_BATCH,
  budgetMs: EMBEDDING_JOB_BUDGET_MS,
};

/** Semantic search was turned off while a batch waited to retry. */
export class SemanticSearchTurnedOff extends Error {
  constructor() {
    super('Semantic search was turned off');
    this.name = 'SemanticSearchTurnedOff';
  }
}

/**
 * Retries a batch while the service is busy or loading, backing off between
 * attempts. Stops early when semantic search is turned off. A final failure
 * throws, and pg-boss retries the job later with its own backoff.
 */
export async function withEmbeddingBackoff<T>(
  call: () => Promise<T>,
  deps: Pick<EmbeddingJobDeps, 'sleep' | 'enabled'>,
): Promise<T> {
  for (let attempt = 0; ; attempt++) {
    try {
      return await call();
    } catch (error) {
      const delay = EMBEDDING_BACKOFF_MS[attempt];
      if (!isRetryableEmbeddingError(error) || delay === undefined) throw error;
      await deps.sleep(delay);
      if (!(await deps.enabled())) throw new SemanticSearchTurnedOff();
    }
  }
}

type Batch<Row> = {
  rows: Row[];
  embed: (rows: Row[]) => Promise<number[][]>;
  write: (row: Row, vector: string) => Promise<void>;
  id: (row: Row) => string;
};

/**
 * Embeds one batch. When the service rejects the batch outright (not busy),
 * each row is tried alone so one bad row cannot block the backlog; a row that
 * still fails is skipped for the rest of this job.
 */
async function embedBatch<Row>(batch: Batch<Row>, deps: EmbeddingJobDeps, skipped: Set<string>): Promise<number> {
  const writeAll = async (rows: Row[], vectors: number[][]) => {
    for (let i = 0; i < rows.length; i++) await batch.write(rows[i]!, vectorLiteral(vectors[i]!));
  };
  try {
    await writeAll(batch.rows, await withEmbeddingBackoff(() => batch.embed(batch.rows), deps));
    return batch.rows.length;
  } catch (error) {
    if (isRetryableEmbeddingError(error) || error instanceof SemanticSearchTurnedOff) throw error;
    if (batch.rows.length === 1) {
      skipped.add(batch.id(batch.rows[0]!));
      console.warn(`[embedding] skipped ${batch.id(batch.rows[0]!)}: ${error instanceof Error ? error.message : error}`);
      return 0;
    }
  }
  let done = 0;
  for (const row of batch.rows) done += await embedBatch({ ...batch, rows: [row] }, deps, skipped);
  return done;
}

/**
 * Drains the embedding backlog in batches of up to 64: memories (role
 * document, task search) and skills (task none, name as title). Runs only
 * while semantic search is on; one worker at a time.
 */
export async function runEmbeddingIndexJob(
  _payload?: EmbeddingIndexPayload,
  overrides: Partial<EmbeddingJobDeps> = {},
): Promise<{ embedded: number; remaining: boolean }> {
  const deps = { ...defaultDeps, ...overrides };
  if (!(await deps.enabled())) return { embedded: 0, remaining: false };
  const client = await pool().connect();
  let locked = false;
  try {
    // Cross-worker admission: no more than one background model request.
    const lock = await client.query("SELECT pg_try_advisory_lock(hashtext('embedding-processing')) AS acquired");
    locked = lock.rows[0]?.acquired === true;
    if (!locked) return { embedded: 0, remaining: true };
    const started = deps.now();
    const skipped = new Set<string>();
    let embedded = 0;
    for (;;) {
      if (!(await deps.enabled())) return { embedded, remaining: true };
      if (deps.now() - started > deps.budgetMs) return { embedded, remaining: true };
      const skippedIds = [...skipped];
      const memories = await client.query<{ id: string; org_id: string; text: string }>(
        `SELECT id, org_id, text FROM work_memory WHERE ${MEMORY_ELIGIBLE}
          AND NOT (id::text = ANY($2::text[])) ORDER BY updated_at, id LIMIT $1`,
        [deps.batchSize, skippedIds],
      );
      const skills = await client.query<{ id: string; org_id: string; skill_name: string; document: string }>(
        `SELECT id, org_id, skill_name, document FROM skill_embedding WHERE ${SKILL_ELIGIBLE}
          AND NOT (id::text = ANY($2::text[])) ORDER BY updated_at, id LIMIT $1`,
        [deps.batchSize, skippedIds],
      );
      if (!memories.rows.length && !skills.rows.length) return { embedded, remaining: false };
      if (memories.rows.length) {
        embedded += await embedBatch({
          rows: memories.rows,
          id: (row) => row.id,
          embed: (rows) => deps.embedMemories(rows.map((row) => row.text)),
          // A concurrent edit/archive/delete must never receive an obsolete vector.
          write: async (row, vector) => {
            await client.query(`UPDATE work_memory SET embedding = $3::vector
              WHERE org_id = $1 AND id = $2 AND ${MEMORY_ELIGIBLE} AND text = $4`, [row.org_id, row.id, vector, row.text]);
          },
        }, deps, skipped);
      }
      if (skills.rows.length) {
        embedded += await embedBatch({
          rows: skills.rows,
          id: (row) => row.id,
          embed: (rows) => deps.embedSkills(rows.map((row) => ({ name: row.skill_name, document: row.document }))),
          write: async (row, vector) => {
            await client.query(`UPDATE skill_embedding SET embedding = $3::vector
              WHERE org_id = $1 AND id = $2 AND ${SKILL_ELIGIBLE} AND document = $4`, [row.org_id, row.id, vector, row.document]);
          },
        }, deps, skipped);
      }
    }
  } catch (error) {
    // Turning it off stops the work; the remaining rows keep their null vector.
    if (error instanceof SemanticSearchTurnedOff) return { embedded: 0, remaining: true };
    throw error;
  } finally {
    if (locked) await client.query("SELECT pg_advisory_unlock(hashtext('embedding-processing'))").catch(() => {});
    client.release();
  }
}
