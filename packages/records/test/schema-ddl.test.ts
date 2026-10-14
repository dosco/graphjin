import { describe, expect, it } from "vitest";
import { validateRecordIdentifier } from "../src/naming";
import { buildRecordsGraphjinDdl } from "../src/schema/ddl";

const id = validateRecordIdentifier;

describe("buildRecordsGraphjinDdl", () => {
  it("gives every object the same table: system columns and one jsonb document", () => {
    const ddl = buildRecordsGraphjinDdl([
      {
        appId: "equipment",
        apiName: id("loan"),
        tableName: id("equipment__loan"),
        nameField: id("name"),
        visibility: "owner",
        fields: [
          { columnName: id("id"), kind: "id", required: true },
          { columnName: id("name"), kind: "text", required: true },
          { columnName: id("borrower_id"), kind: "reference", required: false },
          { columnName: id("replacement_cost"), kind: "currency", required: false },
          { columnName: id("tags"), kind: "multipicklist", required: false },
          { columnName: id("retired_note"), kind: "text", required: false, archived: true },
        ],
      },
    ]);

    expect(ddl).toBe(
      [
        "type equipment__loan {",
        "  id: Text! @id",
        "  org_id: Text!",
        "  owner_user_id: Text!",
        "  data: Jsonb!",
        "  nk_created_at: Timestamptz!",
        "  nk_created_by: Text!",
        "  nk_updated_at: Timestamptz!",
        "  nk_updated_by: Text!",
        "  nk_action_request_id: Text!",
        "  nk_mutation_id: Text!",
        "  nk_deleted_at: Timestamptz",
        "}",
        "",
      ].join("\n"),
    );
  });

  it("produces the same table when fields change", () => {
    const object = {
      appId: "crm",
      apiName: id("account"),
      tableName: id("crm__account"),
      nameField: id("name"),
      visibility: "org" as const,
    };
    const before = buildRecordsGraphjinDdl([
      { ...object, fields: [{ columnName: id("name"), kind: "text" as const, required: true }] },
    ]);
    const after = buildRecordsGraphjinDdl([
      {
        ...object,
        fields: [
          { columnName: id("name"), kind: "text" as const, required: true },
          { columnName: id("industry"), kind: "picklist" as const, required: false },
        ],
      },
    ]);
    expect(after).toBe(before);
  });

  it("reserves the document column name", () => {
    expect(() =>
      buildRecordsGraphjinDdl([
        {
          appId: "crm",
          apiName: id("account"),
          tableName: id("crm__account"),
          nameField: id("name"),
          visibility: "org",
          fields: [
            { columnName: id("name"), kind: "text", required: true },
            { columnName: id("data"), kind: "text", required: false },
          ],
        },
      ]),
    ).toThrow(/system column/);
  });

  it("omits archived objects without authoring destructive DDL", () => {
    const ddl = buildRecordsGraphjinDdl([
      {
        appId: "crm",
        apiName: id("account"),
        tableName: id("crm__account"),
        nameField: id("name"),
        visibility: "org",
        fields: [{ columnName: id("name"), kind: "text", required: true }],
      },
      {
        appId: "crm",
        apiName: id("legacy"),
        tableName: id("crm__legacy"),
        nameField: id("name"),
        visibility: "org",
        archived: true,
        fields: [{ columnName: id("name"), kind: "text", required: false }],
      },
    ]);
    expect(ddl).toContain("crm__account");
    expect(ddl).not.toContain("crm__legacy");
  });

  it("represents an all-archived registry without requesting table drops", () => {
    const ddl = buildRecordsGraphjinDdl([
      {
        appId: "crm",
        apiName: id("legacy"),
        tableName: id("crm__legacy"),
        nameField: id("name"),
        visibility: "org",
        archived: true,
        fields: [{ columnName: id("name"), kind: "text", required: false }],
      },
    ]);
    expect(ddl).toMatch(/^# No live records objects/);
    expect(ddl).not.toMatch(/\bDROP\b/i);
  });

  it("rejects malformed, duplicate, or system-column fields", () => {
    const base = {
      appId: "crm",
      apiName: id("account"),
      tableName: id("crm__account"),
      nameField: id("name"),
      visibility: "org" as const,
    };
    expect(() =>
      buildRecordsGraphjinDdl([
        {
          ...base,
          fields: [{ columnName: id("org_id"), kind: "text", required: true }],
        },
      ]),
    ).toThrow(/system column/);
    expect(() =>
      buildRecordsGraphjinDdl([
        {
          ...base,
          fields: [
            { columnName: id("name"), kind: "text", required: true },
            { columnName: id("name"), kind: "text", required: false },
          ],
        },
      ]),
    ).toThrow(/duplicate/);
  });
});
