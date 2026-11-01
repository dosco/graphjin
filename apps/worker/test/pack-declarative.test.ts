import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { loadSolutionPack, type PackArtifact, type SolutionPackBundle } from "@neko/packs";
import { bindPackQueries, declarativeGraphjinUpdate, declarativePackPermissions, installedPackPolicyEnabled, packPolicyControlsWrite, packValue } from "../src/packs/declarative.js";
import { nativeArtifactStateHash } from "../src/packs/artifact-state.js";

function bundle(): SolutionPackBundle {
  const artifact = (kind: PackArtifact["kind"], content: unknown, path = ""): PackArtifact => ({ kind, content, path, key: `${kind}.health`, targetRef: "health", hash: "fixture" });
  return { manifest: { oauth: [], metadata: { id: "fixture" }, health: { requiredPreflight: [], postInstall: [], postWriteCanary: [], readiness: {} } }, artifacts: [
    artifact("source", { name: "service_health", kind: "api", base_url: "{{service.base_url}}", openapi: "graphjin/specs/service-health.yaml", auth: { type: "bearer", token: "{{secret.service.api_token}}" } }),
    artifact("spec", { openapi: "3.0.3", paths: { "/health-summary": { get: { operationId: "getHealthSummary" } } } }, "graphjin/specs/service-health.yaml"),
    artifact("relationships", { source: "service_health", relationships: [] }),
    artifact("saved_query", "query Health { health { healthy } }"),
  ] } as SolutionPackBundle;
}
const inputs = { "service.base_url": "https://health.example.test" };
const secrets = { "service.api_token": 'fixture-"token\\with\ncharacters' };

describe("declarative pack configuration", () => {
  it("exposes the installed Google Workspace writes only to the pack executor", async () => {
    const here = dirname(fileURLToPath(import.meta.url));
    const workspace = await loadSolutionPack(resolve(here, "../../../packs/google-workspace"));
    const update = declarativeGraphjinUpdate(workspace, {}, {
      "google-workspace.access_token": "token",
    }) as { roles: Array<{ name: string }>; update_sources: Array<Record<string, unknown>> };
    expect(update.roles).toEqual([{ name: "pack_api_executor", comment: expect.any(String) }]);
    const gmail = update.update_sources.find((source) => source.name === "google_workspace_gmail") as {
      specs: Record<string, { operations: Record<string, Record<string, unknown>> }>;
    };
    expect(gmail.specs.gmail.operations.sendMessage).toEqual({
      expose_mutation: true,
      allowed_roles: ["pack_api_executor"],
      expose_as: "gws_gmail_send_message",
    });
    expect(gmail.specs.gmail).toMatchObject({ auth: { scheme: "bearer", token_from_request: { header: expect.stringMatching(/^X-OpenNeko-Pack-/) } } });
    expect(JSON.stringify(update)).not.toContain('"token":"token"');
    expect(gmail.specs.gmail.operations.getMessage).toEqual({ expose_top_level: true });
  });

  it("sends spec documents and saved queries inline for a GraphJin that keeps its own config", () => {
    const pack = bundle();
    const update = declarativeGraphjinUpdate(pack, inputs, secrets, [], {}, {
      specDocuments: new Map([["graphjin/specs/service-health.yaml", "openapi: 3.0.3\npaths: {}\n"]]),
      savedQueries: [{ name: "fixture_health", query: "query Health { health { healthy } }" }],
    }) as { update_sources: Array<Record<string, unknown>>; update_saved_queries: unknown };
    const source = update.update_sources[0]!;
    expect(source).not.toHaveProperty("specs_dir");
    expect(source.specs).toMatchObject({ "service-health": { document: "openapi: 3.0.3\npaths: {}\n", base_url: inputs["service.base_url"] } });
    expect(update.update_saved_queries).toEqual([{ name: "fixture_health", query: "query Health { health { healthy } }" }]);
  });

  it("rejects GraphJin readiness checks for a pack without GraphJin", () => {
    const pack = bundle();
    pack.manifest.artifacts = { skills: [] };
    pack.artifacts = [];
    pack.manifest.health.postInstall = ["graphjin-reload"];
    expect(() => declarativeGraphjinUpdate(pack, {}, {})).toThrow("requires GraphJin artifacts");
  });

  it("maps a bundled connector to the existing read-only GraphJin contract without mutating the pack", () => {
    const pack = bundle();
    const original = JSON.stringify(pack);
    const update = declarativeGraphjinUpdate(pack, inputs, secrets, ["old_health"]);
    expect(update).toMatchObject({
      update_sources: [{ name: "service_health", kind: "api", read_only: true, specs_dir: "/config/specs",
        access: { read: "authenticated", write: "blocked", delete: "blocked" },
        specs: { "service-health": { base_url: inputs["service.base_url"], auth: { scheme: "bearer", token: secrets["service.api_token"] } } } }],
      source_patches: [{ name: "old_health", access: { read: "blocked" } }],
    });
    expect(update).not.toHaveProperty("relationships");
    expect(JSON.stringify(pack)).toBe(original);
  });

  it("maps an API key credential to GraphJin api_key auth", () => {
    const pack = bundle();
    const source = pack.artifacts[0]!.content as Record<string, unknown>;
    source.auth = { type: "api_key", key_name: "Api-Key", key_value: "{{secret.service.api_token}}" };
    expect(declarativeGraphjinUpdate(pack, inputs, secrets)).toMatchObject({
      update_sources: [{ specs: { "service-health": { auth: { scheme: "api_key", key_name: "Api-Key", key_value: secrets["service.api_token"], key_in: "header" } } } }],
    });
    source.auth = { type: "api_key", key_name: "api_key", key_value: "{{secret.service.api_token}}", key_in: "query" };
    expect(declarativeGraphjinUpdate(pack, inputs, secrets)).toMatchObject({
      update_sources: [{ specs: { "service-health": { auth: { scheme: "api_key", key_name: "api_key", key_in: "query" } } } }],
    });
    for (const auth of [
      { type: "api_key", key_name: "Api-Key", key_value: "plaintext" },
      { type: "api_key", key_name: "Api Key", key_value: "{{secret.service.api_token}}" },
      { type: "api_key", key_name: "Api-Key", key_value: "{{secret.service.api_token}}", key_in: "cookie" },
      { type: "api_key", key_name: "Api-Key", key_value: "{{secret.service.api_token}}", token: "{{secret.service.api_token}}" },
      { type: "api_key", key_value: "{{secret.service.api_token}}" },
      { type: "basic", username: "reader", password: "{{secret.service.api_token}}" },
    ]) {
      source.auth = auth;
      expect(() => declarativeGraphjinUpdate(pack, inputs, secrets)).toThrow();
    }
  });

  it("preserves a pack API source write request while keeping undeclared operations blocked", () => {
    const pack = bundle();
    pack.artifacts[0]!.content = {
      ...(pack.artifacts[0]!.content as Record<string, unknown>),
      read_only: false,
      capabilities: { "api.read": true, "api.write": true, "api.delete": false },
    };
    (pack.artifacts[1]!.content as Record<string, unknown>).paths = {
      "/health-summary": {
        get: { operationId: "getHealthSummary" },
        put: { operationId: "updateHealth" },
      },
    };
    expect(declarativeGraphjinUpdate(pack, inputs, secrets)).toMatchObject({
      update_sources: [{
        name: "service_health",
        read_only: false,
        access: { read: "authenticated", write: "authenticated", delete: "blocked" },
        capabilities: { "api.read": true, "api.write": true, "api.delete": false },
      }],
    });
    expect(declarativePackPermissions(pack)).toEqual({
      database: "none",
      apiWrite: "requested; actions require an enabled policy",
    });
  });

  it("passes explicit GET top-level exposure to GraphJin", () => {
    const pack = bundle();
    pack.artifacts[0]!.content = {
      ...(pack.artifacts[0]!.content as Record<string, unknown>),
      operations: { getHealthSummary: { expose_top_level: true } },
    };
    expect(declarativeGraphjinUpdate(pack, inputs, secrets)).toMatchObject({
      update_sources: [{ specs: { "service-health": { operations: {
        getHealthSummary: { expose_top_level: true },
      } } } }],
    });
  });

  it("exposes only operations declared by a governed pack action", () => {
    const pack = bundle();
    pack.artifacts[0]!.content = {
      ...(pack.artifacts[0]!.content as Record<string, unknown>),
      read_only: false,
      capabilities: { "api.read": true, "api.write": true, "api.delete": false },
    };
    (pack.artifacts[1]!.content as Record<string, unknown>).paths = {
      "/health-summary": {
        get: { operationId: "getHealthSummary" },
        put: { operationId: "updateHealth" },
      },
    };
    pack.artifacts.push({
      kind: "action",
      key: "action.change_health",
      targetRef: "fixture.change_health",
      path: "actions/change-health.yaml",
      hash: "fixture-action",
      content: {
        kind: "fixture.change_health",
        adapter: {
          kind: "graphjin_api_operation",
          source: "service_health",
          spec: "service-health",
          operations: {
            update: { operationId: "updateHealth", mutationRoot: "fixture_update_health" },
          },
        },
      },
    });
    expect(declarativeGraphjinUpdate(pack, inputs, secrets)).toMatchObject({
      roles: [{ name: "pack_api_executor" }],
      update_sources: [{
        specs: {
          "service-health": {
            operations: {
              updateHealth: {
                expose_mutation: true,
                allowed_roles: ["pack_api_executor"],
                expose_as: "fixture_update_health",
              },
            },
          },
        },
      }],
    });
  });

  it("rejects unsupported and unsafe declarations before mutation", () => {
    for (const change of [
      (pack: SolutionPackBundle) => { (pack.artifacts[0]!.content as Record<string, unknown>).read_only = false; },
      (pack: SolutionPackBundle) => { (pack.artifacts[0]!.content as Record<string, unknown>).capabilities = { "api.write": true }; },
      (pack: SolutionPackBundle) => { (pack.artifacts[0]!.content as Record<string, unknown>).capabilities = { "data.write": true }; },
      (pack: SolutionPackBundle) => { (pack.artifacts[0]!.content as Record<string, unknown>).specs_dir = "/arbitrary"; },
      (pack: SolutionPackBundle) => { (pack.artifacts[0]!.content as Record<string, unknown>).openapi = "https://example.test/spec"; },
      (pack: SolutionPackBundle) => { (pack.artifacts[0]!.content as Record<string, unknown>).auth = { type: "bearer", token: "plaintext" }; },
      (pack: SolutionPackBundle) => { (pack.artifacts[0]!.content as Record<string, unknown>).operations = { getHealthSummary: { expose_mutation: true } }; },
      (pack: SolutionPackBundle) => { pack.artifacts[3]!.content = "mutation { delete_records { id } }"; },
      (pack: SolutionPackBundle) => { pack.artifacts[1]!.content = { $ref: "file:///private" }; },
      (pack: SolutionPackBundle) => { pack.artifacts[3]!.kind = "action"; },
      (pack: SolutionPackBundle) => { pack.manifest.health.requiredPreflight = ["unknown-check"]; },
      (pack: SolutionPackBundle) => { pack.manifest.health.requiredPreflight = ["queries"]; pack.artifacts.pop(); },
      (pack: SolutionPackBundle) => { pack.artifacts[3]!.content = "query One { health { healthy } } query Two { health { healthy } }"; },
    ]) {
      const pack = bundle();
      change(pack);
      expect(() => declarativeGraphjinUpdate(pack, inputs, secrets)).toThrow();
    }
    expect(() => declarativeGraphjinUpdate(bundle(), inputs, {})).toThrow(/missing pack template/);

    const database = bundle();
    database.artifacts[0]!.content = {
      name: "service_health",
      kind: "database",
      type: "postgres",
      host: "db.example.test",
      dbname: "health",
      user: "reader",
      password: "{{secret.service.api_token}}",
      read_only: false,
    };
    expect(() => declarativeGraphjinUpdate(database, inputs, secrets)).toThrow(/database sources must be read-only/);
  });

  it("binds database query roots and named directives without rewriting literals or nested fields", () => {
    const pack = bundle();
    pack.artifacts[0]!.content = { name: "service_health", kind: "database" };
    pack.artifacts[3]!.content = '# mutation in a comment is harmless\nquery Health { ...Root } fragment Root on Query { alias: health(where: { label: { eq: "mutation" } }) { healthy details { id } } }';
    const bound = bindPackQueries(pack, { "source.health": "customer_db" });
    expect(bound.tables).toEqual([{ name: expect.stringMatching(/^pack_[a-f0-9]{20}$/), table: "health", source: "customer_db" }]);
    expect(String(bound.bundle.artifacts[3]!.content)).toContain('@database(name: "customer_db")');
    expect(String(bound.bundle.artifacts[3]!.content).match(/@database/g)).toHaveLength(1);
    expect(String(bound.bundle.artifacts[3]!.content)).toContain('eq: "mutation"');
    expect(String(pack.artifacts[3]!.content)).not.toContain("@database");
    pack.artifacts[3]!.content = '{ health @database(name: service_health) { healthy } }';
    expect(String(bindPackQueries(pack, { "source.health": "customer_db" }).bundle.artifacts[3]!.content)).toContain('@database(name: "customer_db")');
    for (const query of ['{ health @database(name: "wrong") { healthy } }', 'query ($db: String!) { health @database(name: $db) { healthy } }', 'query { ...Loop } fragment Loop on Query { ...Loop }']) {
      pack.artifacts[3]!.content = query;
      expect(() => bindPackQueries(pack, { "source.health": "customer_db" })).toThrow();
    }
    pack.artifacts.push({ ...pack.artifacts[0]!, key: "source.other", content: { name: "other", kind: "database" } });
    pack.artifacts[3]!.content = '{ health { healthy } }';
    expect(() => bindPackQueries(pack, { "source.health": "customer_db" })).toThrow(/select a declared database/);
    pack.artifacts[3]!.content = '{ a: health @database(name: service_health) { healthy } b: health @database(name: other) { healthy } }';
    const text = String(bindPackQueries(pack, { "source.health": "customer_db" }).bundle.artifacts[3]!.content);
    expect(text).toContain('@database(name: "customer_db")');
    expect(text).toContain('@database(name: "other")');
  });

  it("preserves template value types and fails on missing or interpolated values", () => {
    expect(packValue({ count: "{{count}}", enabled: "{{enabled}}" }, { count: 3, enabled: false })).toEqual({ count: 3, enabled: false });
    expect(() => packValue("prefix-{{missing}}", {})).toThrow();
    expect(() => packValue("{{missing}}", {})).toThrow();
  });

  it("disables only write policies on first install and preserves later admin choices", () => {
    expect(installedPackPolicyEnabled({ declared: true, controlsWrite: true })).toBe(false);
    expect(installedPackPolicyEnabled({ declared: true, controlsWrite: false })).toBe(true);
    expect(installedPackPolicyEnabled({ declared: false, controlsWrite: false })).toBe(false);
    expect(installedPackPolicyEnabled({ declared: false, controlsWrite: true, existing: true })).toBe(true);

    const pack = bundle();
    pack.artifacts.push({
      kind: "action",
      key: "action.change_health",
      targetRef: "fixture.change_health",
      path: "actions/change-health.yaml",
      hash: "fixture-action",
      content: {
        kind: "fixture.change_health",
        adapter: { kind: "graphjin_api_operation" },
      },
    });
    expect(packPolicyControlsWrite(pack, { appliesToKinds: ["fixture.change_health"] })).toBe(true);
    expect(packPolicyControlsWrite(pack, { appliesToKinds: ["fixture.read_health"] })).toBe(false);
    expect(nativeArtifactStateHash("policy", { name: "Fixture", enabled: false }))
      .toBe(nativeArtifactStateHash("policy", { name: "Fixture", enabled: true }));
  });
});
