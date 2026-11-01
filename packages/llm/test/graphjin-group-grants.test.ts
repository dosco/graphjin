import { describe, expect, it } from "vitest";
import { parse } from "yaml";
import { applyGroupGrantsToConfig, buildGroupGrantsModel, groupRoleName, listConfigApiOperations, readDatabaseSourceReadModes, removeGroupGrantsFromConfig } from "../src/graphjin/group-grants";

const CONFIG = `
identity:
  role_claims: [role]
  admin_roles: [admin]
roles:
  - name: member
  - name: og_stale
sources:
  - name: shop
    kind: database
    read_only: true
    access:
      read: account
      grants:
        - role: packrole
          tables:
            - name: products
              columns: [id]
        - role: og_stale
          tables:
            - name: orders
              columns: [id]
  - name: payments
    kind: api
    specs:
      stripe:
        operations:
          refund:
            expose_mutation: true
            allowed_roles: [admin, og_stale]
          list_charges:
            expose_mutation: false
`;

const MODES = { shop: "account" };

describe("group grants config", () => {
  const model = buildGroupGrantsModel({
    groups: [{ slug: "finance", name: "Finance" }, { slug: "sales", name: "Sales" }, { slug: "everyone", name: "Everyone" }],
    rules: [
      { groupSlug: "finance", groupName: "Finance", source: "shop", tableSchema: "public", tableName: "orders", columns: ["id", "amount"], rowFilter: { column: "region", op: "eq", value: "emea" } },
      { groupSlug: "sales", groupName: "Sales", source: "shop", tableSchema: "", tableName: "orders", columns: ["id"], rowFilter: null },
    ],
    apiOperationHolders: new Map([["payments:stripe:refund", ["finance"]]]),
  });

  it("writes group roles, grants, union identity, deny by default and API roles", () => {
    const { content, changed } = applyGroupGrantsToConfig(CONFIG, model, MODES);
    expect(changed).toBe(true);
    const config = parse(content);
    expect(config.identity).toMatchObject({ role_mode: "union", role_claims: ["role", "roles"], group_claims: ["groups"], admin_roles: ["admin"] });
    expect(config.roles.map((r: { name: string }) => r.name)).toEqual(["member", "og_finance", "og_sales"]);
    expect(config.sources[0].access.read).toBe("admin");
    expect(config.sources[0].access.grants).toEqual([
      { role: "packrole", tables: [{ name: "products", columns: ["id"] }] },
      { role: "og_finance", tables: [{ name: "public.orders", columns: ["id", "amount"], filter: '{ and: [{ account_id: { eq: $account_id } }, { region: { eq: "emea" } }] }' }] },
      { role: "og_sales", tables: [{ name: "orders", columns: ["id"], filter: "{ account_id: { eq: $account_id } }" }] },
    ]);
    expect(config.sources[1].access).toBeUndefined();
    expect(config.sources[1].specs.stripe.operations.refund.allowed_roles).toEqual(["admin", "og_finance"]);
    expect(config.sources[1].specs.stripe.operations.list_charges.allowed_roles).toBeUndefined();
  });

  it("is idempotent and removes grants when groups lose their rules", () => {
    const once = applyGroupGrantsToConfig(CONFIG, model, MODES).content;
    expect(applyGroupGrantsToConfig(once, model, MODES)).toEqual({ content: once, changed: false });
    const empty = parse(applyGroupGrantsToConfig(once, buildGroupGrantsModel({ groups: [], rules: [], apiOperationHolders: new Map() }), MODES).content);
    expect(empty.roles.map((r: { name: string }) => r.name)).toEqual(["member"]);
    expect(empty.sources[0].access.grants).toEqual([{ role: "packrole", tables: [{ name: "products", columns: ["id"] }] }]);
    expect(empty.sources[1].specs.stripe.operations.refund.allowed_roles).toEqual(["admin"]);
  });

  it("keeps the previous owner scope after read turns to admin", () => {
    const owner = CONFIG.replace("read: account", "read: owner\n      owner_column: created_by");
    const once = applyGroupGrantsToConfig(owner, model, { shop: "owner" }).content;
    const again = parse(applyGroupGrantsToConfig(once, model, { shop: "owner" }).content);
    expect(again.sources[0].access.grants[2]).toEqual({ role: "og_sales", tables: [{ name: "orders", columns: ["id"], filter: "{ created_by: { eq: $user_id } }" }] });
    const open = parse(applyGroupGrantsToConfig(once, model, { shop: "authenticated" }).content);
    expect(open.sources[0].access.grants[2]).toEqual({ role: "og_sales", tables: [{ name: "orders", columns: ["id"] }] });
  });

  it("leaves legacy configs without sources alone and lists API operations", () => {
    expect(applyGroupGrantsToConfig("database:\n  type: postgres\n", model, MODES)).toEqual({ content: "database:\n  type: postgres\n", changed: false });
    expect(listConfigApiOperations(CONFIG)).toEqual(["payments:stripe:list_charges", "payments:stripe:refund"]);
    expect(groupRoleName("finance-emea")).toBe("og_finance-emea");
  });

  it("restores the previous read modes and first role mode when turned off", () => {
    const modes = readDatabaseSourceReadModes(CONFIG);
    expect(modes).toEqual({ shop: "account" });
    const on = applyGroupGrantsToConfig(CONFIG, model, MODES).content;
    const off = parse(removeGroupGrantsFromConfig(on, modes).content);
    expect(off.identity.role_mode).toBe("first");
    expect(off.identity.group_claims).toBeUndefined();
    expect(off.sources[0].access).toEqual({ read: "account", grants: [{ role: "packrole", tables: [{ name: "products", columns: ["id"] }] }] });
    expect(off.roles.map((r: { name: string }) => r.name)).toEqual(["member"]);
    expect(off.sources[1].specs.stripe.operations.refund.allowed_roles).toEqual(["admin"]);
  });

  it("rejects an invalid stored row filter", () => {
    expect(() =>
      buildGroupGrantsModel({
        groups: [],
        rules: [{ groupSlug: "x", groupName: "X", source: "shop", tableSchema: "", tableName: "t", columns: ["id"], rowFilter: { column: "id", op: "like", value: 1 } }],
        apiOperationHolders: new Map(),
      }),
    ).toThrow("op must be one of");
  });
});
