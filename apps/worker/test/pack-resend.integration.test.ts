import { execFile, spawn, type ChildProcess } from "node:child_process";
import { randomBytes, randomUUID } from "node:crypto";
import { createServer, type IncomingMessage } from "node:http";
import { cp, mkdir, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { promisify } from "node:util";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import pg from "pg";
import { parse, stringify } from "yaml";
import { pool } from "@neko/db";
import { graphjinQuery, graphjinSigningSecretB64, mintGraphjinToken } from "@neko/llm/graphjin";
import { withActionTargets, type ActionRequestRecord } from "@neko/llm/workflows";
import { declarativePackActionAdapter } from "../src/packs/declarative-action-runtime.js";
import { PackService } from "../src/packs/service.js";

// The Resend pack, end to end: PackService installs it into a real GraphJin
// (a host binary, no Docker), and an approved resend.email action runs through
// the declarative runtime to a fake Resend HTTP server. Only the workspace
// location is substituted.
//
// Run with OPENNEKO_RESEND_PACK_E2E=1 and the NEKO_PG_* settings for a migrated
// OpenNeko database. OPENNEKO_GRAPHJIN_BIN names a GraphJin binary; without it
// the test builds one from graphjin/.
const workspace = vi.hoisted(() => ({ root: "" }));
vi.mock("@neko/db/jobs", () => ({ enqueue: async () => "fixture-job", QUEUE: { METRIC_REFRESH: "metric_refresh" } }));
vi.mock("@neko/llm/work", () => ({ ensureOrgWorkspace: async () => ({ orgRoot: workspace.root, skillsRoot: join(workspace.root, "skills") }) }));

const exec = promisify(execFile);
const enabled = process.env.OPENNEKO_RESEND_PACK_E2E === "1";

type Received = { method: string; url: string; headers: IncomingMessage["headers"]; body: unknown };

describe.skipIf(!enabled)("Resend pack through GraphJin", () => {
  const org = `resend-pack-${Date.now()}`;
  const apiKey = `re_${randomBytes(18).toString("hex")}`;
  const sender = "Acme Ops <ops@mail.acme.test>";
  const received: Received[] = [];
  let root = "";
  let endpoint = "";
  let sourceId = "";
  let graphjin: ChildProcess | undefined;
  let graphjinLog = "";
  let service: PackService;

  const resend = createServer((request, response) => {
    let raw = "";
    request.on("data", (chunk) => { raw += chunk; });
    request.on("end", () => {
      received.push({ method: request.method ?? "", url: request.url ?? "", headers: request.headers, body: raw ? JSON.parse(raw) : null });
      response.setHeader("content-type", "application/json");
      if (request.headers.authorization !== `Bearer ${apiKey}`) {
        response.writeHead(401).end(JSON.stringify({ message: "API key is invalid" }));
        return;
      }
      if (request.method === "POST" && request.url === "/emails") {
        response.end(JSON.stringify({ id: "4ef9a417-02e9-4d39-ad75-9611e0fcc33c" }));
      } else if (request.method === "GET" && request.url === "/emails/4ef9a417-02e9-4d39-ad75-9611e0fcc33c") {
        response.end(JSON.stringify({
          object: "email", id: "4ef9a417-02e9-4d39-ad75-9611e0fcc33c", to: ["ops@acme.test"],
          from: sender, subject: "Low stock", created_at: "2026-10-19 12:00:00.000000+00", last_event: "delivered",
        }));
      } else if (request.method === "GET" && new URL(request.url ?? "/", "http://resend").pathname === "/templates") {
        response.end(JSON.stringify({ object: "list", has_more: false, data: [{ id: "tpl_1", alias: "daily-ops-summary", name: "Daily ops", status: "published" }] }));
      } else {
        response.writeHead(404).end(JSON.stringify({ message: "not found" }));
      }
    });
  });

  const query = (document: string, variables?: Record<string, unknown>) => graphjinQuery({
    baseUrl: endpoint, query: document, ...(variables ? { variables } : {}),
    headers: { authorization: `Bearer ${mintGraphjinToken({ orgId: org, userId: "fixture", role: "service" })}` },
    signal: AbortSignal.timeout(10_000),
  });

  function actionRequest(body: Record<string, unknown>): ActionRequestRecord {
    return {
      id: randomUUID(), orgId: org, actorUserId: "fixture", actorRole: "admin", actorBackend: null,
      workflowRunId: null, triggeredByObservationId: null, policyId: null, scope: "external",
      kind: "resend.email", target: null, payload: { operation: "send_email", body },
      riskLevel: null, status: "approved", summary: null,
    } as unknown as ActionRequestRecord;
  }

  beforeAll(async () => {
    root = await mkdtemp(join(tmpdir(), "pack-resend-"));
    workspace.root = join(root, "workspace");
    await mkdir(join(workspace.root, "skills"), { recursive: true });
    vi.stubEnv("XDG_CONFIG_HOME", join(root, "secrets"));
    const configRoot = join(root, "graphjin");
    await mkdir(configRoot);
    // No shared config folder: the pack reaches GraphJin through gj_config,
    // as it does for a GraphJin that the customer runs.
    vi.stubEnv("OPENNEKO_GRAPHJIN_CONFIG", "");

    await new Promise<void>((done) => resend.listen(0, "127.0.0.1", done));
    const resendUrl = `http://127.0.0.1:${(resend.address() as { port: number }).port}`;
    // The shipped pack pins api.resend.com. This copy points the same source at
    // the fake server and changes nothing else.
    await cp(resolve("../../packs/resend"), join(root, "packs/resend"), { recursive: true });
    const sourcesFile = join(root, "packs/resend/graphjin/sources.yaml");
    const sources = parse(await readFile(sourcesFile, "utf8"));
    expect(sources.sources[0].base_url).toBe("https://api.resend.com");
    sources.sources[0].base_url = resendUrl;
    await writeFile(sourcesFile, stringify(sources));

    // GraphJin needs a default database source; give it a one-row table that
    // also answers the readiness probe.
    const database = `resend_pack_e2e`;
    if (!(await pool().query("select 1 from pg_database where datname=$1", [database])).rowCount) {
      await pool().query(`create database ${database}`);
    }
    const seed = new pg.Client({
      host: process.env.NEKO_PG_HOST, port: Number(process.env.NEKO_PG_PORT ?? 5432),
      user: process.env.NEKO_PG_USER, password: process.env.NEKO_PG_PASSWORD, database,
    });
    await seed.connect();
    await seed.query("create table if not exists seed(id integer primary key); insert into seed values (1) on conflict do nothing");
    await seed.end();
    const reservation = createServer();
    await new Promise<void>((done) => reservation.listen(0, "127.0.0.1", done));
    const port = (reservation.address() as { port: number }).port;
    await new Promise<void>((done) => reservation.close(() => done()));
    const config = parse(await readFile(resolve("../../db/graphjin/dev.sources.example.yml"), "utf8"));
    config.host_port = `127.0.0.1:${port}`;
    config.production = false;
    // gj_config applies changes live. The file watcher would also re-exec
    // GraphJin when it saves them, dropping connections mid-test.
    config.reload_on_config_change = false;
    config.auth.jwt.secret = graphjinSigningSecretB64(org);
    config.secrets.keystore.key = randomBytes(32).toString("base64");
    delete config.agent;
    config.sources = [{
      name: "fixture", kind: "database", default: true, read_only: true, type: "postgres",
      host: process.env.NEKO_PG_HOST ?? "127.0.0.1", port: Number(process.env.NEKO_PG_PORT ?? 5432), dbname: database,
      user: process.env.NEKO_PG_USER ?? "neko", password: process.env.NEKO_PG_PASSWORD ?? "",
      access: { read: "authenticated", write: "blocked", delete: "blocked" },
    }];
    await writeFile(join(configRoot, "agentic.yml"), stringify(config));

    let binary = process.env.OPENNEKO_GRAPHJIN_BIN ?? "";
    if (!binary) {
      binary = join(root, "graphjin-bin");
      await exec("go", ["build", "-o", binary, "./cmd"], { cwd: resolve("../../graphjin"), maxBuffer: 16 * 1024 * 1024 });
    }
    graphjin = spawn(binary, ["serve", "--path", configRoot], { env: { ...process.env, GO_ENV: "agentic" }, stdio: ["ignore", "pipe", "pipe"] });
    graphjin.stdout?.on("data", (chunk) => { graphjinLog += chunk; });
    graphjin.stderr?.on("data", (chunk) => { graphjinLog += chunk; });
    endpoint = `http://127.0.0.1:${port}/api/v1/graphql`;
    const deadline = Date.now() + 60_000;
    for (;;) {
      try {
        const response = await query("query Ready { seed { id } }");
        if (response.data) break;
      } catch { /* not listening yet */ }
      if (Date.now() > deadline || graphjin.exitCode !== null) throw new Error(`GraphJin did not start:\n${graphjinLog.slice(-4000)}`);
      await new Promise((done) => setTimeout(done, 250));
    }

    await pool().query("insert into organization(id,name) values ($1,'Resend pack fixture')", [org]);
    sourceId = (await pool().query("insert into data_source(org_id,kind,graphql_url,auth_mode) values ($1,'graphjin',$2,'jwt') returning id", [org, endpoint])).rows[0].id;
    service = new PackService(org, join(root, "packs"));
  }, 600_000);

  afterAll(async () => {
    graphjin?.kill("SIGTERM");
    if (process.env.OPENNEKO_RESEND_PACK_E2E_LOG) await writeFile(process.env.OPENNEKO_RESEND_PACK_E2E_LOG, graphjinLog.replaceAll(apiKey, "[REDACTED]"));
    resend.close();
    await pool().query("delete from organization where id=$1", [org]).catch(() => {});
    await pool().end();
    vi.unstubAllEnvs();
    if (root) await rm(root, { recursive: true, force: true });
  });

  it("installs with a sealed API key and serves the Resend reads", async () => {
    const result = await service.install("resend", {
      dataSourceId: sourceId,
      inputs: { "resend.from": sender },
      secrets: { "resend.api_key": apiKey },
    });
    expect(result.status, graphjinLog.slice(-4000)).toBe("installed");
    const config = await readFile(join(root, "graphjin/agentic.yml"), "utf8");
    expect(config).not.toContain(apiKey);
    expect(config).toContain("gjsecret://");
    const persisted = await pool().query("select config from pack_install where org_id=$1", [org]);
    expect(JSON.stringify(persisted.rows)).not.toContain(apiKey);

    const templates = await query("query { resend_list_templates(limit: 5) { id alias name status } }");
    expect(templates.errors).toBeUndefined();
    expect(templates.data).toEqual({
      resend_list_templates: [{ id: "tpl_1", alias: "daily-ops-summary", name: "Daily ops", status: "published" }],
    });
    // GraphJin takes remote-table arguments as literals, not variables.
    const status = await query('query { resend_get_email(email_id: "4ef9a417-02e9-4d39-ad75-9611e0fcc33c") { id to subject created_at last_event } }');
    expect(status.errors).toBeUndefined();
    expect(status.data).toMatchObject({ resend_get_email: { last_event: "delivered", to: ["ops@acme.test"] } });
    expect(received.every((request) => request.method === "GET")).toBe(true);

    // A read-scoped token cannot reach the send mutation.
    const blocked = await query("mutation { resend_send_email(call: {body: {}}) { ok } }");
    expect(blocked.errors?.length).toBeGreaterThan(0);
    expect(received.some((request) => request.method === "POST")).toBe(false);
  }, 120_000);

  it("derives recipient domains for the policy engine from the installed action", async () => {
    const subject = await withActionTargets(org, { scope: "external", kind: "resend.email", target: "agent text" }, {
      operation: "send_email",
      body: { to: ["a@Acme.test", "Buyer <buyer@partner.test>"], cc: ["c@acme.test"], bcc: ["x@sub.acme.test"] },
    });
    expect(subject.targets).toEqual(["acme.test", "partner.test", "sub.acme.test"]);
    expect(subject.target).toBe("acme.test, partner.test, sub.acme.test");
  });

  it("sends from the locked sender with the request id as the idempotency key", async () => {
    received.length = 0;
    const request = actionRequest({
      from: "Attacker <ceo@evil.test>",
      to: ["ops@acme.test"],
      cc: ["Buyer <buyer@acme.test>"],
      subject: "Low stock: 3 SKUs below reorder point",
      html: "<p>WB-120: 4 on hand</p>",
      text: "WB-120: 4 on hand",
    });
    const outcome = await declarativePackActionAdapter({ request });
    expect(outcome).toMatchObject({
      commandOrOperation: "sendEmail",
      result: { operation: "send_email", statusCode: 200, response: { id: "4ef9a417-02e9-4d39-ad75-9611e0fcc33c" } },
    });
    expect(received).toHaveLength(1);
    const [sent] = received;
    expect(sent).toMatchObject({ method: "POST", url: "/emails" });
    expect(sent!.headers.authorization).toBe(`Bearer ${apiKey}`);
    expect(sent!.headers["idempotency-key"]).toBe(request.id);
    expect(sent!.body).toEqual({
      from: sender,
      to: ["ops@acme.test"],
      cc: ["Buyer <buyer@acme.test>"],
      subject: "Low stock: 3 SKUs below reorder point",
      html: "<p>WB-120: 4 on hand</p>",
      text: "WB-120: 4 on hand",
    });
  }, 60_000);

  it("sends a Resend template without a subject", async () => {
    received.length = 0;
    const request = actionRequest({
      to: ["ops@acme.test"],
      template: { id: "daily-ops-summary", variables: { count: 3, region: "EU" } },
    });
    await expect(declarativePackActionAdapter({ request })).resolves.toMatchObject({ result: { statusCode: 200 } });
    expect(received.map((value) => value.body)).toEqual([{
      from: sender,
      to: ["ops@acme.test"],
      template: { id: "daily-ops-summary", variables: { count: 3, region: "EU" } },
    }]);
  }, 60_000);

  it("refuses a malformed email before it reaches Resend", async () => {
    received.length = 0;
    for (const body of [
      // Two recipients in one item would hide the second domain from policy.
      { to: ["ops@acme.test, ceo@evil.test"], subject: "Hi", text: "Hi" },
      // No body.
      { to: ["ops@acme.test"], subject: "Hi" },
      // A template and HTML together.
      { to: ["ops@acme.test"], template: { id: "daily-ops-summary" }, html: "<p>x</p>" },
      // A field the reviewed spec does not declare.
      { to: ["ops@acme.test"], subject: "Hi", text: "Hi", headers: { "X-Spoof": "1" } },
    ]) {
      await expect(declarativePackActionAdapter({ request: actionRequest(body) })).rejects.toThrow();
    }
    expect(received).toHaveLength(0);
  }, 60_000);
});
