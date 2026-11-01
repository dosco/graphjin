import { and, db, eq, inArray, isNotNull, isNull, skill_embedding, sql } from "@neko/db";
import { embedSkillQuery, skillDocument, vectorLiteral } from "../embedding";
import { semanticSearchEnabled } from "../semantic-search";
import { entitlementActorForRun, runHeldItemIds } from "./entitlement-scope";
import { interleaveResults, keywordTsQuery } from "./keyword-search";

export type SkillSearchResult = { name: string; score: number };

/**
 * Keeps the org's skill index in step with its installed skills. A new or
 * changed description clears the vector. Nothing is embedded here, so a chat
 * turn never waits on the model: when semantic search is on, the embedding job
 * indexes the cleared rows in the background.
 */
export async function syncSkillIndex(
  orgId: string,
  skills: ReadonlyArray<{ name: string; description: string }>,
): Promise<void> {
  const existing = await db()
    .select({ name: skill_embedding.skill_name, document: skill_embedding.document })
    .from(skill_embedding)
    .where(eq(skill_embedding.org_id, orgId));
  const known = new Map(existing.map((row) => [row.name, row.document]));
  const installed = new Set(skills.map((skill) => skill.name));

  let changed = false;
  for (const skill of skills) {
    const document = skillDocument(skill);
    if (known.get(skill.name) === document) continue;
    changed = true;
    await db()
      .insert(skill_embedding)
      .values({ org_id: orgId, skill_name: skill.name, document, embedding: null })
      .onConflictDoUpdate({
        target: [skill_embedding.org_id, skill_embedding.skill_name],
        set: { document, embedding: null, updated_at: new Date() },
      });
  }
  const removed = existing.map((row) => row.name).filter((name) => !installed.has(name));
  if (removed.length) {
    await db()
      .delete(skill_embedding)
      .where(and(eq(skill_embedding.org_id, orgId), inArray(skill_embedding.skill_name, removed)));
  }
  if (changed && (await semanticSearchEnabled())) {
    const { requestEmbeddingBackfill } = await import("../embedding-jobs");
    await requestEmbeddingBackfill("skills").catch((error) => {
      // The minute sweep queues it instead.
      console.warn("[skill-index] could not queue indexing:", error instanceof Error ? error.message : error);
    });
  }
}

function clampLimit(limit: number | undefined): number {
  return Math.min(Math.max(limit ?? 3, 1), 10);
}

/**
 * Full-text search over each skill's name and description. With
 * `unindexedOnly`, only skills that have no vector yet.
 */
async function keywordSkillSearch(
  orgId: string,
  held: string[] | null | undefined,
  query: string,
  limit: number,
  unindexedOnly = false,
): Promise<SkillSearchResult[]> {
  const tsquery = keywordTsQuery(query);
  const document = sql`to_tsvector('english', ${skill_embedding.document})`;
  const rank = sql<number>`ts_rank_cd(${document}, ${tsquery})`;
  const rows = await db()
    .select({ name: skill_embedding.skill_name, rank })
    .from(skill_embedding)
    .where(
      and(
        eq(skill_embedding.org_id, orgId),
        ...(held ? [inArray(skill_embedding.skill_name, held)] : []),
        ...(unindexedOnly ? [isNull(skill_embedding.embedding)] : []),
        sql`${document} @@ ${tsquery}`,
      ),
    )
    .orderBy(sql`${rank} desc`, skill_embedding.skill_name)
    .limit(limit);
  return rows.map((row) => ({ name: row.name, score: Number(row.rank) || 0 }));
}

/**
 * The installed skills that best match the query and that the run's actor
 * holds. With semantic search on, skills are ranked by meaning; skills not yet
 * indexed are still found by keyword, interleaved after the closest ones.
 * With it off, or when the embedding service fails or is slow, the search is
 * by keyword alone.
 */
export async function searchSkillsForRun(input: {
  orgId: string;
  runId: string;
  query: string;
  limit?: number;
}): Promise<SkillSearchResult[]> {
  const query = input.query.trim();
  if (!query) return [];
  const actor = await entitlementActorForRun(input.orgId, input.runId);
  if (!actor) return [];
  const held = await runHeldItemIds(actor, "skill");
  if (held && held.length === 0) return [];
  const limit = clampLimit(input.limit);

  if (!(await semanticSearchEnabled())) return keywordSkillSearch(input.orgId, held, query, limit);
  let vector: string;
  try {
    vector = vectorLiteral(await embedSkillQuery(query));
  } catch (error) {
    console.warn(
      "[skill-index] semantic search unavailable; searching by keyword:",
      error instanceof Error ? error.message : error,
    );
    return keywordSkillSearch(input.orgId, held, query, limit);
  }
  const distance = sql<number>`${skill_embedding.embedding} <=> ${vector}::vector`;
  const [rows, unindexed] = await Promise.all([
    db()
      .select({ name: skill_embedding.skill_name, distance })
      .from(skill_embedding)
      .where(
        and(
          eq(skill_embedding.org_id, input.orgId),
          isNotNull(skill_embedding.embedding),
          ...(held ? [inArray(skill_embedding.skill_name, held)] : []),
        ),
      )
      .orderBy(distance)
      .limit(limit),
    keywordSkillSearch(input.orgId, held, query, limit, true),
  ]);
  const semantic = rows.map((row) => ({ name: row.name, score: 1 - Number(row.distance) }));
  return interleaveResults(semantic, unindexed, (result) => result.name, limit);
}
