import { afterAll, beforeAll, expect, it, vi } from 'vitest';
import pg from 'pg';
import PgBoss from 'pg-boss';

const state = vi.hoisted(() => ({ pool: null as unknown as pg.Pool, boss: null as unknown as PgBoss, embed: vi.fn() }));
vi.mock('@neko/db', () => ({ pool: () => state.pool }));
vi.mock('@neko/db/jobs', () => ({ boss: async () => state.boss, QUEUE: { EMBEDDING_INDEX: 'embedding_index' } }));
vi.mock('../../../packages/llm/src/embedding', () => ({ embedText: state.embed, vectorLiteral: (v: number[]) => JSON.stringify(v) }));
import { dispatchEmbeddingJobs, runEmbeddingIndexJob } from '../../../packages/llm/src/embedding-jobs';

// Dedicated disposable database only: this test creates its own minimal tables.
const url = process.env.EMBEDDING_TEST_DATABASE_URL;
beforeAll(async () => {
  if (!url) return;
  state.pool = new pg.Pool({ connectionString: url });
  state.boss = new PgBoss(url);
  await state.boss.start();
  await state.boss.createQueue('embedding_index');
  await state.pool.query(`CREATE EXTENSION IF NOT EXISTS vector;
    CREATE TABLE work_memory (id uuid PRIMARY KEY, org_id text, text text, embedding vector(384),
      archived_at timestamptz, suppressed boolean DEFAULT false, expires_at timestamptz, updated_at timestamptz DEFAULT now());
    CREATE TABLE library_concept (id uuid PRIMARY KEY, org_id text, title text, description text, body text,
      embedding vector(384), archived_at timestamptz, updated_at timestamptz DEFAULT now());
    CREATE TABLE skill_embedding (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), org_id text, skill_name text,
      document text DEFAULT '', embedding vector(384), updated_at timestamptz DEFAULT now());`);
});
afterAll(async () => {
  if (!url) return;
  await state.boss.stop({ graceful: true });
  await state.pool.end();
});
it.skipIf(!url)('durably dispatches, deduplicates, retries, scopes updates and rejects stale results', async () => {
  const id = '00000000-0000-4000-8000-000000000001';
  const job = { kind: 'memory' as const, orgId: 'org-a', id };
  await state.pool.query('INSERT INTO work_memory(id,org_id,text) VALUES($1,$2,$3)', [id, job.orgId, 'original']);
  await Promise.all([dispatchEmbeddingJobs(), dispatchEmbeddingJobs()]);
  expect((await state.pool.query("SELECT count(*)::int AS n FROM pgboss.job WHERE name='embedding_index'")).rows[0].n).toBe(1);
  const durable = await state.boss.fetch('embedding_index');
  expect(durable[0].data).toEqual(job);
  state.embed.mockRejectedValueOnce(new Error('offline'));
  await expect(runEmbeddingIndexJob(job)).rejects.toThrow('offline');
  expect((await state.pool.query('SELECT embedding FROM work_memory WHERE id=$1', [id])).rows[0].embedding).toBeNull();
  const blocker = await state.pool.connect();
  await blocker.query("BEGIN; SELECT pg_advisory_xact_lock(hashtext('embedding-processing'))");
  await expect(runEmbeddingIndexJob(job)).rejects.toThrow('busy');
  await blocker.query('ROLLBACK'); blocker.release();
  const vector = [1, ...Array(383).fill(0)];
  state.embed.mockImplementationOnce(async () => {
    await state.pool.query('UPDATE work_memory SET text=$2 WHERE id=$1', [id, 'edited']);
    return vector;
  });
  await runEmbeddingIndexJob(job);
  expect((await state.pool.query('SELECT embedding FROM work_memory WHERE id=$1', [id])).rows[0].embedding).toBeNull();
  state.embed.mockResolvedValue(vector);
  await runEmbeddingIndexJob({ ...job, orgId: 'wrong-org' });
  expect(state.embed).toHaveBeenCalledTimes(2);
  await runEmbeddingIndexJob(job);
  expect(state.embed).toHaveBeenLastCalledWith('edited', 150_000);
  expect((await state.pool.query('SELECT embedding IS NOT NULL AS indexed FROM work_memory WHERE id=$1', [id])).rows[0].indexed).toBe(true);
  const conceptId = '00000000-0000-4000-8000-000000000002';
  await state.pool.query('INSERT INTO library_concept(id,org_id,title,description,body) VALUES($1,$2,$3,$4,$5)',
    [conceptId, 'org-b', 'Title', null, 'Body']);
  await runEmbeddingIndexJob({ kind: 'concept', orgId: 'org-b', id: conceptId });
  expect(state.embed).toHaveBeenLastCalledWith('Title\n\nBody', 150_000);
  expect((await state.pool.query('SELECT embedding IS NOT NULL AS indexed FROM library_concept WHERE id=$1', [conceptId])).rows[0].indexed).toBe(true);
  await state.pool.query('UPDATE work_memory SET embedding=NULL, archived_at=now() WHERE id=$1', [id]);
  const calls = state.embed.mock.calls.length;
  await runEmbeddingIndexJob(job);
  expect(state.embed).toHaveBeenCalledTimes(calls);
  await state.boss.complete('embedding_index', durable[0].id);
  await dispatchEmbeddingJobs();
  expect((await state.pool.query("SELECT count(*)::int AS n FROM pgboss.job WHERE name='embedding_index'")).rows[0].n).toBe(1);
});
