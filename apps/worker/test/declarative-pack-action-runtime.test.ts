import { beforeEach, describe, expect, it, vi } from "vitest";

const query = vi.fn();
vi.mock("@neko/db", async (importOriginal) => ({
  ...await importOriginal<typeof import("@neko/db")>(),
  pool: () => ({ query }),
}));
vi.mock("@neko/llm/graphjin", async (importOriginal) => ({
  ...await importOriginal<typeof import("@neko/llm/graphjin")>(),
  graphjinQuery: vi.fn(),
  mintGraphjinToken: vi.fn(() => "executor-token"),
  resolvePackSource: vi.fn(async (_orgId, source) => source),
}));

vi.mock("@neko/llm/graphjin/pack-user-connections", async original => ({
  ...await original<typeof import("@neko/llm/graphjin/pack-user-connections")>(),
  packConnectionBindings: vi.fn(),
}));
import { packConnectionBindings } from "@neko/llm/graphjin/pack-user-connections";
import { graphjinQuery, mintGraphjinToken } from "@neko/llm/graphjin";
import { declarativePackActionAdapter } from "../src/packs/declarative-action-runtime.js";

const request = {
  id: "request-1",
  orgId: "org-1",
  kind: "google_workspace.sheets",
  status: "approved",
  approvedByUserId: "admin-1",
  actorUserId: "member-1",
  payload: {
    operation: "update_values",
    path: { spreadsheetId: "sheet-1", range: "Prices!A2:B2" },
    query: { valueInputOption: "RAW" },
    body: { values: [["SKU-1", 19.99]] },
  },
} as Parameters<typeof declarativePackActionAdapter>[0]["request"];

beforeEach(() => {
  vi.clearAllMocks();
  query.mockResolvedValue({ rows: [{
    id: "install-1", pack_id: "fixture", config: {},
    definition: {
      adapter: {
        kind: "graphjin_api_operation",
        source: "google_workspace_sheets",
        spec: "sheets",
        operations: {
          update_values: {
            operationId: "updateSpreadsheetValues",
            mutationRoot: "gws_sheets_update_values",
          },
        },
      },
    },
    enabled: true,
    readiness: "ready",
    readinessReason: null,
    installStatus: "installed",
    source: { id: "source-1", graphqlUrl: "https://graphjin.example", authMode: "jwt" },
  }] });
});

describe("declarative pack action runtime", () => {
  it("executes only the declared mutation with reviewed path, query, and body", async () => {
    vi.mocked(graphjinQuery).mockResolvedValue({
      data: { gws_sheets_update_values: { ok: true, status_code: 200, operation_id: "updateSpreadsheetValues" } },
    });
    await expect(declarativePackActionAdapter({ request })).resolves.toMatchObject({
      commandOrOperation: "updateSpreadsheetValues",
      result: { operation: "update_values", statusCode: 200 },
    });
    expect(mintGraphjinToken).toHaveBeenCalledWith(expect.objectContaining({ userId: "member-1" }));
    expect(vi.mocked(graphjinQuery).mock.calls[0]![0]).toMatchObject({
      role: "pack_api_executor",
      variables: { call: { path: request.payload.path, query: request.payload.query, body: request.payload.body } },
    });
  });

  it("binds the requester account despite JSONB key ordering and rejects a reconnect", async () => {
    const row = (await query()).rows[0];
    row.config = { _userOAuth: [{ key: "account" }] };
    query.mockResolvedValue({ rows: [row] });
    vi.mocked(packConnectionBindings).mockResolvedValue([{ installId: "install-1", connectionKey: "account", revision: "r1" }]);
    vi.mocked(graphjinQuery).mockResolvedValue({ data: { gws_sheets_update_values: { ok: true, status_code: 200 } } });
    const bound = { ...request, payload: { ...request.payload, _packConnections: [{ revision: "r1", connectionKey: "account", installId: "install-1" }] } };
    await declarativePackActionAdapter({ request: bound });
    expect(packConnectionBindings).toHaveBeenCalledWith({ orgId: "org-1", userId: "member-1" }, "install-1");
    expect(vi.mocked(graphjinQuery).mock.calls[0]?.[0].connectionBindings).toEqual([{ installId: "install-1", connectionKey: "account", revision: "r1" }]);
    vi.mocked(graphjinQuery).mockClear();
    vi.mocked(packConnectionBindings).mockResolvedValue([{ installId: "install-1", connectionKey: "account", revision: "r2" }]);
    await expect(declarativePackActionAdapter({ request: bound })).rejects.toThrow("changed");
    expect(graphjinQuery).not.toHaveBeenCalled();
  });
  it("rejects an operation that the installed pack did not declare", async () => {
    await expect(declarativePackActionAdapter({
      request: { ...request, payload: { ...request.payload, operation: "delete_spreadsheet" } },
    })).rejects.toThrow("unsupported operation");
    expect(graphjinQuery).not.toHaveBeenCalled();
  });

  it("locks body fields to pack inputs and sends the request id as the idempotency header", async () => {
    const row = (await query()).rows[0];
    row.config = { from_address: "Billing <billing@acme.com>" };
    row.definition.adapter.lockedBody = { from: { input: "from_address" } };
    row.definition.adapter.operations.update_values.idempotencyHeader = "Idempotency-Key";
    query.mockResolvedValue({ rows: [row] });
    vi.mocked(graphjinQuery).mockResolvedValue({ data: { gws_sheets_update_values: { ok: true, status_code: 200 } } });
    const sent = { ...request, payload: { ...request.payload, body: { from: "attacker@evil.test", values: [] } } };
    await declarativePackActionAdapter({ request: sent });
    expect(vi.mocked(graphjinQuery).mock.calls[0]![0].variables).toEqual({
      call: {
        path: request.payload.path,
        query: request.payload.query,
        headers: { "Idempotency-Key": "request-1" },
        body: { from: "Billing <billing@acme.com>", values: [] },
      },
    });
  });

  it("refuses a locked field whose pack input is not set", async () => {
    const row = (await query()).rows[0];
    row.config = {};
    row.definition.adapter.lockedBody = { from: { input: "from_address" } };
    query.mockResolvedValue({ rows: [row] });
    await expect(declarativePackActionAdapter({ request })).rejects.toThrow("needs the pack input from_address");
    expect(graphjinQuery).not.toHaveBeenCalled();
  });
});
