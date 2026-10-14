import { describe, expect, it } from "vitest";
import { validateRecordIdentifier } from "../src/naming";
import {
  buildRecordDetailQuery,
  buildRecordChangeLogQuery,
  buildRecordAggregateQuery,
  buildRecordListQuery,
  buildRecordReferenceLabelQuery,
  buildRecordRecycleDetailQuery,
  buildRecordRecycleListQuery,
  buildRecordSchemaLogQuery,
  projectRecordRow,
  RecordReadPermissionError,
  RecordReadTargetError,
} from "../src/read/query";
import type { AppRegistrySnapshot } from "../src/types";

const id = validateRecordIdentifier;

function snapshot(): AppRegistrySnapshot {
  return {
    revision: "4",
    app: {
      orgId: "org-a",
      appId: "operations",
      label: "Field Operations",
      purpose: null,
      status: "active",
      navOrder: 0,
      registryRevision: "4",
    },
    objects: [
      {
        id: "object-work-order",
        orgId: "org-a",
        appId: "operations",
        apiName: id("work_order"),
        sourceApiName: null,
        label: "Work order",
        pluralLabel: "Work orders",
        tableSchema: id("public"),
        tableName: id("operations__work_order"),
        nameField: id("subject"),
        visibility: "owner",
        custom: true,
        archivedAt: null,
        recordCount: "12",
      },
    ],
    fields: [
      {
        id: "field-subject",
        orgId: "org-a",
        objectId: "object-work-order",
        apiName: id("subject"),
        sourceApiName: null,
        label: "Subject",
        kind: "text",
        columnName: id("subject"),
        required: true,
        readOnly: false,
        archivedAt: null,
        picklistValues: null,
        referenceTargets: null,
        length: 200,
        scale: null,
      },
      {
        id: "field-status",
        orgId: "org-a",
        objectId: "object-work-order",
        apiName: id("status"),
        sourceApiName: null,
        label: "Current status",
        kind: "picklist",
        columnName: id("status"),
        required: false,
        readOnly: false,
        archivedAt: null,
        picklistValues: [
          { value: "new", label: "New", semantic: "open" },
          { value: "closed", label: "Closed", semantic: ["closed"] },
        ],
        referenceTargets: null,
        length: null,
        scale: null,
      },
      {
        id: "field-due-at",
        orgId: "org-a",
        objectId: "object-work-order",
        apiName: id("due_at"),
        sourceApiName: null,
        label: "Due at",
        kind: "datetime",
        columnName: id("due_at"),
        required: false,
        readOnly: false,
        archivedAt: null,
        picklistValues: null,
        referenceTargets: null,
        length: null,
        scale: null,
      },
      {
        id: "field-old",
        orgId: "org-a",
        objectId: "object-work-order",
        apiName: id("old_status"),
        sourceApiName: null,
        label: "Old status",
        kind: "text",
        columnName: id("old_status"),
        required: false,
        readOnly: false,
        archivedAt: new Date(),
        picklistValues: null,
        referenceTargets: null,
        length: null,
        scale: null,
      },
    ],
    layouts: [
      {
        objectId: "object-work-order",
        kind: "list",
        definition: { columns: [{ field: "status" }, "subject", "missing"] },
      },
      {
        objectId: "object-work-order",
        kind: "detail",
        definition: { sections: [{ fields: ["subject", "status"] }] },
      },
    ],
    pages: [],
    permissions: [
      {
        appId: "operations",
        role: "admin",
        objectApiName: id("work_order"),
        canRead: true,
        canCreate: true,
        canUpdate: true,
        canDelete: true,
      },
      {
        appId: "operations",
        role: "member",
        objectApiName: id("work_order"),
        canRead: false,
        canCreate: false,
        canUpdate: false,
        canDelete: false,
      },
    ],
  };
}

describe("generated record read queries", () => {
  it("builds a bounded, layout-driven list query with bound filter values", () => {
    const built = buildRecordListQuery({
      snapshot: snapshot(),
      objectApiName: "work_order",
      role: "admin",
      userId: "admin-1",
      first: 25,
      after: "opaque-cursor",
      search: "50%_ready",
      sort: { field: "status", direction: "desc" },
      filters: [{ field: "status", operator: "in", value: ["new"] }],
      myRecords: true,
    });

    expect(built.view.columns.map((column) => column.apiName)).toEqual([
      "status",
      "subject",
      "owner_user_id",
    ]);
    expect(built.query).toContain(
      "rows: operations__work_order(first: $first, after: $rows_cursor",
    );
    expect(built.query).toContain("order_by: { data: { status: desc }, id: desc }");
    expect(built.query).toContain("{ id owner_user_id data }");
    expect(built.query).toContain("{ data: { subject: { ilike: $search } } }");
    expect(built.query).toContain("operations__work_order_cursor");
    expect(built.query).toContain("totals: operations__work_order");
    expect(built.query).toContain("count_id");
    expect(built.query).not.toContain("50%_ready");
    expect(built.variables).toEqual({
      first: 25,
      rows_cursor: "opaque-cursor",
      search: "%50\\%\\_ready%",
      viewer_id: "admin-1",
      filter_0_0: "new",
    });
  });

  it("builds detail fields from sections and includes generic substrate metadata", () => {
    const built = buildRecordDetailQuery({
      snapshot: snapshot(),
      objectApiName: "work_order",
      role: "admin",
      recordId: "wo-1",
    });
    expect(built.query).toContain("{ id owner_user_id nk_updated_at data }");
    expect(built.variables).toEqual({ record_id: "wo-1" });
  });

  it("builds a fixed, bound change-history query after detail authorization", () => {
    const built = buildRecordChangeLogQuery({
      snapshot: snapshot(),
      objectApiName: "work_order",
      role: "admin",
      recordId: "work-1' } secret {",
      first: 25,
    });
    expect(built.query).toContain("history: record_change_log(first: $first");
    expect(built.query).toContain("record_id: { eq: $record_id }");
    expect(built.query).not.toContain("work-1");
    expect(built.variables).toEqual({
      app_id: "operations",
      object_api_name: "work_order",
      record_id: "work-1' } secret {",
      first: 25,
    });
  });

  it("builds admin-only schema history without DDL fields", () => {
    const built = buildRecordSchemaLogQuery({
      snapshot: snapshot(),
      role: "admin",
      first: 50,
    });
    expect(built.query).toContain("history: app_schema_log(first: $first");
    expect(built.query).not.toContain(" ddl ");
    expect(built.variables).toEqual({ app_id: "operations", first: 50 });
    expect(() => buildRecordSchemaLogQuery({ snapshot: snapshot(), role: "member" }))
      .toThrow(RecordReadPermissionError);
  });

  it("builds permission-scoped count and sum metric queries", () => {
    const count = buildRecordAggregateQuery({
      snapshot: snapshot(),
      objectApiName: "work_order",
      role: "admin",
      aggregate: "count",
      filter: { field: "status", operator: "not_in", value: ["closed"] },
    });
    expect(count.query).toContain(
      "metric: operations__work_order(where: { and: [{ data: { status: { neq: $filter_0_0 } } }] }) { count_id }",
    );
    expect(count.variables).toEqual({ filter_0_0: "closed" });

    const current = snapshot();
    current.fields.push({
      id: "field-amount",
      orgId: "org-a",
      objectId: "object-work-order",
      apiName: id("amount"),
      sourceApiName: null,
      label: "Amount",
      kind: "currency",
      columnName: id("amount"),
      required: false,
      readOnly: false,
      archivedAt: null,
      picklistValues: null,
      referenceTargets: null,
      length: null,
      scale: 2,
    });
    const sum = buildRecordAggregateQuery({
      snapshot: current,
      objectApiName: "work_order",
      role: "admin",
      aggregate: "sum",
      field: "amount",
    });
    expect(sum.query).toContain(
      '{ total: sum(expr: { cast: { expr: { json: { col: data, path: [amount] } }, as: "numeric" } }) }',
    );
    expect(sum.valueField).toBe("total");
  });

  it("compiles nested semantic filters, relative dates, and picklist groups", () => {
    const built = buildRecordListQuery({
      snapshot: snapshot(),
      objectApiName: "work_order",
      role: "admin",
      userId: "admin-1",
      now: new Date("2026-08-03T12:00:00.000Z"),
      filter: {
        op: "and",
        clauses: [
          { field: "due_at", operator: "relative", value: { macro: "this_quarter" } },
          {
            op: "or",
            clauses: [
              { field: "status", operator: "is_open" },
              { field: "status", operator: "eq", value: "blocked" },
            ],
          },
        ],
      },
    });

    expect(built.query).toContain("and: [{ data: { due_at: { gte: $filter_0_0_start } } }")
    expect(built.query).toContain("or: [{ or: [{ data: { status: { eq: $filter_0_1_0_0 } } }] }")
    expect(built.query).not.toContain("blocked");
    expect(built.variables).toMatchObject({
      filter_0_0_start: "2026-07-01T00:00:00.000Z",
      filter_0_0_end: "2026-10-01T00:00:00.000Z",
      filter_0_1_0_0: "new",
      filter_0_1_1: "blocked",
    });
  });

  it("rejects semantic filters that do not match registry field metadata", () => {
    expect(() =>
      buildRecordListQuery({
        snapshot: snapshot(),
        objectApiName: "work_order",
        role: "admin",
        userId: "admin-1",
        filter: { field: "subject", operator: "relative", value: { macro: "last_n_days", days: 7 } },
      }),
    ).toThrow(/date field/);
    expect(() =>
      buildRecordListQuery({
        snapshot: snapshot(),
        objectApiName: "work_order",
        role: "admin",
        userId: "admin-1",
        filter: { field: "status", operator: "is_open" },
      }),
    ).not.toThrow();
  });

  it("builds fixed, bound recycle-bin list and detail queries", () => {
    const list = buildRecordRecycleListQuery({
      snapshot: snapshot(),
      objectApiName: "work_order",
      role: "admin",
      first: 20,
      after: "recycle-cursor",
      search: "camera%_",
    });
    expect(list.query).toContain("query RecordsRecycleList");
    expect(list.query).toContain("rows: recycle_record(first: $first, after: $rows_cursor");
    expect(list.query).toContain("order_by: { deleted_at: desc, record_id: asc }");
    expect(list.query).toContain("count: count_record_id");
    expect(list.query).not.toContain("camera%_");
    expect(list.query).not.toContain("subject");
    expect(list.variables).toEqual({
      app_id: "operations",
      object_api_name: "work_order",
      first: 20,
      rows_cursor: "recycle-cursor",
      search: "%camera\\%\\_%",
    });

    const detail = buildRecordRecycleDetailQuery({
      snapshot: snapshot(),
      objectApiName: "work_order",
      role: "admin",
      recordId: "wo-deleted",
    });
    expect(detail.query).toContain("query RecordsRecycleDetail");
    expect(detail.query).toContain("record_id: { eq: $record_id }");
    expect(detail.query).not.toContain("wo-deleted");
    expect(detail.variables).toEqual({
      app_id: "operations",
      object_api_name: "work_order",
      record_id: "wo-deleted",
    });
  });

  it("builds a bounded, policy-checked reference label query", () => {
    const current = snapshot();
    current.objects.push({
      id: "object-account",
      orgId: "org-a",
      appId: "operations",
      apiName: id("account"),
      sourceApiName: null,
      label: "Account",
      pluralLabel: "Accounts",
      tableSchema: id("public"),
      tableName: id("operations__account"),
      nameField: id("name"),
      visibility: "org",
      custom: false,
      archivedAt: null,
      recordCount: "2",
    });
    current.fields.push({
      id: "field-account-name",
      orgId: "org-a",
      objectId: "object-account",
      apiName: id("name"),
      sourceApiName: null,
      label: "Account name",
      kind: "text",
      columnName: id("name"),
      required: true,
      readOnly: false,
      archivedAt: null,
      picklistValues: null,
      referenceTargets: null,
      length: 200,
      scale: null,
    });
    current.permissions.push({
      appId: "operations",
      role: "admin",
      objectApiName: id("account"),
      canRead: true,
      canCreate: true,
      canUpdate: true,
      canDelete: true,
    });

    const built = buildRecordReferenceLabelQuery({
      snapshot: current,
      objectApiName: "account",
      role: "admin",
      recordIds: ["001-b", "001-a", "001-a"],
    });
    expect(built.query).toBe(
      "query RecordsReferenceLabels { rows: operations__account(where: { id: { in: $record_ids } }, limit: 2) { id data } }",
    );
    expect(built.variables).toEqual({ record_ids: ["001-b", "001-a"] });

    current.permissions = current.permissions.filter(
      (permission) => permission.objectApiName !== "account",
    );
    expect(() =>
      buildRecordReferenceLabelQuery({
        snapshot: current,
        objectApiName: "account",
        role: "admin",
        recordIds: ["001-a"],
      }),
    ).toThrow(RecordReadPermissionError);
  });

  it("treats an explicit stale layout as a fail-closed field allowlist", () => {
    const current = snapshot();
    current.layouts = [
      {
        objectId: "object-work-order",
        kind: "list",
        definition: { columns: ["missing", "old_status", "not valid"] },
      },
    ];
    const built = buildRecordListQuery({
      snapshot: current,
      objectApiName: "work_order",
      role: "admin",
      userId: "admin-1",
    });

    expect(built.view.columns.map((column) => column.apiName)).toEqual([
      "owner_user_id",
    ]);
    expect(built.query).toContain("{ id owner_user_id }");
    expect(built.query).not.toContain("old_status");
  });

  it("fails closed for unreadable roles, unknown identifiers, and unsafe budgets", () => {
    expect(() =>
      buildRecordListQuery({
        snapshot: snapshot(),
        objectApiName: "work_order",
        role: "member",
        userId: "member-1",
      }),
    ).toThrow(RecordReadPermissionError);
    expect(() =>
      buildRecordListQuery({
        snapshot: snapshot(),
        objectApiName: "work_order) { secret }",
        role: "admin",
        userId: "admin-1",
      }),
    ).toThrow(RecordReadTargetError);
    expect(() =>
      buildRecordListQuery({
        snapshot: snapshot(),
        objectApiName: "work_order",
        role: "admin",
        userId: "admin-1",
        first: 101,
      }),
    ).toThrow(/between 1 and 100/);
  });
});

describe("record document reads", () => {
  function withFields(...fields: Array<{ apiName: string; kind: AppRegistrySnapshot["fields"][number]["kind"] }>) {
    const current = snapshot();
    for (const field of fields) {
      current.fields.push({
        id: `field-${field.apiName}`,
        orgId: "org-a",
        objectId: "object-work-order",
        apiName: id(field.apiName),
        sourceApiName: null,
        label: field.apiName,
        kind: field.kind,
        columnName: id(field.apiName),
        required: false,
        readOnly: false,
        archivedAt: null,
        picklistValues: null,
        referenceTargets: null,
        length: null,
        scale: null,
      });
    }
    return current;
  }

  it("inlines number and boolean filter values so the key is cast", () => {
    const built = buildRecordListQuery({
      snapshot: withFields({ apiName: "hours", kind: "decimal" }, { apiName: "urgent", kind: "boolean" }),
      objectApiName: "work_order",
      role: "admin",
      userId: "user-1",
      filter: {
        op: "and",
        clauses: [
          { field: "hours", operator: "gt", value: 8 },
          { field: "hours", operator: "in", value: [1, 2.5] },
          { field: "urgent", operator: "eq", value: true },
        ],
      },
    });
    expect(built.query).toContain("{ data: { hours: { gt: 8 } } }");
    expect(built.query).toContain("{ data: { hours: { in: [1, 2.5] } } }");
    expect(built.query).toContain("{ data: { urgent: { eq: true } } }");
    expect(built.variables).not.toHaveProperty("filter_0_0");
  });

  it("binds every text list value instead of writing it into the query", () => {
    const built = buildRecordListQuery({
      snapshot: snapshot(),
      objectApiName: "work_order",
      role: "admin",
      userId: "user-1",
      filter: { field: "status", operator: "in", value: ["it's", 'x" } }) { id } }'] },
    });
    expect(built.query).toContain(
      "or: [{ data: { status: { eq: $filter_0_0 } } }, { data: { status: { eq: $filter_0_1 } } }]",
    );
    expect(built.query).not.toContain("it's");
    expect(built.variables).toMatchObject({ filter_0_0: "it's", filter_0_1: 'x" } }) { id } }' });
  });

  it("rejects a number filter with a non-numeric value", () => {
    expect(() =>
      buildRecordListQuery({
        snapshot: withFields({ apiName: "hours", kind: "integer" }),
        objectApiName: "work_order",
        role: "admin",
        userId: "user-1",
        filter: { field: "hours", operator: "gt", value: "8 OR 1=1" },
      }),
    ).toThrow(RecordReadTargetError);
  });

  it("compares date fields as YYYY-MM-DD text", () => {
    const built = buildRecordListQuery({
      snapshot: withFields({ apiName: "due_on", kind: "date" }),
      objectApiName: "work_order",
      role: "admin",
      userId: "user-1",
      now: new Date("2026-10-14T09:00:00Z"),
      filter: { field: "due_on", operator: "relative", value: { macro: "this_quarter" } },
    });
    expect(built.variables).toMatchObject({ filter_0_start: "2026-10-01", filter_0_end: "2027-01-01" });
  });

  it("flattens the document into the view and drops keys outside it", () => {
    const built = buildRecordDetailQuery({
      snapshot: snapshot(),
      objectApiName: "work_order",
      role: "admin",
      recordId: "wo-1",
    });
    const row = projectRecordRow(built.view, {
      id: "wo-1",
      owner_user_id: "user-1",
      nk_updated_at: "2026-10-14T09:00:00Z",
      data: { subject: "Pump check", status: "open", internal_cost: 900 },
    });
    expect(row).toMatchObject({ id: "wo-1", subject: "Pump check", owner_user_id: "user-1" });
    expect(row).not.toHaveProperty("internal_cost");
  });
});
