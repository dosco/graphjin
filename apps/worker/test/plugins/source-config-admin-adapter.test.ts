// OL5 — the source_config_admin adapter configures the CUSTOMER GraphJin
// (roles, per-source access, source registration) via its admin-only
// gj_config two-phase preview→apply, once an admin approves the proposal.
// These tests pin the three guarantees: correct mutation shape against the
// customer engine, the hard boundary against the OpenNeko internal GraphJin,
// and that connection secrets stay value-blind to the agent/payload/audit.

import { createHash, randomUUID } from "node:crypto";
import { mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import {
  afterAll,
  afterEach,
  beforeAll,
  beforeEach,
  describe,
  expect,
  it,
  vi,
} from "vitest";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import {
  app_user,
  data_source,
  data_source_secret,
  db,
  eq,
  GRAPHJIN_CONFIG_SCOPE,
  llm_provider_config,
  openapi_spec_asset,
  pool,
  work_run,
  work_thread,
  setLocalAdministrator,
} from "@neko/db";
import {
  createActionRequest,
  executeApprovedActionRequest,
} from "@neko/llm/workflows";
import { maybeEncryptSecret } from "@neko/llm/secrets";
import {
  graphjinConfigPatchHash,
  managedFileSourceRuntimeRoot,
  managedOpenApiSpecFilename,
  stageManagedFileSourceFiles,
} from "@neko/llm/graphjin";
import { inProcessControlPlane } from "@neko/llm/work";
import { registerSourceConfigAdminAdapter } from "../../src/plugins/manage-adapters.js";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[source-config-admin-adapter] skipping: Postgres unreachable.");
}

const CUSTOMER = "http://customer-gj:8080/api/v1/graphql";
const INTERNAL = "http://127.0.0.1:8089";

/** Stub global fetch to emulate the customer GraphJin gj_config/gj_catalog. */
function installFetchMock(tableName = "api:pets:listPets") {
  const calls: {
    url: string;
    body: { query: string; variables?: Record<string, unknown> };
  }[] = [];
  const fn = vi.fn(async (url: string, init: { body: string }) => {
    const body = JSON.parse(init.body) as {
      query: string;
      variables?: Record<string, unknown>;
    };
    calls.push({ url, body });
    const q = body.query;
    let payload: unknown;
    if (q.includes('mode: "preview"')) {
      payload = {
        data: {
          gj_config: {
            valid: true,
            preview_id: "pv1",
            change_summary_json: '["changed"]',
            errors_json: "[]",
          },
        },
      };
    } else if (q.includes('mode: "apply"')) {
      payload = {
        data: { gj_config: { applied: true, catalog_revision: "rev2", errors_json: "null" } },
      };
    } else if (q.includes("gj_catalog")) {
      const kind = q.match(/kind: \{ eq: "(\w+)" \}/)?.[1] ?? "database";
      payload = {
        data: {
          gj_catalog:
            kind === "table"
              ? [{ id: tableName, name: tableName, summary: "List pets" }]
              : [{ id: `${kind}-1`, name: `${kind} one`, summary: "s" }],
        },
      };
    } else {
      payload = {
        data: {
          gj_config: {
            catalog_revision: "rev1",
            sources: [
              { name: "adventureworks", kind: "database", read_only: true },
              { name: "shop-api", kind: "api" },
            ],
          },
        },
      };
    }
    return { ok: true, json: async () => payload, text: async () => "" } as Response;
  });
  vi.stubGlobal("fetch", fn);
  return { calls };
}

describeIfDb("source_config_admin adapter (OL5)", () => {
  const orgId = uniqueOrgId("srccfg");
  const previousConfigFile = process.env.OPENNEKO_GRAPHJIN_CONFIG;
  let configRoot = "";
  let configFile = "";

  beforeAll(async () => {
    registerSourceConfigAdminAdapter();
    process.env.OPENNEKO_GRAPHJIN_URL = INTERNAL;
    configRoot = await mkdtemp(join(tmpdir(), "openneko-source-config-"));
    configFile = join(configRoot, "agentic.yml");
    process.env.OPENNEKO_GRAPHJIN_CONFIG = configFile;
    await createTestOrg(orgId);
    await db().insert(llm_provider_config).values({
      org_id: orgId,
      scope: GRAPHJIN_CONFIG_SCOPE,
      provider: "graphjin-config",
      enabled: true,
      config: { sourceConfigEnabled: true },
      secrets: {},
    });
    await db().insert(data_source).values({
      org_id: orgId,
      kind: "graphjin",
      graphql_url: CUSTOMER,
      name: "default",
      is_default: true,
    });
  });

  beforeEach(async () => {
    await writeFile(
      configFile,
      [
        "production: true",
        "system:",
        "  capabilities:",
        "    config.write: true",
        "sources:",
        "  - name: adventureworks",
        "    kind: database",
        "    access:",
        "      read: blocked",
        "      write: blocked",
        "      delete: blocked",
        "",
      ].join("\n"),
    );
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
    if (previousConfigFile === undefined) {
      delete process.env.OPENNEKO_GRAPHJIN_CONFIG;
    } else {
      process.env.OPENNEKO_GRAPHJIN_CONFIG = previousConfigFile;
    }
    await rm(configRoot, { recursive: true, force: true });
  });

  async function setEndpoint(url: string) {
    await db()
      .update(data_source)
      .set({ graphql_url: url })
      .where(eq(data_source.org_id, orgId));
  }

  async function setSourceConfigEnabled(enabled: boolean) {
    await db()
      .update(llm_provider_config)
      .set({
        enabled,
        config: { sourceConfigEnabled: enabled },
        updated_at: new Date(),
      })
      .where(eq(llm_provider_config.org_id, orgId));
  }

  async function runAction(payload: Record<string, unknown>) {
    const approvedPayload = {
      ...payload,
      preview: {
        patchHash: graphjinConfigPatchHash(payload),
        catalogRevision: "rev1",
      },
    };
    const request = await createActionRequest({
      orgId,
      scope: "internal",
      kind: "source_config_admin",
      target: String(payload.action ?? ""),
      payload: approvedPayload,
      riskLevel: "high",
      status: "approved",
      summary: "test",
      actorUserId: "admin-user",
      actorRole: "admin",
    });
    const result = await executeApprovedActionRequest(orgId, request.id);
    return { request, result };
  }

  it("refuses execution when the GraphJin config capability is disabled", async () => {
    await setEndpoint(CUSTOMER);
    await setSourceConfigEnabled(false);
    const { calls } = installFetchMock();
    const { result } = await runAction({
      action: "set_source_access",
      source: "adventureworks",
      read: "authenticated",
    });
    expect(result.ok).toBe(false);
    expect(result.error).toMatch(/capability is disabled/i);
    expect(calls).toHaveLength(0);
    await setSourceConfigEnabled(true);
  });

  it("control plane ignores a spoofed admin role when the bound run is not admin", async () => {
    await setSourceConfigEnabled(true);
    const threadId = randomUUID();
    const runId = randomUUID();
    await db().insert(work_thread).values({
      id: threadId,
      org_id: orgId,
      title: "member run",
    });
    await db().insert(work_run).values({
      id: runId,
      org_id: orgId,
      thread_id: threadId,
      backend: "ax",
      status: "running",
      actor_role: "member",
    });

    await expect(
      inProcessControlPlane.createActionRequest({
        orgId,
        scope: "internal",
        kind: "source_config_admin",
        target: "access:adventureworks",
        payload: { action: "set_source_access", source: "adventureworks" },
        riskLevel: "high",
        status: "pending_approval",
        summary: "spoof",
        workRunId: runId,
        actorRole: "admin",
      }),
    ).rejects.toThrow(/admin-only/i);
  });

  it("set_source_access runs the two-phase preview→apply with the right shape", async () => {
    await setEndpoint(CUSTOMER);
    const { calls } = installFetchMock();
    const { result } = await runAction({
      action: "set_source_access",
      source: "adventureworks",
      read: "authenticated",
      write: "blocked",
      delete: "blocked",
    });
    expect(result.ok, result.error).toBe(true);

    // rev read → preview → apply, all to the customer endpoint.
    expect(calls).toHaveLength(3);
    expect(calls.every((c) => c.url === CUSTOMER)).toBe(true);

    const preview = calls[1].body.query;
    expect(preview).toContain('mode: "preview"');
    expect(preview).toContain('expected_catalog_revision: "rev1"');
    expect(preview).toContain("source_patches");

    const apply = calls[2].body.query;
    expect(apply).toContain('mode: "apply"');
    expect(apply).toContain('preview_id: "pv1"');
    expect(apply).toContain("applied"); // selects `applied`, not `valid`
    const durableConfig = await readFile(configFile, "utf8");
    expect(durableConfig).toContain("name: adventureworks");
    expect(durableConfig).toContain("read: authenticated");
  });

  it("refuses to open a write path on a database source at approved execution", async () => {
    await setEndpoint(CUSTOMER);
    const { calls } = installFetchMock();
    const { result } = await runAction({
      action: "set_source_access",
      source: "adventureworks",
      write: "authenticated",
    });
    expect(result.ok).toBe(false);
    expect(result.error).toContain('database source "adventureworks" stays read-only');
    // The revision read happened; no preview or apply reached GraphJin.
    expect(calls.filter((c) => c.body.query.includes("mode:"))).toHaveLength(0);
  });

  it("expose_api_operation patches an API source through update_sources and persists it", async () => {
    await setEndpoint(CUSTOMER);
    await writeFile(
      configFile,
      "mode: agentic\nsources:\n  - name: shop-api\n    kind: api\n    specs_dir: /config/specs/shop\n",
    );
    const { calls } = installFetchMock();
    const { result } = await runAction({
      action: "expose_api_operation",
      source: "shop-api",
      spec: "shop",
      operation: "createOrder",
      exposeMutation: true,
      allowedRoles: ["admin"],
    });
    expect(result.ok, result.error).toBe(true);
    const preview = calls[1].body.query;
    expect(preview).toContain("update_sources");
    expect(preview).toContain("expose_mutation: true");
    const durableConfig = await readFile(configFile, "utf8");
    expect(durableConfig).toContain("createOrder:");
    expect(durableConfig).toContain("expose_mutation: true");
    expect(durableConfig).toContain("- admin");
  });

  it("register_source uses update_sources (upsert), never sources: (replace)", async () => {
    await setEndpoint(CUSTOMER);
    const { calls } = installFetchMock();
    const { result } = await runAction({
      action: "register_source",
      name: "warehouse",
      kind: "database",
      host: "wh-db",
      port: 5432,
      dbname: "warehouse",
      user: "reader",
    });
    expect(result.ok, result.error).toBe(true);
    const preview = calls[1].body.query;
    expect(preview).toContain("update_sources");
    // Must not wipe the top-level system config via a full source replace.
    expect(preview).not.toContain("source_add");
    expect(preview).not.toMatch(/[^_]sources:/);
    expect(await readFile(configFile, "utf8")).toContain("name: warehouse");
  });

  it("materializes, applies, and verifies a managed OpenAPI asset", async () => {
    await setEndpoint(CUSTOMER);
    const root = await mkdtemp(join(tmpdir(), "openneko-openapi-"));
    const previousConfig = process.env.OPENNEKO_GRAPHJIN_CONFIG;
    process.env.OPENNEKO_GRAPHJIN_CONFIG = join(root, "agentic.yml");
    await writeFile(
      process.env.OPENNEKO_GRAPHJIN_CONFIG,
      "production: true\nsystem:\n  capabilities:\n    config.write: true\nsources: []\n",
    );
    const content = [
      "openapi: 3.0.3",
      "info:",
      "  title: Pets",
      "  version: '1'",
      "servers:",
      "  - url: https://pets.example.com",
      "paths:",
      "  /pets:",
      "    get:",
      "      responses:",
      "        '200': { description: ok }",
      "",
    ].join("\n");
    const [asset] = await db()
      .insert(openapi_spec_asset)
      .values({
        org_id: orgId,
        source_type: "upload",
        original_name: "pets.yaml",
        content,
        checksum_sha256: createHash("sha256").update(content).digest("hex"),
        title: "Pets",
        version: "1",
        base_url: "https://pets.example.com",
        operation_count: 1,
        read_operation_count: 1,
      })
      .returning();
    const tableName = managedOpenApiSpecFilename(orgId, "pets").replace(
      /\.yaml$/,
      "_list_pets",
    );
    const { calls } = installFetchMock(tableName);
    try {
      const { result } = await runAction({
        action: "register_source",
        name: "pets",
        kind: "api",
        specAssetId: asset!.id,
      });
      expect(result.ok, result.error).toBe(true);
      expect(
        calls.some((call) => call.body.query.includes('kind: "api"')),
      ).toBe(true);
      expect(
        calls.some((call) =>
          call.body.query.includes('specs_dir: "/config/specs"'),
        ),
      ).toBe(true);
      const specPath = join(
        root,
        "specs",
        managedOpenApiSpecFilename(orgId, "pets"),
      );
      expect(await readFile(specPath, "utf8")).toBe(content);
      expect(
        await readFile(process.env.OPENNEKO_GRAPHJIN_CONFIG, "utf8"),
      ).toContain("name: pets");
      const [stored] = await db()
        .select({
          status: openapi_spec_asset.status,
          sourceName: openapi_spec_asset.source_name,
        })
        .from(openapi_spec_asset)
        .where(eq(openapi_spec_asset.id, asset!.id));
      expect(stored).toEqual({ status: "active", sourceName: "pets" });
    } finally {
      if (previousConfig === undefined) {
        delete process.env.OPENNEKO_GRAPHJIN_CONFIG;
      }
      else process.env.OPENNEKO_GRAPHJIN_CONFIG = previousConfig;
      await rm(root, { recursive: true, force: true });
    }
  });

  it("verifies, applies, and persists an isolated read-only local file source", async () => {
    await setEndpoint(CUSTOMER);
    const staged = await stageManagedFileSourceFiles({
      configFile,
      orgId,
      sourceName: "documents",
      files: [
        {
          name: "revenue.csv",
          content: new TextEncoder().encode("quarter,revenue\nQ1,42\n"),
          contentType: "text/csv",
        },
      ],
    });
    const localFiles = {
      sourceName: "documents",
      ready: true as const,
      files: staged,
      totalSize: staged.reduce((sum, file) => sum + file.size, 0),
    };
    const { calls } = installFetchMock("documents");
    const { result } = await runAction({
      action: "register_source",
      name: "documents",
      kind: "file",
      backend: "local",
      localFiles,
    });
    expect(result.ok, result.error).toBe(true);
    const preview = calls.find((call) =>
      call.body.query.includes('mode: "preview"'),
    )!.body.query;
    const runtimeRoot = managedFileSourceRuntimeRoot({
      orgId,
      sourceName: "documents",
    });
    expect(preview).toContain(runtimeRoot);
    expect(preview).toContain("read_only: true");
    expect(preview).toContain('read: "authenticated"');
    const previewVariables = calls.find((call) =>
      call.body.query.includes('mode: "preview"'),
    )!.body.variables;
    expect(previewVariables).toMatchObject({
      json1: {
        "files.list": true,
        "files.read": true,
        "files.write": false,
        "files.delete": false,
        "files.watch": false,
      },
    });
    expect(
      calls.some(
        (call) =>
          call.body.query.includes("gj_catalog") &&
          call.body.query.includes('kind: { eq: "table" }'),
      ),
    ).toBe(true);
    expect(result.outcome?.result).toMatchObject({
      fileSource: {
        backend: "local",
        fileCount: 1,
        totalSize: staged[0]!.size,
        readOnly: true,
      },
    });
    const durableConfig = await readFile(configFile, "utf8");
    expect(durableConfig).toContain("name: documents");
    expect(durableConfig).toContain(`root: ${runtimeRoot}`);
    expect(durableConfig).toContain("read_only: true");
    expect(durableConfig).not.toContain(configRoot);
  });

  it("BOUNDARY: refuses to configure the OpenNeko internal GraphJin", async () => {
    await setEndpoint(`${INTERNAL}/api/v1/graphql`);
    const { calls } = installFetchMock();
    const { result } = await runAction({
      action: "set_source_access",
      source: "adventureworks",
      read: "authenticated",
    });
    expect(result.ok).toBe(false);
    expect(result.error).toMatch(/internal GraphJin/i);
    // Nothing was applied — no request ever reached an engine.
    expect(calls).toHaveLength(0);
  });

  it("SECRET-BLIND: the value reaches the gj_config POST only, never the payload/result", async () => {
    await setEndpoint(CUSTOMER);
    const SECRET_NAME = "WAREHOUSE_DB_PASSWORD";
    const SECRET_VALUE = "s3cr3t-pw-xyz";
    await db()
      .insert(data_source_secret)
      .values({
        org_id: orgId,
        name: SECRET_NAME,
        value_enc: maybeEncryptSecret(SECRET_VALUE),
        updated_at: new Date(),
      })
      .onConflictDoNothing();

    const { calls } = installFetchMock();
    const { request, result } = await runAction({
      action: "register_source",
      name: "warehouse2",
      kind: "database",
      host: "wh-db",
      port: 5432,
      dbname: "warehouse",
      user: "reader",
      secretRef: SECRET_NAME,
    });
    expect(result.ok, result.error).toBe(true);

    // The resolved value IS injected into the apply POST body (worker-side).
    const applyBody = calls.find((c) => c.body.query.includes('mode: "apply"'))!;
    expect(applyBody.body.query).toContain(SECRET_VALUE);

    // …but it is NOWHERE in the action payload (what the agent proposed)…
    expect(JSON.stringify(request.payload)).not.toContain(SECRET_VALUE);
    expect(JSON.stringify(request.payload)).toContain(SECRET_NAME);
    // …nor in the adapter result (which carries the NAME only).
    expect(JSON.stringify(result)).not.toContain(SECRET_VALUE);
    expect(JSON.stringify(result)).toContain(SECRET_NAME);
    const durableConfig = await readFile(configFile, "utf8");
    expect(durableConfig).not.toContain(SECRET_VALUE);
    expect(durableConfig).toContain(
      "password: gjsecret://sources/warehouse2/password",
    );
  });

  it("describeSourceGraph summarizes gj_catalog rows for the customer engine", async () => {
    await setEndpoint(CUSTOMER);
    installFetchMock();
    const graph = await inProcessControlPlane.describeSourceGraph({
      orgId,
      actorRole: "admin",
    });
    expect(graph.reachable).toBe(true);
    expect(graph.host).toBe("customer-gj");
    expect(graph.databases?.[0]?.id).toBe("database-1");
    expect(graph.capabilities?.[0]?.id).toBe("capability-1");
    expect(graph.namespaces?.[0]?.id).toBe("namespace-1");
  });

  it("server agent is admin-only, bearer-authenticated, and rechecks live role", async () => {
    await setEndpoint(CUSTOMER);
    const userId = `admin-${randomUUID()}`;
    const threadId = randomUUID();
    const runId = randomUUID();
    await db().insert(app_user).values({
      id: userId,
      org_id: orgId,
      email: `${userId}@example.test`,
    });
    await setLocalAdministrator(orgId, userId, true);
    await db().insert(work_thread).values({
      id: threadId,
      org_id: orgId,
      title: "private admin config",
      created_by_user_id: userId,
    });
    await db().insert(work_run).values({
      id: runId,
      org_id: orgId,
      thread_id: threadId,
      backend: "ax",
      status: "running",
      actor_user_id: userId,
      actor_role: "admin",
    });

    const calls: Array<{ url: string; headers: Headers; body?: unknown }> = [];
    vi.stubGlobal(
      "fetch",
      vi.fn(async (url: string, init: RequestInit = {}) => {
        const headers = new Headers(init.headers);
        calls.push({
          url,
          headers,
          body: typeof init.body === "string" ? JSON.parse(init.body) : undefined,
        });
        if (url.endsWith("/api/v1/agent/status")) {
          return new Response(
            JSON.stringify({
              status: "ready",
              enabled: true,
              ready: true,
              endpoint: "/api/v1/agent",
              provider: "openai",
              api_key_configured: true,
              max_steps: 8,
              timeout_seconds: 50,
              read_only: true,
              return_trace: false,
              message: "ready",
            }),
            { status: 200 },
          );
        }
        return new Response(
          JSON.stringify({
            status: "answered",
            answer: "The redacted configuration is healthy.",
            trace_id: "gj-trace-1",
          }),
          { status: 200 },
        );
      }),
    );

    const answer = await inProcessControlPlane.askSourceConfigAgent({
      orgId,
      runId,
      instruction: "Explain the current redacted configuration.",
    });
    expect(answer.response?.status).toBe("answered");
    expect(answer.agentStatus?.read_only).toBe(true);
    expect(calls).toHaveLength(2);
    expect(calls.every((call) => call.headers.has("authorization"))).toBe(true);
    expect(calls.every((call) => !call.headers.has("x-role"))).toBe(true);

    // The guard keeps one administrator, so the org gets a second before the demotion.
    const keeperId = `keeper-${randomUUID()}`;
    await db().insert(app_user).values({ id: keeperId, org_id: orgId, email: `${keeperId}@example.test` });
    await setLocalAdministrator(orgId, keeperId, true);
    await setLocalAdministrator(orgId, userId, false);
    calls.length = 0;
    const denied = await inProcessControlPlane.askSourceConfigAgent({
      orgId,
      runId,
      instruction: "Show configuration.",
    });
    expect(denied.denied).toBe(true);
    expect(denied.error).toMatch(/admin-only/i);
    expect(calls).toHaveLength(0);
  });
});
