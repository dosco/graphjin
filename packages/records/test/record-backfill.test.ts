import type { Pool } from "pg";
import { describe, expect, it, vi } from "vitest";
import { recordIdentifier } from "../src/naming";
import type { AppRegistrySnapshot } from "../src/types";
import {
  RecordBackfillAuditError,
  RecordBackfillExecutor,
  RecordBackfillPermissionError,
  RecordBackfillValueError,
} from "../src/write/backfill";

const now = new Date("2026-08-03T06:00:00.000Z");

function snapshot(canUpdate = true): AppRegistrySnapshot {
  const objectId = "00000000-0000-0000-0000-000000000801";
  return {
    revision: "1",
    app: {
      orgId: "org-a",
      appId: "crm",
      label: "CRM",
      purpose: null,
      status: "active",
      navOrder: 0,
      registryRevision: "1",
    },
    objects: [
      {
        id: objectId,
        orgId: "org-a",
        appId: "crm",
        apiName: recordIdentifier("account"),
        sourceApiName: null,
        label: "Account",
        pluralLabel: "Accounts",
        tableSchema: recordIdentifier("public"),
        tableName: recordIdentifier("crm__account"),
        nameField: recordIdentifier("legacy_count"),
        visibility: "org",
        custom: true,
        archivedAt: null,
        recordCount: null,
      },
    ],
    fields: [
      {
        id: "00000000-0000-0000-0000-000000000802",
        orgId: "org-a",
        objectId,
        apiName: recordIdentifier("legacy_count"),
        sourceApiName: null,
        label: "Legacy count",
        kind: "text",
        columnName: recordIdentifier("legacy_count"),
        required: false,
        readOnly: false,
        archivedAt: null,
        picklistValues: null,
        referenceTargets: null,
        length: null,
        scale: null,
      },
      {
        id: "00000000-0000-0000-0000-000000000803",
        orgId: "org-a",
        objectId,
        apiName: recordIdentifier("count_v2"),
        sourceApiName: null,
        label: "Count",
        kind: "integer",
        columnName: recordIdentifier("count_v2"),
        required: false,
        readOnly: false,
        archivedAt: null,
        picklistValues: null,
        referenceTargets: null,
        length: null,
        scale: null,
      },
    ],
    layouts: [],
    pages: [],
    permissions: [
      {
        appId: "crm",
        role: "admin",
        objectApiName: recordIdentifier("account"),
        canRead: true,
        canCreate: true,
        canUpdate,
        canDelete: true,
      },
    ],
  };
}

function rawAction(
  status: "running" | "succeeded" | "failed" = "running",
  result: Record<string, unknown> | null = null,
  error: Record<string, unknown> | null = null,
) {
  return {
    action_request_id: "request-backfill",
    org_id: "org-a",
    app_id: "crm",
    action_kind: "record_backfill",
    status,
    lease_owner: status === "running" ? "worker-1" : null,
    lease_expires_at:
      status === "running" ? new Date(now.getTime() + 60_000) : null,
    result,
    error,
    started_at: now,
    finished_at: status === "running" ? null : now,
  };
}

function harness(input: {
  rows?: Array<Record<string, unknown>>;
  remainingRows?: Array<Record<string, unknown>>;
  writeAudits?: boolean;
  canUpdate?: boolean;
} = {}) {
  const rows = input.rows ?? [
    { id: "account-1", legacy_count: "42" },
    { id: "account-2", legacy_count: null },
  ];
  const audits = new Map<string, string>();
  let failed = false;
  let succeeded = false;
  const query = vi.fn(async (sqlValue: unknown, values: unknown[] = []) => {
    const sql = String(sqlValue);
    if (sql.includes("set lease_expires_at")) {
      return { rows: [rawAction()], rowCount: 1 };
    }
    if (sql.includes("select mutation_id from engine.record_change_log")) {
      const requested = values[4] as string[];
      const found = requested
        .filter((id) => audits.has(id))
        .map((mutation_id) => ({ mutation_id }));
      return { rows: found, rowCount: found.length };
    }
    if (sql.includes("select distinct record_id")) {
      const found = [...new Set(audits.values())]
        .sort()
        .map((record_id) => ({ record_id }));
      return { rows: found, rowCount: found.length };
    }
    if (sql.includes("set status = 'succeeded'")) {
      succeeded = true;
      return {
        rows: [rawAction("succeeded", JSON.parse(String(values[2])))],
        rowCount: 1,
      };
    }
    if (sql.includes("set status = 'failed'")) {
      failed = true;
      return {
        rows: [rawAction("failed", null, JSON.parse(String(values[2])))],
        rowCount: 1,
      };
    }
    throw new Error(`unexpected pool query: ${sql}`);
  });
  const clientQuery = vi.fn(async (sqlValue: unknown) => {
    const sql = String(sqlValue);
    if (sql === "begin" || sql === "commit" || sql === "rollback") {
      return { rows: [], rowCount: 0 };
    }
    if (sql.includes("insert into engine.action_execution")) {
      return { rows: [rawAction()], rowCount: 1 };
    }
    throw new Error(`unexpected client query: ${sql}`);
  });
  const pool = {
    query,
    connect: vi.fn(async () => ({ query: clientQuery, release: vi.fn() })),
  } as unknown as Pool;
  const execute = vi.fn(
    async (request: {
      query: string;
      variables?: Record<string, unknown>;
    }) => {
      if (request.query.startsWith("query RecordsFieldBackfillPage")) {
        return {
          rows: rows.map(({ id, ...fields }) => ({ id, data: fields })),
        };
      }
      if (
        request.query.startsWith("query RecordsFieldBackfillRemainingPage")
      ) {
        return { rows: input.remainingRows ?? [] };
      }
      for (const [key, value] of Object.entries(request.variables ?? {})) {
        if (key.startsWith("data_") && input.writeAudits !== false) {
          const index = key.slice("data_".length);
          const mutation = value as Record<string, string>;
          audits.set(
            mutation.nk_mutation_id,
            String(request.variables?.[`id_${index}`]),
          );
        }
      }
      return { row_0: [{ id: "account-1" }] };
    },
  );
  const registry = {
    loadApp: vi.fn(async () => snapshot(input.canUpdate ?? true)),
  };
  const sourceWrite = vi.fn().mockResolvedValue(undefined);
  const executor = new RecordBackfillExecutor({
    pool,
    graphjin: { execute },
    serviceToken: () => "service-token",
    leaseOwner: "worker-1",
    recordSourceWrite: sourceWrite,
    registry: registry as never,
  });
  return {
    executor,
    execute,
    audits,
    sourceWrite,
    failed: () => failed,
    succeeded: () => succeeded,
  };
}

describe("record field backfill", () => {
  it("transforms source values, skips nulls, and audits only live null targets", async () => {
    const test = harness({ remainingRows: [{ id: "account-2" }] });
    await expect(
      test.executor.execute({
        actionRequestId: "request-backfill",
        orgId: "org-a",
        appId: "crm",
        objectApiName: "account",
        sourceFieldApiName: "legacy_count",
        targetFieldApiName: "count_v2",
        transform: "integer",
        actorUserId: "admin-1",
      }),
    ).resolves.toMatchObject({
      scanned: 2,
      updated: 1,
      skippedNullSource: 1,
      remainingNull: 1,
      sourceFieldApiName: "legacy_count",
      transform: "integer",
    });
    const mutation = test.execute.mock.calls.find(([request]) =>
      request.query.startsWith("mutation RecordsFieldBackfillBatch"),
    )?.[0];
    expect(mutation.query).toContain("data: { count_v2: { is_null: true } }");
    expect(mutation.query).toContain("nk_deleted_at: { is_null: true }");
    expect(mutation.variables).toMatchObject({
      id_0: "account-1",
      data_0: {
        data: { count_v2: 42 },
        nk_updated_by: "admin-1",
        nk_action_request_id: "request-backfill",
      },
    });
    expect(test.audits).toHaveLength(1);
    expect(test.sourceWrite).toHaveBeenCalledWith({
      actionRequestId: "request-backfill",
      orgId: "org-a",
      tableName: "crm__account",
      recordId: "account-1",
    });
    expect(test.succeeded()).toBe(true);
  });

  it("supports a validated constant without reading a source field", async () => {
    const test = harness({ rows: [{ id: "account-1" }] });
    await expect(
      test.executor.execute({
        actionRequestId: "request-backfill",
        orgId: "org-a",
        appId: "crm",
        objectApiName: "account",
        targetFieldApiName: "count_v2",
        value: 7,
        actorUserId: "admin-1",
      }),
    ).resolves.toMatchObject({
      updated: 1,
      sourceFieldApiName: null,
      transform: null,
    });
    const page = test.execute.mock.calls[0]?.[0];
    expect(page?.query).not.toContain("legacy_count");
    const mutation = test.execute.mock.calls[1]?.[0];
    expect(mutation?.variables).toMatchObject({
      data_0: { data: { count_v2: 7 } },
    });
  });

  it("fails the durable command on an invalid source conversion", async () => {
    const test = harness({
      rows: [{ id: "account-1", legacy_count: "forty-two" }],
    });
    await expect(
      test.executor.execute({
        actionRequestId: "request-backfill",
        orgId: "org-a",
        appId: "crm",
        objectApiName: "account",
        sourceFieldApiName: "legacy_count",
        targetFieldApiName: "count_v2",
        transform: "integer",
        actorUserId: "admin-1",
      }),
    ).rejects.toBeInstanceOf(RecordBackfillValueError);
    expect(test.failed()).toBe(true);
    expect(test.audits).toHaveLength(0);
  });

  it("fails retryably when an attempted mutation lacks its audit receipt", async () => {
    const test = harness({
      rows: [{ id: "account-1", legacy_count: "42" }],
      writeAudits: false,
    });
    await expect(
      test.executor.execute({
        actionRequestId: "request-backfill",
        orgId: "org-a",
        appId: "crm",
        objectApiName: "account",
        sourceFieldApiName: "legacy_count",
        targetFieldApiName: "count_v2",
        transform: "integer",
        actorUserId: "admin-1",
      }),
    ).rejects.toBeInstanceOf(RecordBackfillAuditError);
    expect(test.failed()).toBe(false);
    expect(test.succeeded()).toBe(false);
  });

  it("uses the shared object evaluator and denies an admin without update grant", async () => {
    const test = harness({ canUpdate: false });
    await expect(
      test.executor.execute({
        actionRequestId: "request-backfill",
        orgId: "org-a",
        appId: "crm",
        objectApiName: "account",
        targetFieldApiName: "count_v2",
        value: 7,
        actorUserId: "admin-1",
      }),
    ).rejects.toBeInstanceOf(RecordBackfillPermissionError);
    expect(test.execute).not.toHaveBeenCalled();
  });
});
