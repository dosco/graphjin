import pg from "pg";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import {
  buildRecordsPoolConfig,
  runRecordsMigrations,
} from "@neko/db/records-migrate";
import {
  decideIdentityMapping,
  linkIdentityMappingsForUser,
  listIdentityMappings,
  listLinkedIdentityScopesForUser,
  readImportedSourceUsers,
  reconcileImportedIdentities,
} from "../src/identity/map";

async function recordsDbReachable(): Promise<boolean> {
  const probe = new pg.Pool(buildRecordsPoolConfig());
  try {
    await probe.query("select 1");
    return true;
  } catch {
    return false;
  } finally {
    await probe.end();
  }
}

const reachable = await recordsDbReachable();
const describeIfLive = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[records-identity-map] skipping: records Postgres unavailable.");
}

describeIfLive("source-scoped records identity mapping", () => {
  const database = `records_identity_${Date.now().toString(36)}_${Math.random()
    .toString(36)
    .slice(2, 7)}`;
  let adminPool: pg.Pool;
  let testPool: pg.Pool;

  beforeAll(async () => {
    adminPool = new pg.Pool(buildRecordsPoolConfig());
    await adminPool.query(`create database ${database}`);
    testPool = new pg.Pool(buildRecordsPoolConfig(process.env, { database }));
    await runRecordsMigrations({ pool: testPool });
    await testPool.query(`
      insert into engine.record_app (org_id, app_id, label, status)
      values
        ('org-a', 'crm', 'CRM', 'active'),
        ('org-a', 'support', 'Support', 'active');
      insert into engine.registry_version (org_id, revision)
      values ('org-a', 1);
      insert into engine.record_object
        (id, org_id, app_id, api_name, source_api_name, label, plural_label,
         table_schema, table_name, name_field, visibility)
      values
        ('00000000-0000-4000-a000-000000000891', 'org-a', 'crm', 'user',
         'User', 'User', 'Users', 'public', 'crm__user', 'name', 'org');
      insert into engine.record_field
        (id, org_id, object_id, api_name, source_api_name, label, kind, column_name)
      values
        ('00000000-0000-4000-a000-000000000892', 'org-a',
         '00000000-0000-4000-a000-000000000891', 'email', 'Email', 'Email', 'email', 'email'),
        ('00000000-0000-4000-a000-000000000893', 'org-a',
         '00000000-0000-4000-a000-000000000891', 'name', 'Name', 'Name', 'text', 'name'),
        ('00000000-0000-4000-a000-000000000894', 'org-a',
         '00000000-0000-4000-a000-000000000891', 'is_active', 'IsActive', 'Active', 'boolean', 'is_active');
    `);
  });

  afterAll(async () => {
    if (testPool) await testPool.end();
    if (adminPool) {
      await adminPool.query(`drop database if exists ${database}`);
      await adminPool.end();
    }
  });

  it("links unique emails and reports shared-mailbox collisions explicitly", async () => {
    const result = await reconcileImportedIdentities(testPool, {
      orgId: "org-a",
      sourceInstanceId: "sf-prod",
      appId: "crm",
      appUsers: [
        { id: "user-alice", email: "alice@example.com" },
        { id: "user-shared", email: "shared@example.com" },
        { id: "user-disabled", email: "disabled@example.com", disabled: true },
      ],
      sourceUsers: [
        { sourceUserId: "005-alice", email: "ALICE@example.com", name: "Alice" },
        { sourceUserId: "005-shared-a", email: "shared@example.com" },
        { sourceUserId: "005-shared-b", email: "shared@example.com" },
        { sourceUserId: "005-missing", email: "missing@example.com" },
        { sourceUserId: "005-disabled", email: "disabled@example.com" },
      ],
    });

    expect(result).toMatchObject({ linked: 1, conflicts: 2, unlinked: 2, ignored: 0 });
    expect(result.mappings.find((mapping) => mapping.sourceUserId === "005-alice")).toMatchObject({
      sourceEmail: "alice@example.com",
      appUserId: "user-alice",
      status: "linked",
    });
    expect(
      result.mappings.filter((mapping) => mapping.sourceUserId.startsWith("005-shared")),
    ).toEqual([
      expect.objectContaining({ status: "conflict", appUserId: null }),
      expect.objectContaining({ status: "conflict", appUserId: null }),
    ]);
  });

  it("reads live imported Salesforce users through validated registry columns", async () => {
    const execute = vi.fn().mockResolvedValue({
      rows: [
        {
          id: "005000000000001AAA",
          data: { email: "Alice@example.com", name: "Alice", is_active: true },
        },
        {
          id: "005000000000002AAA",
          data: { email: "not-an-email", name: "Invalid", is_active: true },
        },
      ],
    });
    await expect(
      readImportedSourceUsers(testPool, {
        orgId: "org-a",
        appId: "crm",
        graphjin: { execute },
        token: "service-token",
      }),
    ).resolves.toEqual({
      found: true,
      users: [
        {
          sourceUserId: "005000000000001AAA",
          email: "Alice@example.com",
          name: "Alice",
          active: true,
        },
      ],
      invalidEmails: 1,
      invalidEmailSourceUserIds: ["005000000000002AAA"],
    });
    expect(execute).toHaveBeenCalledWith(
      expect.objectContaining({
        operationName: "RecordsImportedSourceUsers",
        query: expect.stringContaining("rows: crm__user"),
        variables: { org_id: "org-a" },
        token: "service-token",
      }),
    );
  });

  it("keeps identical external ids independent across connector instances", async () => {
    const result = await reconcileImportedIdentities(testPool, {
      orgId: "org-a",
      sourceInstanceId: "sf-sandbox",
      appId: "crm",
      appUsers: [{ id: "user-alice", email: "alice@example.com" }],
      sourceUsers: [{ sourceUserId: "005-alice", email: "alice@example.com" }],
    });
    expect(result.mappings).toEqual([
      expect.objectContaining({
        sourceInstanceId: "sf-sandbox",
        sourceUserId: "005-alice",
        status: "linked",
      }),
    ]);
    const all = await listIdentityMappings(testPool, { orgId: "org-a", appId: "crm" });
    expect(all.filter((mapping) => mapping.sourceUserId === "005-alice")).toHaveLength(2);
    await expect(
      listLinkedIdentityScopesForUser(testPool, {
        orgId: "org-a",
        appUserId: "user-alice",
      }),
    ).resolves.toEqual([
      { appId: "crm", sourceInstanceId: "sf-prod", sourceUserId: "005-alice" },
      { appId: "crm", sourceInstanceId: "sf-sandbox", sourceUserId: "005-alice" },
    ]);
  });

  it("preserves ignore decisions, supports manual links, and prevents double binding", async () => {
    await decideIdentityMapping(testPool, {
      orgId: "org-a",
      sourceInstanceId: "sf-prod",
      appId: "crm",
      sourceUserId: "005-missing",
      decision: "ignore",
    });
    const rerun = await reconcileImportedIdentities(testPool, {
      orgId: "org-a",
      sourceInstanceId: "sf-prod",
      appId: "crm",
      appUsers: [{ id: "user-missing", email: "missing@example.com" }],
      sourceUsers: [{ sourceUserId: "005-missing", email: "missing@example.com" }],
    });
    expect(rerun.mappings[0]).toMatchObject({ status: "ignored", appUserId: null });

    await expect(
      decideIdentityMapping(testPool, {
        orgId: "org-a",
        sourceInstanceId: "sf-prod",
        appId: "crm",
        sourceUserId: "005-disabled",
        decision: "link",
        appUserId: "user-manual",
      }),
    ).resolves.toMatchObject({ status: "linked", appUserId: "user-manual" });
    await expect(
      decideIdentityMapping(testPool, {
        orgId: "org-a",
        sourceInstanceId: "sf-prod",
        appId: "crm",
        sourceUserId: "005-shared-a",
        decision: "link",
        appUserId: "user-manual",
      }),
    ).rejects.toThrow(/already linked/);
  });

  it("lazy-links unambiguous SSO emails and marks ambiguous rows as conflicts", async () => {
    await reconcileImportedIdentities(testPool, {
      orgId: "org-a",
      sourceInstanceId: "sf-prod",
      appId: "support",
      appUsers: [],
      sourceUsers: [
        { sourceUserId: "new-one", email: "new@example.com" },
        { sourceUserId: "dupe-one", email: "dupe@example.com" },
        { sourceUserId: "dupe-two", email: "dupe@example.com" },
      ],
    });
    await expect(
      linkIdentityMappingsForUser(testPool, {
        orgId: "org-a",
        appUserId: "user-new",
        email: "NEW@example.com",
      }),
    ).resolves.toEqual({ linked: 1, conflicts: 0 });
    await expect(
      linkIdentityMappingsForUser(testPool, {
        orgId: "org-a",
        appUserId: "user-dupe",
        email: "dupe@example.com",
      }),
    ).resolves.toEqual({ linked: 0, conflicts: 2 });
  });
});
