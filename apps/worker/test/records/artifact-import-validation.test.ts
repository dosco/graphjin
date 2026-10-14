import { describe, expect, it, vi } from "vitest";
import type { Pool } from "pg";
import {
  buildRecordImportPlan,
  recordIdentifier,
  type AppRegistrySnapshot,
  type RecordImportPlan,
} from "@neko/records";
import { validateArtifactImport } from "../../src/records/artifact-import-validation.js";

const orgId = "org-validation";
const appId = "crm";
const accountObjectId = "00000000-0000-4000-a000-000000000851";
const contactObjectId = "00000000-0000-4000-a000-000000000852";
const accountRunId = "00000000-0000-4000-a000-000000000853";
const contactRunId = "00000000-0000-4000-a000-000000000854";

function snapshot(): AppRegistrySnapshot {
  const objects = [
    {
      id: accountObjectId,
      orgId,
      appId,
      apiName: recordIdentifier("account"),
      sourceApiName: "Account",
      label: "Account",
      pluralLabel: "Accounts",
      tableSchema: recordIdentifier("public"),
      tableName: recordIdentifier("crm__account"),
      nameField: recordIdentifier("name"),
      visibility: "org" as const,
      custom: false,
      archivedAt: null,
      recordCount: null,
    },
    {
      id: contactObjectId,
      orgId,
      appId,
      apiName: recordIdentifier("contact"),
      sourceApiName: "Contact",
      label: "Contact",
      pluralLabel: "Contacts",
      tableSchema: recordIdentifier("public"),
      tableName: recordIdentifier("crm__contact"),
      nameField: recordIdentifier("name"),
      visibility: "org" as const,
      custom: false,
      archivedAt: null,
      recordCount: null,
    },
  ];
  return {
    revision: "1",
    app: {
      orgId,
      appId,
      label: "CRM",
      purpose: null,
      status: "active",
      navOrder: 0,
      registryRevision: "1",
    },
    objects,
    fields: [
      {
        id: "00000000-0000-4000-a000-000000000855",
        orgId,
        objectId: accountObjectId,
        apiName: recordIdentifier("name"),
        sourceApiName: "Name",
        label: "Name",
        kind: "text",
        columnName: recordIdentifier("name"),
        required: true,
        readOnly: false,
        archivedAt: null,
        picklistValues: null,
        referenceTargets: null,
        length: 255,
        scale: null,
      },
      {
        id: "00000000-0000-4000-a000-000000000856",
        orgId,
        objectId: contactObjectId,
        apiName: recordIdentifier("name"),
        sourceApiName: "Name",
        label: "Name",
        kind: "text",
        columnName: recordIdentifier("name"),
        required: true,
        readOnly: false,
        archivedAt: null,
        picklistValues: null,
        referenceTargets: null,
        length: 255,
        scale: null,
      },
      {
        id: "00000000-0000-4000-a000-000000000857",
        orgId,
        objectId: contactObjectId,
        apiName: recordIdentifier("accountid"),
        sourceApiName: "AccountId",
        label: "Account",
        kind: "reference",
        columnName: recordIdentifier("accountid"),
        required: false,
        readOnly: false,
        archivedAt: null,
        picklistValues: null,
        referenceTargets: [recordIdentifier("account")],
        length: null,
        scale: null,
      },
    ],
    layouts: [],
    pages: [],
    permissions: objects.flatMap((object) => [
      {
        appId,
        role: "admin",
        objectApiName: object.apiName,
        canRead: true,
        canCreate: false,
        canUpdate: false,
        canDelete: false,
      },
      {
        appId,
        role: "member",
        objectApiName: object.apiName,
        canRead: true,
        canCreate: false,
        canUpdate: false,
        canDelete: false,
      },
    ]),
  };
}

function plans(registry: AppRegistrySnapshot): RecordImportPlan[] {
  return [
    buildRecordImportPlan({
      snapshot: registry,
      objectApiName: "account",
      sourcePath: "imports/salesforce/job/data/account.csv",
      sourceName: "account.csv",
      bytes: Buffer.from("id,Name\naccount-1,Acme\n"),
      mapping: { id: "id", Name: "name" },
      allowReadOnly: true,
    }),
    buildRecordImportPlan({
      snapshot: registry,
      objectApiName: "contact",
      sourcePath: "imports/salesforce/job/data/contact.csv",
      sourceName: "contact.csv",
      bytes: Buffer.from(
        "id,Name,AccountId\ncontact-1,Ada,account-1\ncontact-2,Bob,missing-account\n",
      ),
      mapping: { id: "id", Name: "name", AccountId: "accountid" },
      allowReadOnly: true,
    }),
  ];
}

describe("artifact import Stage 6 validation", () => {
  it("reconciles rows and samples, counts dangling references, and refreshes metadata", async () => {
    const registry = snapshot();
    const [accountPlan, contactPlan] = plans(registry);
    const controlQueries: string[] = [];
    const metadataQueries: string[] = [];
    const client = {
      query: vi.fn(async (sql: string) => {
        metadataQueries.push(sql);
        if (sql.includes("returning revision::text")) {
          return { rowCount: 1, rows: [{ revision: "2" }] };
        }
        return { rowCount: sql.includes("update engine.record_object") ? 1 : null, rows: [] };
      }),
      release: vi.fn(),
    };
    const pool = {
      query: vi.fn(async (sql: string) => {
        controlQueries.push(sql);
        if (sql.includes("result->'plan'")) {
          return {
            rowCount: 2,
            rows: [
              {
                id: accountRunId,
                plan: accountPlan,
                report: {
                  sourceRows: 1,
                  inserted: 1,
                  rejected: 0,
                  duplicates: 0,
                  reconciled: true,
                },
              },
              {
                id: contactRunId,
                plan: contactPlan,
                report: {
                  sourceRows: 2,
                  inserted: 2,
                  rejected: 0,
                  duplicates: 0,
                  reconciled: true,
                },
              },
            ],
          };
        }
        if (sql.includes("from engine.import_reject")) {
          return { rowCount: 0, rows: [] };
        }
        throw new Error(`unexpected control query: ${sql}`);
      }),
      connect: vi.fn(async () => client),
    } as unknown as Pool;
    const graphjin = {
      execute: vi.fn(async (input: { operationName: string; query: string }) => {
        if (input.operationName === "RecordsImportValidationCount") {
          return { totals: [{ count_id: input.query.includes("crm__account") ? 1 : 2 }] };
        }
        if (input.operationName === "RecordsImportValidationSample") {
          return input.query.includes("crm__account")
            ? { rows: [{ id: "account-1", data: { name: "Acme" } }] }
            : {
                rows: [
                  { id: "contact-1", data: { name: "Ada", accountid: "account-1" } },
                  { id: "contact-2", data: { name: "Bob", accountid: "missing-account" } },
                ],
              };
        }
        if (input.operationName === "RecordsImportReferenceScan") {
          return {
            rows: [
              { id: "contact-1", data: { accountid: "account-1" } },
              { id: "contact-2", data: { accountid: "missing-account" } },
            ],
          };
        }
        if (input.operationName === "RecordsImportReferenceTargets") {
          return { rows: [{ id: "account-1" }] };
        }
        throw new Error(`unexpected GraphJin operation: ${input.operationName}`);
      }),
    };

    await expect(
      validateArtifactImport({
        pool,
        orgId,
        appId,
        runIds: [accountRunId, contactRunId],
        graphjin,
        serviceToken: "service-token",
        registry: { loadApp: async () => registry },
      }),
    ).resolves.toMatchObject({
      integrity: "passed",
      rows: [
        { objectApiName: "account", expected: 1, live: 1, reconciled: true },
        { objectApiName: "contact", expected: 2, live: 2, reconciled: true },
      ],
      sampledChecksums: [
        { objectApiName: "account", verified: 1, matched: true },
        { objectApiName: "contact", verified: 2, matched: true },
      ],
      danglingReferences: [
        {
          objectApiName: "contact",
          fieldApiName: "accountid",
          populated: 2,
          dangling: 1,
          sampleValues: ["missing-account"],
        },
      ],
      permissionCollapse: {
        strategy: "admin_member",
        roles: [
          { role: "admin", objects: 2, readable: 2 },
          { role: "member", objects: 2, readable: 2 },
        ],
      },
      recordCountsRefreshed: true,
    });
    expect(controlQueries.every((query) => query.includes("engine."))).toBe(true);
    expect(metadataQueries.filter((query) => query.includes("record_object"))).toHaveLength(2);
    expect(graphjin.execute).toHaveBeenCalledWith(
      expect.objectContaining({
        operationName: "RecordsImportReferenceScan",
        token: "service-token",
      }),
    );
  });
});
