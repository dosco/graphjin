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
    expect(built.query).toContain("order_by: { status: desc, id: desc }");
    expect(built.query).toContain("operations__work_order_cursor");
    expect(built.query).toContain("totals: operations__work_order");
    expect(built.query).toContain("count_id");
    expect(built.query).not.toContain("50%_ready");
    expect(built.variables).toEqual({
      first: 25,
      rows_cursor: "opaque-cursor",
      search: "%50\\%\\_ready%",
      viewer_id: "admin-1",
      filter_0: ["new"],
    });
  });

  it("builds detail fields from sections and includes generic substrate metadata", () => {
    const built = buildRecordDetailQuery({
      snapshot: snapshot(),
      objectApiName: "work_order",
      role: "admin",
      recordId: "wo-1",
    });
    expect(built.query).toContain("id subject status owner_user_id nk_updated_at");
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
      "metric: operations__work_order(where: { status: { not_in: $filter_0 } }) { count_id }",
    );
    expect(count.variables).toEqual({ filter_0: ["closed"] });

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
    expect(sum.query).toContain("{ sum_amount }");
    expect(sum.valueField).toBe("sum_amount");
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

    expect(built.query).toContain("and: [{ due_at: { gte: $filter_0_0_start }")
    expect(built.query).toContain("or: [{ status: { in: $filter_0_1_0 } }")
    expect(built.query).not.toContain("blocked");
    expect(built.variables).toMatchObject({
      filter_0_0_start: "2026-07-01T00:00:00.000Z",
      filter_0_0_end: "2026-10-01T00:00:00.000Z",
      filter_0_1_0: ["new"],
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
      "query RecordsReferenceLabels { rows: operations__account(where: { id: { in: $record_ids } }, limit: 2) { id name } }",
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
