/**
 * Single-tenant admin-org resolver.
 *
 * Returns the id of the (currently one) organization row, creating it on
 * first call when the table is empty. Result is cached per-process.
 *
 * Both web and worker import this so the "current org" is consistent
 * regardless of which side of the app is running. When SSO/multi-tenant
 * lands the web side switches to session-derived org ids; the worker stays
 * single-tenant per process or gets refactored to per-job.
 *
 * Nothing in the schema forbids a second organization row, and on a fresh
 * database web, worker, the graphjin-secret-init one-shot, and the demo
 * seed can all arrive here at once — so creation is serialized under a
 * transaction-scoped advisory lock, and every reader picks the same row
 * via a stable ORDER BY. (Compose ordering usually prevents the race, but
 * `pnpm dev` and non-Compose topologies have no such ordering.)
 */

import { randomUUID } from "node:crypto";
import { sql } from "drizzle-orm";
import { db } from "./index";
import { organization } from "./schema";

export const ORG_BOOTSTRAP_LOCK_KEY = "openneko.organization.bootstrap";

let _cachedOrgId: string | null = null;

async function selectFirstOrgId(
  runner: Pick<ReturnType<typeof db>, "select">,
): Promise<string | null> {
  const rows = await runner
    .select({ id: organization.id })
    .from(organization)
    .orderBy(organization.created_at, organization.id)
    .limit(1);
  return rows[0]?.id ?? null;
}

export async function getOrgId(): Promise<string> {
  if (_cachedOrgId) return _cachedOrgId;

  // Steady state: the org exists, one ordered select, no lock traffic.
  const existing = await selectFirstOrgId(db());
  if (existing) {
    _cachedOrgId = existing;
    return _cachedOrgId;
  }

  const resolved = await db().transaction(async (tx) => {
    await tx.execute(
      sql`select pg_advisory_xact_lock(hashtext(${ORG_BOOTSTRAP_LOCK_KEY}))`,
    );
    // Re-check under the lock: whoever held it before us created the row.
    const raced = await selectFirstOrgId(tx);
    if (raced) return raced;
    const newId = randomUUID();
    await tx.insert(organization).values({
      id: newId,
      name: "My Workspace",
    });
    return newId;
  });
  _cachedOrgId = resolved;
  return _cachedOrgId;
}

/** Test-only: drop the cache so different tests can use different orgs. */
export function _resetOrgIdCacheForTesting(): void {
  _cachedOrgId = null;
}
