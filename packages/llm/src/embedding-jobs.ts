import { pool } from '@neko/db';
import { boss, QUEUE } from '@neko/db/jobs';
import { embedText, vectorLiteral } from './embedding';

export type EmbeddingIndexPayload = { kind: 'memory' | 'concept' | 'skill'; orgId: string; id: string };

// Missing vectors are the durable indexing backlog, written atomically with
// content. A periodic dispatcher closes the save/enqueue crash window.
export async function dispatchEmbeddingJobs(): Promise<void> {
  const b = await boss();
  const client = await pool().connect();
  try {
    await client.query('BEGIN');
    const lock = await client.query("SELECT pg_try_advisory_xact_lock(hashtext('embedding-dispatch')) AS acquired");
    if (!lock.rows[0].acquired) return;
    const { rows } = await client.query(`
      WITH pending AS (
        SELECT singleton_key FROM pgboss.job WHERE name = $1 AND state <= 'active'
      ), candidates AS (
        SELECT 'memory' AS kind, id, org_id, updated_at FROM work_memory
        WHERE embedding IS NULL AND archived_at IS NULL AND NOT suppressed AND btrim(text) <> ''
          AND (expires_at IS NULL OR expires_at > now())
        UNION ALL
        SELECT 'concept', id, org_id, updated_at FROM library_concept
        WHERE embedding IS NULL AND archived_at IS NULL AND btrim(title || body) <> ''
        UNION ALL
        SELECT 'skill', id, org_id, updated_at FROM skill_embedding WHERE embedding IS NULL
      ), ranked AS (
        SELECT *, row_number() OVER (PARTITION BY org_id ORDER BY updated_at, id) AS turn
        FROM candidates c WHERE NOT EXISTS (
          SELECT 1 FROM pending p WHERE p.singleton_key = c.kind || ':' || c.id::text
        )
      ) SELECT kind, id, org_id FROM ranked ORDER BY turn, updated_at, id
      LIMIT greatest(0, least(32, 128 - (SELECT count(*) FROM pending)))`, [QUEUE.EMBEDDING_INDEX]);
    for (const row of rows) {
      await b.send(QUEUE.EMBEDDING_INDEX, { kind: row.kind, orgId: row.org_id, id: row.id }, {
        singletonKey: `${row.kind}:${row.id}`, retryLimit: 8, retryDelay: 30,
        retryBackoff: true, expireInSeconds: 180,
        db: { executeSql: (text, values) => client.query(text, values) },
      });
    }
    await client.query('COMMIT');
  } finally {
    await client.query('ROLLBACK').catch(() => {});
    client.release();
  }
}

export async function runEmbeddingIndexJob(payload: EmbeddingIndexPayload): Promise<void> {
  if (!payload || !['memory', 'concept', 'skill'].includes(payload.kind) ||
      typeof payload.orgId !== 'string' || typeof payload.id !== 'string') {
    throw new Error('Invalid embedding job');
  }
  const memory = payload.kind === 'memory';
  const table = { memory: 'work_memory', concept: 'library_concept', skill: 'skill_embedding' }[payload.kind];
  const expression = {
    memory: 'text',
    concept: "title || E'\\n' || coalesce(description, '') || E'\\n' || body",
    skill: 'document',
  }[payload.kind];
  const eligible = payload.kind === 'skill'
    ? 'embedding IS NULL'
    : `embedding IS NULL AND archived_at IS NULL${memory ? ' AND NOT suppressed AND (expires_at IS NULL OR expires_at > now())' : ''}`;
  const client = await pool().connect();
  try {
    await client.query('BEGIN');
    // Cross-worker admission: no more than one background model request.
    const lock = await client.query("SELECT pg_try_advisory_xact_lock(hashtext('embedding-processing')) AS acquired");
    if (!lock.rows[0].acquired) throw new Error('Embedding processing is busy');
    const { rows } = await client.query(`SELECT ${expression} AS content FROM ${table}
      WHERE org_id = $1 AND id = $2 AND ${eligible}`, [payload.orgId, payload.id]);
    if (rows.length) {
      const content: string = rows[0].content;
      const vector = vectorLiteral(await embedText(content.slice(0, memory ? 32000 : 4000), 150_000));
      // A concurrent edit/archive/delete must never receive an obsolete vector.
      await client.query(`UPDATE ${table} SET embedding = $3::vector
        WHERE org_id = $1 AND id = $2 AND ${eligible} AND ${expression} = $4`,
      [payload.orgId, payload.id, vector, content]);
    }
    await client.query('COMMIT');
  } finally {
    await client.query('ROLLBACK').catch(() => {});
    client.release();
  }
}
