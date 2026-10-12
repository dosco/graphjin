import { and, db, eq, inArray, isNotNull, skill_embedding, sql } from "@neko/db";
import { embedText, vectorLiteral } from "../embedding";
import { entitlementActorForRun, runHeldItemIds } from "./entitlement-scope";

export type SkillSearchResult = { name: string; score: number };

const INLINE_EMBEDS = 64;

/** A skill is indexed by its name and description; longer guide text blurs MiniLM's ranking. */
function skillDocument(skill: { name: string; description: string }): string {
  return [skill.name, skill.description].filter(Boolean).join("\n");
}

/**
 * Keeps the org's skill index in step with its installed skills. A new or
 * changed description clears the vector; up to 64 are embedded now and the embedding
 * job queue indexes the rest.
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

  for (const skill of skills) {
    const document = skillDocument(skill);
    if (known.get(skill.name) === document) continue;
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

  const pending = await db()
    .select({ id: skill_embedding.id, document: skill_embedding.document })
    .from(skill_embedding)
    .where(and(eq(skill_embedding.org_id, orgId), sql`${skill_embedding.embedding} IS NULL`))
    .limit(INLINE_EMBEDS);
  for (const row of pending) {
    const vector = vectorLiteral(await embedText(row.document));
    // A guide changed in between keeps its null vector for the next pass.
    await db()
      .update(skill_embedding)
      .set({ embedding: sql`${vector}::vector` })
      .where(and(eq(skill_embedding.id, row.id), eq(skill_embedding.document, row.document)));
  }
}

/** The installed skills closest in meaning to the query that the run's actor holds. */
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
  const vector = vectorLiteral(await embedText(query));
  const distance = sql<number>`${skill_embedding.embedding} <=> ${vector}::vector`;
  const rows = await db()
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
    .limit(Math.min(Math.max(input.limit ?? 3, 1), 10));
  return rows.map((row) => ({ name: row.name, score: 1 - Number(row.distance) }));
}
