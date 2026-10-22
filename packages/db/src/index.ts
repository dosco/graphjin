/**
 * Metadata DB client.
 * Drizzle ORM bound to the Neko metadata Postgres.
 */

import { drizzle, type NodePgDatabase } from "drizzle-orm/node-postgres";
import { createHash } from "node:crypto";
import pg from "pg";
import { buildPoolConfig } from "./connection";
import { guardPoolErrors } from "./pool-errors";
import * as schema from "./schema";

export { schema };
export * from "./schema";
export { buildPoolConfig } from "./connection";
export {
  buildRecordsPoolConfig,
  defaultRecordsMigrationsDir,
  discoverRecordsMigrations,
  runRecordsMigrations,
  type RecordsMigration,
  type RecordsMigrationResult,
} from "./records-migrate";
export { createNotifyClient, type NotifyClient } from "./notify";
export {
  guardPoolErrors,
  isDatabaseUnavailable,
  keepProcessThroughDatabaseRestarts,
  retryWhileDatabaseUnavailable,
} from "./pool-errors";
export { getOrCreateSoloAdmin, isUnclaimedSoloEmail, soloAdminNeedsEmail } from "./solo-admin";
export { getOrgId, _resetOrgIdCacheForTesting } from "./org";
export {
  GroupError,
  activeAdministratorIds,
  addLocalGroupMember,
  administratorUserIds,
  assertAdministratorsKept,
  builtinGroupId,
  createIdpGroupRule,
  createUserGroup,
  deleteIdpGroupRule,
  deleteUserGroup,
  getUserGroup,
  listGroupMembers,
  listIdpGroupRules,
  listIdpGroups,
  listUserGroups,
  reconcileDirectorySnapshot,
  reconcileSignInGroups,
  recomputeRuleMemberships,
  removeLocalGroupMember,
  resolveUserGroups,
  setLocalAdministrator,
  slugifyGroupName,
  updateUserGroup,
  withAdministratorGuard,
  type DirectorySnapshot,
  type DirectorySyncStats,
  type GroupErrorCode,
  type GroupMemberRow,
  type IdpGroupInput,
  type IdpGroupRow,
  type IdpGroupRuleRow,
  type ResolvedUserGroups,
  type UserGroupRow,
} from "./groups";
export {
  ITEM_TYPES,
  _resetEntitlementCacheForTesting,
  effectiveAccess,
  filterHeld,
  grantItem,
  grantItems,
  groupHolds,
  heldItems,
  holds,
  isItemType,
  listGroupItemGrants,
  packItemIds,
  packsContaining,
  revokeItem,
  whoHolds,
  type EffectiveItem,
  type EntitlementActor,
  type GroupItemGrant,
  type HeldItems,
  type HoldResult,
  type ItemGrantInput,
  type ItemHolder,
  type ItemRef,
  type ItemType,
} from "./entitlements";
export {
  deleteDataAccessRule,
  getGroupGrantsEnabled,
  getPreviousReadModes,
  recordPreviousReadModes,
  seedEveryoneDataAccess,
  graphjinGroupClaims,
  listDataAccessRules,
  loadGroupGrantInputs,
  setGroupGrantsEnabled,
  upsertDataAccessRule,
  type DataAccessRuleRow,
} from "./data-access";
export {
  readLocalConfig,
  writeLocalConfig,
  hasCustomPassword,
  localConfigPath,
  type LocalConfig,
  type LocalPgConfig,
} from "./local-config";
export {
  DEFAULT_INSTALL_POLICY,
  INSTALL_POLICY_SCOPE,
  getInstallPolicyForOrg,
  policyFromConfig,
  type InstallPolicy,
} from "./install-policy";
export {
  DEFAULT_GRAPHJIN_CONFIG_SETTINGS,
  GRAPHJIN_CONFIG_SCOPE,
  getGraphjinConfigSettingsForOrg,
  graphjinConfigSettingsFromConfig,
  type GraphjinConfigSettings,
} from "./graphjin-config";
export {
  GRAPHJIN_AGENT_REUSE_PRIMARY,
  GRAPHJIN_AGENT_SCOPE,
  getGraphjinAgentRow,
  graphjinAgentEnabled,
  graphjinAgentEnabledForOrg,
  type GraphjinAgentRow,
} from "./graphjin-agent";
export {
  and,
  asc,
  desc,
  eq,
  gt,
  gte,
  inArray,
  isNull,
  isNotNull,
  like,
  lt,
  lte,
  ne,
  notInArray,
  or,
  sql,
} from "drizzle-orm";

let _pool: pg.Pool | null = null;
let _db: NodePgDatabase<typeof schema> | null = null;
let _poolConfigSignature: string | null = null;

function configSignature(config: pg.PoolConfig): string {
  return createHash("sha256")
    .update(JSON.stringify(config, (_key, value) => (typeof value === "function" ? "[function]" : value)))
    .digest("hex");
}

// Self-heal after .end() — vitest singleFork shares this singleton across
// every test file in a workspace; once one file's afterAll calls
// `pool().end()`, every later file gets a dead pool unless we recreate.
function isPoolUsable(p: pg.Pool | null): p is pg.Pool {
  if (!p) return false;
  const flags = p as pg.Pool & { ended?: boolean; ending?: boolean };
  return !flags.ended && !flags.ending;
}

export function pool(): pg.Pool {
  const config = buildPoolConfig();
  const signature = configSignature(config);
  if (isPoolUsable(_pool) && _poolConfigSignature === signature) return _pool;

  if (_pool) {
    void _pool.end().catch(() => {
      // Pool already closing or closed — fine.
    });
  }

  _pool = guardPoolErrors(new pg.Pool(config), "db");
  _poolConfigSignature = signature;
  _db = null;
  return _pool;
}

export function db(): NodePgDatabase<typeof schema> {
  // Re-resolve through pool() so a closed pool gets rebuilt + the drizzle
  // wrapper rebound. Otherwise _db could hold a reference to a dead pool.
  const live = pool();
  if (_db && (_db as { client?: unknown }).client === live) return _db;
  _db = drizzle(live, { schema });
  return _db;
}

/**
 * Drain and reset the connection pool so the next `db()` / `pool()` call
 * builds a fresh one against the current config (e.g. after the admin
 * changes the DB password via /setup).
 *
 * Safe to await even if the pool was never created.
 */
export async function reconnectPool(): Promise<void> {
  if (_pool) {
    try {
      await _pool.end();
    } catch {
      // Pool already closing or closed — fine.
    }
  }
  _pool = null;
  _db = null;
  _poolConfigSignature = null;
}
