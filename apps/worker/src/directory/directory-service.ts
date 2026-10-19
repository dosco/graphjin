import {
  db,
  directory_sync_state,
  eq,
  reconcileDirectorySnapshot,
  sql,
  type DirectorySnapshot,
  type DirectorySyncStats,
} from "@neko/db";
import type { ApplyDirectoryChangeResult, DirectoryCapabilities, DirectoryChange, ListDirectoryResult } from "./types.js";

export interface DirectoryProviderInfo {
  id: string;
  capabilities: DirectoryCapabilities;
}

export interface DirectorySource {
  getDirectoryProvider(): DirectoryProviderInfo | null;
  listDirectory(cursor: string | null): Promise<ListDirectoryResult>;
  applyDirectoryChange(change: DirectoryChange): Promise<ApplyDirectoryChangeResult>;
}

export type DirectoryStatus = {
  provider: { pluginName: string; providerLabel: string; canCreateUsers: boolean; canDeactivateUsers: boolean } | null;
  state: {
    status: string;
    startedAt: string | null;
    finishedAt: string | null;
    lastError: string | null;
    stats: Partial<DirectorySyncStats>;
  };
};

export const DIRECTORY_SYNC_CRON = "0 */6 * * *";
const MAX_PAGES = 500;

export class DirectorySyncError extends Error {
  constructor(public readonly code: "no_provider" | "running" | "invalid" | "unsupported", message: string) {
    super(message);
    this.name = "DirectorySyncError";
  }
}

export async function directoryStatus(source: DirectorySource | null, orgId: string): Promise<DirectoryStatus> {
  const provider = source?.getDirectoryProvider() ?? null;
  const [row] = await db().select().from(directory_sync_state).where(eq(directory_sync_state.org_id, orgId)).limit(1);
  return {
    provider: provider
      ? {
          pluginName: provider.id,
          providerLabel: provider.capabilities.providerLabel,
          canCreateUsers: provider.capabilities.write.createUser,
          canDeactivateUsers: provider.capabilities.write.deactivateUser,
        }
      : null,
    state: {
      status: row?.status ?? "never",
      startedAt: row?.started_at?.toISOString() ?? null,
      finishedAt: row?.finished_at?.toISOString() ?? null,
      lastError: row?.last_error ?? null,
      stats: (row?.stats as Partial<DirectorySyncStats> | undefined) ?? {},
    },
  };
}

/** Reads every page from the directory provider, then reconciles in one transaction. */
export async function collectDirectorySnapshot(
  source: DirectorySource,
  orgId: string,
): Promise<DirectorySnapshot> {
  const provider = source.getDirectoryProvider();
  if (!provider) throw new DirectorySyncError("no_provider", "no directory provider is configured");
  const snapshot: DirectorySnapshot = {
    orgId,
    provider: provider.id,
    tenantId: "",
    createUsers: provider.capabilities.read.users,
    users: [],
    groups: [],
    memberships: [],
  };
  let cursor: string | null = null;
  for (let page = 0; page < MAX_PAGES; page++) {
    const result = await source.listDirectory(cursor);
    if (snapshot.tenantId && snapshot.tenantId !== result.tenantId) {
      throw new DirectorySyncError("invalid", "directory provider changed tenant between pages");
    }
    snapshot.tenantId = result.tenantId;
    snapshot.users.push(...result.users);
    snapshot.groups.push(...result.groups.map((g) => ({ externalId: g.externalId, displayName: g.name ?? null })));
    snapshot.memberships.push(...result.memberships);
    cursor = result.nextCursor ?? null;
    if (!cursor) return snapshot;
  }
  throw new DirectorySyncError("invalid", `directory provider returned more than ${MAX_PAGES} pages`);
}

export async function runDirectorySync(source: DirectorySource, orgId: string): Promise<DirectorySyncStats> {
  const provider = source.getDirectoryProvider();
  if (!provider) throw new DirectorySyncError("no_provider", "no directory provider is configured");
  const claimed = await db().execute(sql`
    insert into directory_sync_state (org_id, provider, status, started_at, finished_at, last_error, updated_at)
    values (${orgId}, ${provider.id}, 'running', now(), null, null, now())
    on conflict (org_id) do update set
      provider = excluded.provider, status = 'running', started_at = now(), finished_at = null, last_error = null, updated_at = now()
    where directory_sync_state.status <> 'running' or directory_sync_state.started_at < now() - interval '1 hour'
    returning org_id`);
  if ((claimed as unknown as { rows: unknown[] }).rows.length === 0) {
    throw new DirectorySyncError("running", "a directory sync is already running");
  }
  try {
    const snapshot = await collectDirectorySnapshot(source, orgId);
    const stats = await reconcileDirectorySnapshot(snapshot);
    await db()
      .update(directory_sync_state)
      .set({ status: "ok", finished_at: new Date(), stats, updated_at: new Date() })
      .where(eq(directory_sync_state.org_id, orgId));
    return stats;
  } catch (err) {
    await db()
      .update(directory_sync_state)
      .set({
        status: "failed",
        finished_at: new Date(),
        last_error: err instanceof Error ? err.message : String(err),
        updated_at: new Date(),
      })
      .where(eq(directory_sync_state.org_id, orgId));
    throw err;
  }
}

/** Creates the user in the identity provider. Sign-in links it to the OpenNeko user by email. */
export async function createDirectoryUser(
  source: DirectorySource,
  input: { email: string; name: string | null },
): Promise<ApplyDirectoryChangeResult> {
  const provider = source.getDirectoryProvider();
  if (!provider) throw new DirectorySyncError("no_provider", "no directory provider is configured");
  if (!provider.capabilities.write.createUser) {
    throw new DirectorySyncError("unsupported", `${provider.capabilities.providerLabel} does not create users`);
  }
  return source.applyDirectoryChange({ op: "create_user", email: input.email, name: input.name });
}
