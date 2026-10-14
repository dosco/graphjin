import { describe, expect, it, vi } from "vitest";
import { validateRecordIdentifier, type AppRegistrySnapshot } from "@neko/records";
import type { ActionRequestRecord } from "@neko/llm/workflows";
import {
  applyRecordNewFields,
  inferFieldKind,
  proposeRecordNewFields,
  RecordNewFieldsError,
} from "../../src/records/new-fields.js";

const id = validateRecordIdentifier;

function snapshot(): AppRegistrySnapshot {
  return {
    revision: "3",
    app: {
      orgId: "org-a",
      appId: "social",
      label: "Social calendar",
      purpose: null,
      status: "active",
      navOrder: 0,
      registryRevision: "3",
    },
    objects: [
      {
        id: "object-post",
        orgId: "org-a",
        appId: "social",
        apiName: id("post"),
        sourceApiName: null,
        label: "Post",
        pluralLabel: "Posts",
        tableSchema: id("public"),
        tableName: id("social__post"),
        nameField: id("title"),
        visibility: "org",
        custom: true,
        archivedAt: null,
        recordCount: "0",
      },
    ],
    fields: [
      {
        id: "field-title",
        orgId: "org-a",
        objectId: "object-post",
        apiName: id("title"),
        sourceApiName: null,
        label: "Title",
        kind: "text",
        columnName: id("title"),
        required: true,
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
        objectId: "object-post",
        apiName: id("old_tag"),
        sourceApiName: null,
        label: "Old tag",
        kind: "text",
        columnName: id("old_tag"),
        required: false,
        readOnly: false,
        archivedAt: new Date("2026-01-01T00:00:00Z"),
        picklistValues: null,
        referenceTargets: null,
        length: null,
        scale: null,
      },
    ],
    layouts: [],
    pages: [],
    permissions: [],
  };
}

const registry = { loadApp: vi.fn(async () => snapshot()) };

function request(payload: Record<string, unknown>, approvedByUserId: string | null = "admin-1") {
  return {
    id: "request-1",
    orgId: "org-a",
    kind: "record_create",
    payload,
    approvedByUserId,
  } as unknown as ActionRequestRecord;
}

describe("record new fields", () => {
  it("infers a field type from each value", () => {
    expect(inferFieldKind(true)).toEqual({ kind: "boolean" });
    expect(inferFieldKind(42)).toEqual({ kind: "integer" });
    expect(inferFieldKind(2.5)).toEqual({ kind: "decimal" });
    expect(inferFieldKind("2026-10-14")).toEqual({ kind: "date" });
    expect(inferFieldKind("2026-10-14T09:00:00Z")).toEqual({ kind: "datetime" });
    expect(inferFieldKind("ops@example.com")).toEqual({ kind: "email" });
    expect(inferFieldKind("https://openneko.app")).toEqual({ kind: "url" });
    expect(inferFieldKind("Short note")).toEqual({ kind: "text" });
    expect(inferFieldKind("line one\nline two")).toEqual({ kind: "textarea" });
    expect(inferFieldKind(["a", "b", "a"])).toEqual({ kind: "multipicklist", picklistValues: ["a", "b"] });
    expect(() => inferFieldKind({ nested: true })).toThrow(RecordNewFieldsError);
  });

  it("proposes only the keys the object does not define", async () => {
    await expect(
      proposeRecordNewFields(
        registry,
        request({ app: "social", object: "post", fields: { title: "Launch", reach: 120, go_live: "2026-11-01" } }),
      ),
    ).resolves.toEqual([
      { api_name: "reach", label: "Reach", kind: "integer" },
      { api_name: "go_live", label: "Go live", kind: "date" },
    ]);
  });

  it("refuses archived names, bad names and empty values", async () => {
    await expect(
      proposeRecordNewFields(registry, request({ app: "social", object: "post", fields: { old_tag: "x" } })),
    ).rejects.toThrow(/archived/);
    await expect(
      proposeRecordNewFields(registry, request({ app: "social", object: "post", fields: { "Bad Name": "x" } })),
    ).rejects.toThrow(/lowercase/);
    await expect(
      proposeRecordNewFields(registry, request({ app: "social", object: "post", fields: { reach: null } })),
    ).rejects.toThrow(/needs a value/);
  });

  it("adds approved fields through the schema path, then resumes without repeating", async () => {
    const changes = new Map<string, { phase: string; previewHash: string }>();
    const schema = {
      planner: {
        prepare: vi.fn(async (input: { id: string }) => {
          const change = { phase: "planned", previewHash: "hash" };
          changes.set(input.id, change);
          return { change, payload: {}, preview: {} } as never;
        }),
      },
      saga: {
        get: vi.fn(async (journalId: string) => (changes.get(journalId) ?? null) as never),
        approve: vi.fn(async (journalId: string) => {
          changes.get(journalId)!.phase = "approved";
          return changes.get(journalId) as never;
        }),
        execute: vi.fn(async (journalId: string) => {
          changes.get(journalId)!.phase = "projected";
          return changes.get(journalId) as never;
        }),
      },
    };
    const write = request({
      app: "social",
      object: "post",
      fields: { title: "Launch", reach: 120 },
      new_fields: [{ api_name: "reach", label: "Reach", kind: "integer" }],
    });
    await expect(
      applyRecordNewFields({ request: write, schema, approverIsAdmin: async () => true }),
    ).resolves.toEqual(["reach"]);
    expect(schema.planner.prepare).toHaveBeenCalledWith(
      expect.objectContaining({
        id: "request-1:field:reach",
        kind: "app_field_add",
        payload: {
          app: "social",
          object: "post",
          field: { api_name: "reach", label: "Reach", kind: "integer", required: false },
        },
        actorRole: "admin",
      }),
    );
    await applyRecordNewFields({ request: write, schema, approverIsAdmin: async () => true });
    expect(schema.planner.prepare).toHaveBeenCalledTimes(1);
    expect(schema.saga.execute).toHaveBeenCalledTimes(1);
  });

  it("requires an administrator to approve new fields", async () => {
    await expect(
      applyRecordNewFields({
        request: request({
          app: "social",
          object: "post",
          fields: { reach: 1 },
          new_fields: [{ api_name: "reach", label: "Reach", kind: "integer" }],
        }),
        schema: {} as never,
        approverIsAdmin: async () => false,
      }),
    ).rejects.toThrow(/administrator/);
  });
});
