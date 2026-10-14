import { describe, expect, it } from "vitest";
import {
  RECORD_GRAPHJIN_ROLES,
  projectRecordsGraphjinRoles,
} from "../src/policy/graphjin";
import { recordsPolicyFixture } from "./policy-fixture";

function tableFor(role: ReturnType<typeof projectRecordsGraphjinRoles>[number], name: string) {
  const table = role.tables.find((candidate) => candidate.name === name);
  if (!table) throw new Error(`missing fixture table ${name}`);
  return table;
}

describe("projectRecordsGraphjinRoles", () => {
  it("covers every live relation, role, and operation without omissions", () => {
    const model = recordsPolicyFixture();
    const roles = projectRecordsGraphjinRoles(model);

    expect(roles.map((role) => role.name)).toEqual(RECORD_GRAPHJIN_ROLES);
    for (const role of roles) {
      expect(role.tables).toHaveLength(model.catalog.length);
      for (const table of role.tables) {
        expect(Object.keys(table)).toEqual(
          expect.arrayContaining(["query", "insert", "update", "upsert", "delete"]),
        );
        for (const operation of ["query", "insert", "update", "upsert", "delete"] as const) {
          expect(typeof table[operation].block).toBe("boolean");
          if (!table[operation].block) {
            expect(table[operation].columns?.length).toBeGreaterThan(0);
          }
        }
      }
    }
  });

  it("fully blocks fallback, anonymous, engine, orphaned, and archived access", () => {
    const roles = projectRecordsGraphjinRoles(recordsPolicyFixture());
    for (const roleName of ["anon", "user"] as const) {
      const role = roles.find((candidate) => candidate.name === roleName)!;
      for (const table of role.tables) {
        expect(Object.values(table).filter((value) => typeof value === "object")).toEqual(
          expect.arrayContaining([
            expect.objectContaining({ block: true }),
          ]),
        );
        expect(table.query.block).toBe(true);
        expect(table.insert.block).toBe(true);
        expect(table.update.block).toBe(true);
        expect(table.upsert.block).toBe(true);
        expect(table.delete.block).toBe(true);
      }
    }

    for (const role of roles) {
      for (const name of ["actor", "orphan_table", "crm__legacy"]) {
        const table = tableFor(role, name);
        expect(table.query.block).toBe(true);
        expect(table.insert.block).toBe(true);
        expect(table.update.block).toBe(true);
        expect(table.upsert.block).toBe(true);
        expect(table.delete.block).toBe(true);
      }
    }
  });

  it("exposes only the narrow recycle index to readable human scopes", () => {
    const roles = projectRecordsGraphjinRoles(recordsPolicyFixture());
    const member = tableFor(
      roles.find((role) => role.name === "member")!,
      "recycle_record",
    );
    const admin = tableFor(
      roles.find((role) => role.name === "admin")!,
      "recycle_record",
    );

    expect(member.query).toMatchObject({
      block: false,
      columns: [
        "app_id",
        "object_api_name",
        "record_id",
        "record_name",
        "owner_user_id",
        "deleted_at",
        "deleted_by",
      ],
      filters: [
        '{ org_id: { eq: "org-a" } }',
        '{ scope_key: { in: ["crm.account", "crm.contact"] } }',
        '{ or: { visibility: { eq: "org" }, owner_user_id: { eq: $user_id } } }',
      ],
    });
    expect(admin.query.block).toBe(false);
    expect(admin.query.filters).toEqual([
      '{ org_id: { eq: "org-a" } }',
      '{ scope_key: { in: ["crm.account", "crm.contact"] } }',
    ]);
    expect(member.query.columns).not.toContain("org_id");
    expect(member.query.columns).not.toContain("deletion_action_request_id");

    for (const roleName of ["anon", "user", "service"] as const) {
      expect(
        tableFor(
          roles.find((role) => role.name === roleName)!,
          "recycle_record",
        ).query.block,
      ).toBe(true);
    }
    for (const role of roles) {
      const recycle = tableFor(role, "recycle_record");
      expect(recycle.insert.block).toBe(true);
      expect(recycle.update.block).toBe(true);
      expect(recycle.upsert.block).toBe(true);
      expect(recycle.delete.block).toBe(true);
    }
  });

  it("exposes identity mappings to admins only and keeps every mutation closed", () => {
    const roles = projectRecordsGraphjinRoles(recordsPolicyFixture());
    const adminIdentity = tableFor(
      roles.find((role) => role.name === "admin")!,
      "identity_map",
    );

    expect(adminIdentity.query).toMatchObject({
      block: false,
      filters: ['{ org_id: { eq: "org-a" } }'],
      columns: [
        "source_instance_id",
        "app_id",
        "source_user_id",
        "source_email",
        "source_name",
        "source_is_active",
        "app_user_id",
        "status",
        "linked_at",
        "updated_at",
      ],
    });
    expect(adminIdentity.query.columns).not.toContain("org_id");
    expect(adminIdentity.query.columns).not.toContain("source_email_normalized");

    for (const roleName of ["anon", "user", "member", "service"] as const) {
      expect(
        tableFor(
          roles.find((role) => role.name === roleName)!,
          "identity_map",
        ).query.block,
      ).toBe(true);
    }
    for (const role of roles) {
      const identity = tableFor(role, "identity_map");
      expect(identity.insert.block).toBe(true);
      expect(identity.update.block).toBe(true);
      expect(identity.upsert.block).toBe(true);
      expect(identity.delete.block).toBe(true);
    }
  });

  it("gives humans column-limited reads and blocks every mutation", () => {
    const roles = projectRecordsGraphjinRoles(recordsPolicyFixture());
    const member = roles.find((role) => role.name === "member")!;
    const admin = roles.find((role) => role.name === "admin")!;
    const memberAccount = tableFor(member, "crm__account");
    const memberContact = tableFor(member, "crm__contact");
    const adminContact = tableFor(admin, "crm__contact");

    expect(memberAccount.query).toMatchObject({
      block: false,
      columns: expect.arrayContaining(["id", "data", "nk_created_at"]),
      filters: expect.arrayContaining([
        '{ org_id: { eq: "org-a" } }',
        "{ nk_deleted_at: { is_null: true } }",
      ]),
    });
    expect(memberAccount.query.columns).not.toContain("industry");
    expect(memberAccount.query.columns).not.toContain("org_id");
    expect(memberAccount.query.columns).not.toContain("nk_action_request_id");

    expect(memberContact.query.filters).toContain(
      "{ owner_user_id: { eq: $user_id } }",
    );
    expect(adminContact.query.filters).not.toContain(
      "{ owner_user_id: { eq: $user_id } }",
    );

    for (const role of [member, admin]) {
      for (const table of role.tables) {
        expect(table.insert.block).toBe(true);
        expect(table.update.block).toBe(true);
        expect(table.upsert.block).toBe(true);
        expect(table.delete.block).toBe(true);
      }
    }
  });

  it("scopes change history by readable object and member ownership", () => {
    const roles = projectRecordsGraphjinRoles(recordsPolicyFixture());
    const member = tableFor(roles.find((role) => role.name === "member")!, "record_change_log");
    const admin = tableFor(roles.find((role) => role.name === "admin")!, "record_change_log");
    expect(member.query).toMatchObject({
      block: false,
      columns: expect.arrayContaining(["record_id", "changes", "at"]),
      disable_functions: true,
    });
    expect(member.query.filters?.join(" ")).toContain(
      "owner_user_id: { eq: $user_id }",
    );
    expect(admin.query.filters?.join(" ")).not.toContain("owner_user_id");
    expect(member.query.columns).not.toContain("owner_user_id");
    expect(member.query.columns).not.toContain("mutation_id");
    for (const role of roles) {
      const table = tableFor(role, "record_change_log");
      expect(table.insert.block).toBe(true);
      expect(table.update.block).toBe(true);
      expect(table.delete.block).toBe(true);
    }
  });

  it("does not wrap a single readable change-history scope in a redundant or", () => {
    const model = recordsPolicyFixture();
    model.objects = model.objects.filter((object) => object.apiName !== "contact");
    model.permissions = model.permissions.filter(
      (permission) => permission.objectApiName !== "contact",
    );

    const roles = projectRecordsGraphjinRoles(model);
    const member = tableFor(
      roles.find((role) => role.name === "member")!,
      "record_change_log",
    );
    expect(member.query.filters).toEqual([
      '{ org_id: { eq: "org-a" } }',
      '{ and: [{ app_id: { eq: "crm" } }, { object_api_name: { eq: "account" } }] }',
    ]);
  });

  it("exposes bounded schema history only to admins without DDL", () => {
    const roles = projectRecordsGraphjinRoles(recordsPolicyFixture());
    const admin = tableFor(roles.find((role) => role.name === "admin")!, "app_schema_log");
    expect(admin.query).toMatchObject({
      block: false,
      columns: expect.arrayContaining(["action", "detail", "at"]),
      disable_functions: true,
    });
    expect(admin.query.columns).not.toContain("ddl");
    expect(admin.query.columns).not.toContain("org_id");
    for (const roleName of ["anon", "user", "member", "service"] as const) {
      expect(tableFor(roles.find((role) => role.name === roleName)!, "app_schema_log").query.block).toBe(true);
    }
  });

  it("makes service the only mutation role while retaining org and deletion scopes", () => {
    const service = projectRecordsGraphjinRoles(recordsPolicyFixture()).find(
      (role) => role.name === "service",
    )!;
    const account = tableFor(service, "crm__account");

    expect(account.query).toMatchObject({
      block: false,
      filters: [
        '{ org_id: { eq: "org-a" } }',
        "{ nk_deleted_at: { is_null: true } }",
      ],
    });
    expect(account.insert).toMatchObject({
      block: false,
      presets: {
        org_id: "org-a",
        nk_created_at: "now",
        nk_updated_at: "now",
      },
    });
    expect(account.insert.columns).not.toContain("org_id");
    expect(account.update.filters).toEqual(['{ org_id: { eq: "org-a" } }']);
    expect(account.update.columns).toContain("id");
    expect(account.update.columns).toContain("org_id");
    expect(account.update.presets).toMatchObject({ org_id: "org-a" });
    expect(account.update.columns).toContain("nk_deleted_at");
    expect(account.upsert.block).toBe(true);
    expect(account.delete.block).toBe(true);
  });
});
