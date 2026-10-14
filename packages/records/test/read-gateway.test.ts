import type { Pool } from "pg";
import { describe, expect, it, vi } from "vitest";
import {
  recordsGraphjinSigningSecret,
  verifyRecordsGraphjinToken,
  type RecordsGraphjinTransport,
} from "../src/graphjin/client";
import { validateRecordIdentifier } from "../src/naming";
import {
  RecordsReadGateway,
  type RecordRegistryReader,
} from "../src/read/gateway";
import type { AppRegistrySnapshot } from "../src/types";

const id = validateRecordIdentifier;

function snapshot(): AppRegistrySnapshot {
  return {
    revision: "7",
    app: {
      orgId: "org-a",
      appId: "equipment",
      label: "Equipment",
      purpose: "Track equipment loans",
      status: "active",
      navOrder: 0,
      registryRevision: "7",
    },
    objects: [
      {
        id: "object-loan",
        orgId: "org-a",
        appId: "equipment",
        apiName: id("loan"),
        sourceApiName: null,
        label: "Loan",
        pluralLabel: "Loans",
        tableSchema: id("public"),
        tableName: id("equipment__loan"),
        nameField: id("subject"),
        visibility: "owner",
        custom: true,
        archivedAt: null,
        recordCount: "2",
      },
    ],
    fields: [
      {
        id: "field-subject",
        orgId: "org-a",
        objectId: "object-loan",
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
        id: "field-retired",
        orgId: "org-a",
        objectId: "object-loan",
        apiName: id("retired_note"),
        sourceApiName: null,
        label: "Retired note",
        kind: "text",
        columnName: id("retired_note"),
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
        objectId: "object-loan",
        kind: "list",
        definition: { columns: ["subject"] },
      },
      {
        objectId: "object-loan",
        kind: "detail",
        definition: { sections: [{ fields: ["subject"] }] },
      },
    ],
    pages: [
      {
        id: "page-overview",
        apiName: id("overview"),
        label: "Overview",
        definition: { blocks: [] },
        navOrder: 0,
      },
    ],
    permissions: [
      {
        appId: "equipment",
        role: "member",
        objectApiName: id("loan"),
        canRead: true,
        canCreate: true,
        canUpdate: true,
        canDelete: false,
      },
    ],
  };
}

function harness(result: Record<string, unknown>) {
  const current = snapshot();
  const registry: RecordRegistryReader = {
    async listApps() {
      return [current.app];
    },
    async loadApp(_orgId, appId) {
      return appId === current.app.appId ? current : null;
    },
  };
  const query = vi.fn(async (statement: string) => {
    if (statement.includes("from engine.app_access_grant")) {
      return { rows: [{ granted: true }] };
    }
    if (statement.includes("from engine.object_access_grant")) {
      return {
        rows: [
          {
            object_api_name: "loan",
            can_read: true,
            can_create: true,
            can_update: true,
            can_delete: false,
          },
        ],
      };
    }
    if (statement.includes("from engine.field_access_grant")) {
      return {
        rows: [{ field_id: "field-subject", can_read: true, can_write: true }],
      };
    }
    return { rows: [] };
  });
  const execute = vi.fn(async () => result);
  const gateway = new RecordsReadGateway(
    { query } as unknown as Pool,
    registry,
    { execute } as RecordsGraphjinTransport,
  );
  return { gateway, query, execute };
}

const viewer = {
  orgId: "org-a",
  userId: "member-1",
  role: "member" as const,
};

describe("RecordsReadGateway", () => {
  it("returns only active, readable, non-archived registry content", async () => {
    const { gateway } = harness({});
    const catalog = await gateway.listCatalog({
      viewer,
      activeAppIds: new Set(["equipment"]),
    });

    expect(catalog.apps).toHaveLength(1);
    expect(catalog.apps[0]).toMatchObject({
      appId: "equipment",
      objects: [
        {
          apiName: "loan",
          permissions: { canRead: true, canDelete: false },
          fields: [{ apiName: "subject" }],
        },
      ],
    });
    await expect(
      gateway.listCatalog({ viewer, activeAppIds: new Set() }),
    ).resolves.toEqual({ apps: [] });
  });

  it("queries only with a generated document and a synced actor JWT", async () => {
    const { gateway, query, execute } = harness({
      rows: [{ id: "loan-1", data: { subject: "Camera", secret_note: "hidden" } }],
      rows_cursor: "cursor-1",
      totals: [{ count_id: 1 }],
    });
    const result = await gateway.findRecords({
      viewer,
      activeAppIds: new Set(["equipment"]),
      appId: "equipment",
      objectApiName: "loan",
      search: "Camera",
      first: 10,
    });

    expect(query).toHaveBeenCalledWith(
      expect.stringContaining("insert into engine.actor"),
      ["org-a", "member-1", "member"],
    );
    const call = execute.mock.calls[0]?.[0];
    expect(call?.query).toContain("query RecordsList");
    expect(call?.query).toContain("equipment__loan");
    expect(call?.query).not.toContain("Camera");
    expect(call?.variables).toMatchObject({ first: 10, search: "%Camera%" });
    expect(
      verifyRecordsGraphjinToken(call?.token ?? "", {
        secret: recordsGraphjinSigningSecret("org-a"),
        orgId: "org-a",
      }),
    ).toMatchObject({ sub: "member-1", role: "member" });
    expect(result).toMatchObject({
      rows: [{ id: "loan-1", subject: "Camera" }],
      total: 1,
      cursor: "cursor-1",
    });
  });

  it("returns normalized recycle summaries through the same actor JWT", async () => {
    const { gateway, execute } = harness({
      rows: [
        {
          app_id: "equipment",
          object_api_name: "loan",
          record_id: "loan-deleted",
          record_name: "Camera",
          owner_user_id: "member-1",
          deleted_at: "2026-08-02T12:00:00Z",
          deleted_by: "member-1",
        },
      ],
      rows_cursor: "recycle-cursor",
      totals: [{ count: 1 }],
    });
    const result = await gateway.findRecycledRecords({
      viewer,
      appId: "equipment",
      objectApiName: "loan",
      first: 10,
      search: "Camera",
    });

    const call = execute.mock.calls[0]?.[0];
    expect(call?.operationName).toBe("RecordsRecycleList");
    expect(call?.query).toContain("recycle_record");
    expect(call?.query).not.toContain("Camera");
    expect(call?.variables).toMatchObject({
      app_id: "equipment",
      object_api_name: "loan",
      search: "%Camera%",
    });
    expect(result).toMatchObject({
      rows: [
        {
          appId: "equipment",
          objectApiName: "loan",
          recordId: "loan-deleted",
          recordName: "Camera",
          ownerUserId: "member-1",
          deletedAt: "2026-08-02T12:00:00Z",
          deletedBy: "member-1",
        },
      ],
      total: 1,
      cursor: "recycle-cursor",
    });
  });

  it("refuses records apps that the metadata plane has not marked active", async () => {
    const { gateway, execute } = harness({ rows: [] });
    await expect(
      gateway.getRecord({
        viewer,
        activeAppIds: new Set(),
        appId: "equipment",
        objectApiName: "loan",
        recordId: "loan-1",
      }),
    ).rejects.toThrow(/not active/);
    expect(execute).not.toHaveBeenCalled();
  });
});
