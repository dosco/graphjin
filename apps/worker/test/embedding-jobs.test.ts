import { afterAll, beforeAll, expect, it, vi } from 'vitest';
import pg from 'pg';
import PgBoss from 'pg-boss';

const state = vi.hoisted(() => ({
  pool: null as unknown as pg.Pool,
  boss: null as unknown as PgBoss,
  enabled: true,
}));
vi.mock('@neko/db', () => ({ pool: () => state.pool, readSemanticSearchEnabled: async () => state.enabled }));
vi.mock('@neko/db/jobs', () => ({ boss: async () => state.boss, QUEUE: { EMBEDDING_INDEX: 'embedding_index' } }));
import { dispatchEmbeddingJobs, runEmbeddingIndexJob } from '../../../packages/llm/src/embedding-jobs';
import { setSemanticSearchEnabledForTests } from '../../../packages/llm/src/semantic-search';

// Dedicated disposable database only: this test creates its own minimal tables.
const url = process.env.EMBEDDING_TEST_DATABASE_URL;
beforeAll(async () => {
  if (!url) return;
  state.pool = new pg.Pool({ connectionString: url });
  state.boss = new PgBoss(url);
  await state.boss.start();
  await state.boss.createQueue('embedding_index');
  await state.pool.query(`CREATE EXTENSION IF NOT EXISTS vector;
    DROP TABLE IF EXISTS work_memory, skill_embedding;
    CREATE TABLE work_memory (id uuid PRIMARY KEY, org_id text, text text, embedding vector(768),
      archived_at timestamptz, suppressed boolean DEFAULT false, expires_at timestamptz, updated_at timestamptz DEFAULT now());
    CREATE TABLE skill_embedding (id uuid PRIMARY KEY DEFAULT gen_random_uuid(), org_id text, skill_name text,
      document text DEFAULT '', embedding vector(768), updated_at timestamptz DEFAULT now());`);
});
afterAll(async () => {
  if (!url) return;
  setSemanticSearchEnabledForTests(null);
  await state.boss.stop({ graceful: true });
  await state.pool.end();
});

it.skipIf(!url)('dispatches only while on, deduplicates, batches, and rejects stale results', async () => {
  const id = '00000000-0000-4000-8000-000000000001';
  await state.pool.query('INSERT INTO work_memory(id,org_id,text) VALUES($1,$2,$3)', [id, 'org-a', 'original']);
  await state.pool.query("INSERT INTO skill_embedding(org_id,skill_name,document) VALUES('org-a','xlsx',$1)", ['xlsx\nSpreadsheets']);
  const count = async () =>
    (await state.pool.query("SELECT count(*)::int AS n FROM pgboss.job WHERE name='embedding_index' AND state <= 'active'")).rows[0].n;

  setSemanticSearchEnabledForTests(false);
  await dispatchEmbeddingJobs();
  expect(await count()).toBe(0);

  setSemanticSearchEnabledForTests(true);
  await Promise.all([dispatchEmbeddingJobs(), dispatchEmbeddingJobs()]);
  expect(await count()).toBe(1);
  const durable = await state.boss.fetch('embedding_index');
  expect(durable[0]!.data).toEqual({ reason: 'sweep' });

  const vector = [1, ...Array(767).fill(0)];
  const embedMemories = vi.fn(async (texts: string[]) => {
    // An edit lands while the batch is embedded: that row keeps its null vector.
    await state.pool.query('UPDATE work_memory SET text=$2 WHERE id=$1', [id, 'edited']);
    return texts.map(() => vector);
  });
  const embedSkills = vi.fn(async (skills: unknown[]) => skills.map(() => vector));
  let rounds = 0;
  const result = await runEmbeddingIndexJob(durable[0]!.data as never, {
    embedMemories,
    embedSkills,
    enabled: async () => ++rounds < 3,
  });
  expect(result.embedded).toBe(2);
  expect(embedMemories).toHaveBeenCalledWith(['original']);
  expect(embedSkills).toHaveBeenCalledWith([{ name: 'xlsx', document: 'xlsx\nSpreadsheets' }]);
  expect((await state.pool.query('SELECT embedding FROM work_memory WHERE id=$1', [id])).rows[0].embedding).toBeNull();
  expect((await state.pool.query('SELECT embedding IS NOT NULL AS indexed FROM skill_embedding')).rows[0].indexed).toBe(true);

  const done = await runEmbeddingIndexJob(undefined, {
    embedMemories: async (texts) => texts.map(() => vector),
    enabled: async () => true,
  });
  expect(done).toEqual({ embedded: 1, remaining: false });
  expect((await state.pool.query('SELECT embedding IS NOT NULL AS indexed FROM work_memory WHERE id=$1', [id])).rows[0].indexed).toBe(true);

  // Archived rows are not indexed; turning the switch off keeps the backlog.
  await state.pool.query('UPDATE work_memory SET embedding=NULL, archived_at=now() WHERE id=$1', [id]);
  const untouched = vi.fn(async (texts: string[]) => texts.map(() => vector));
  expect(await runEmbeddingIndexJob(undefined, { embedMemories: untouched, enabled: async () => true })).toEqual({ embedded: 0, remaining: false });
  expect(untouched).not.toHaveBeenCalled();

  await state.boss.complete('embedding_index', durable[0]!.id);
  await state.pool.query("INSERT INTO skill_embedding(org_id,skill_name,document) VALUES('org-a','docx','docx')");
  setSemanticSearchEnabledForTests(false);
  await dispatchEmbeddingJobs();
  expect(await count()).toBe(0);
  setSemanticSearchEnabledForTests(true);
  await dispatchEmbeddingJobs();
  expect(await count()).toBe(1);
});
