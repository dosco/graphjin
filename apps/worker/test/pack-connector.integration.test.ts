import { execFile } from "node:child_process";
import { randomBytes } from "node:crypto";
import { createServer } from "node:http";
import { cp, mkdir, mkdtemp, readFile, readdir, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { promisify } from "node:util";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { parse, stringify } from "yaml";
import { localConfigPath, pool } from "@neko/db";
import { dirname } from "node:path";
import { packZipEntries, zipFixture } from "../../../packages/packs/test/zip-fixture.js";
import { graphjinQuery, graphjinSigningSecretB64, mintGraphjinToken } from "@neko/llm/graphjin";
import { applyPackGraphjinConfig } from "../src/packs/graphjin-config.js";
import { PackService } from "../src/packs/service.js";
import { sweepWatchers } from "@neko/llm/workflows";
import { runMetricRefresh } from "../src/jobs/metric-refresh.js";
import { createAdminHandler } from "../src/admin-server.js";

// Only the scheduler and workspace location are substituted. Connector HTTP,
// GraphJin preview/apply/restart, credentials, and metadata use the real code.
const workspace = vi.hoisted(() => ({ root: "" }));
vi.mock("@neko/db/jobs", () => ({ enqueue: async () => "fixture-job", QUEUE: { METRIC_REFRESH: "metric_refresh" } }));
vi.mock("@neko/llm/work", () => ({ ensureOrgWorkspace: async () => ({ orgRoot: workspace.root, skillsRoot: join(workspace.root, "skills") }) }));

// A connector accidentally entering either plugin lifecycle must fail this test.
vi.mock("@open-neko/plugin-install", () => { throw new Error("pack connectors must not load the plugin installer"); });
vi.mock("../src/plugins/plugin-registry.js", () => { throw new Error("pack connectors must not load the plugin registry"); });

const exec = promisify(execFile);
const docker = (...args: string[]) => exec("docker", args, { maxBuffer: 1024 * 1024 });
const enabled = process.env.OPENNEKO_PACK_CONNECTOR_TEST === "1";
describe.skipIf(!enabled)("pack-owned connector on packaged GraphJin", () => {
  const org = `connector-test-${Date.now()}`;
  const database = `${org}-db`;
  const graphjin = `${org}-graphjin`;
  const token = randomBytes(24).toString("hex");
  let root = "";
  let endpoint = "";
  let sourceId = "";
  let service: PackService;
  let reads = 0;
  let writes = 0;
  let authorizedReads = 0;
  let health: number | null = 1;
  const provider = createServer((request, response) => {
    if (request.method !== "GET") writes++;
    else reads++;
    if (request.headers.authorization !== `Bearer ${token}`) {
      response.writeHead(401).end(); return;
    }
    authorizedReads++;
    response.setHeader("content-type", "application/json");
    response.end(JSON.stringify({ healthy: health, checked_at: new Date().toISOString() }));
  });
  const query = (document: string) => graphjinQuery({
    baseUrl: endpoint, query: document,
    headers: { authorization: `Bearer ${mintGraphjinToken({ orgId: org, userId: "fixture", role: "service" })}` },
    signal: AbortSignal.timeout(10_000),
  });
  async function ready() {
    const deadline = Date.now() + 45_000;
    let last = "";
    while (Date.now() < deadline) {
      try { const response = await query("query ConnectorReady { seed { id } }"); if (response.data) return; last = JSON.stringify(response.errors); } catch (error) { last = String(error); }
      await new Promise(resolve => setTimeout(resolve, 250));
    }
    throw new Error(`GraphJin did not become ready: ${last}`);
  }
  beforeAll(async () => {
    root = await mkdtemp(join(tmpdir(), "pack-connector-"));
    workspace.root = join(root, "workspace");
    await mkdir(join(workspace.root, "skills"), { recursive: true });
    vi.stubEnv("XDG_CONFIG_HOME", join(root, "secrets"));
    const configRoot = join(root, "graphjin");
    await mkdir(configRoot);
    vi.stubEnv("OPENNEKO_GRAPHJIN_CONFIG", join(configRoot, "agentic.yml"));
    await cp(resolve("test/fixtures/service-health"), join(root, "packs/service-health"), { recursive: true });
    for (const kind of ["actions", "policies"]) await mkdir(join(root, "packs/service-health", kind), { recursive: true });
    await docker("run", "-d", "--name", database, "-e", "POSTGRES_USER=neko", "-e", "POSTGRES_DB=neko", "-e", "POSTGRES_HOST_AUTH_METHOD=trust", "-p", "127.0.0.1::5432", "-v", `${resolve("../../db/migrations")}:/migrations:ro`, "pgvector/pgvector:pg16");
    const port = (await docker("port", database, "5432/tcp")).stdout.trim().split(":").at(-1)!;
    for (const [key, value] of Object.entries({ OPENNEKO_PG_ENV_OVERRIDE: "1", NEKO_PG_HOST: "127.0.0.1", NEKO_PG_PORT: port, NEKO_PG_USER: "neko", NEKO_PG_DATABASE: "neko" })) vi.stubEnv(key, value);
    for (let attempt = 0; ; attempt++) {
      try { await docker("exec", database, "psql", "-h", "127.0.0.1", "-U", "neko", "-d", "neko", "-c", "SELECT 1"); break; }
      catch (error) { if (attempt === 40) throw error; await new Promise(resolve => setTimeout(resolve, 250)); }
    }
    for (const file of (await readdir(resolve("../../db/migrations"))).filter(file => file.endsWith(".sql")).sort()) {
      await docker("exec", database, "psql", "-U", "neko", "-d", "neko", "-v", "ON_ERROR_STOP=1", "-f", `/migrations/${file}`);
    }
    await docker("exec", database, "psql", "-U", "neko", "-d", "neko", "-c", "CREATE DATABASE connector");
    await docker("exec", database, "psql", "-U", "neko", "-d", "connector", "-c", "CREATE TABLE seed(id integer primary key); INSERT INTO seed VALUES (1)");
    const config = parse(await readFile(resolve("../../db/graphjin/dev.sources.example.yml"), "utf8"));
    config.auth.jwt.secret = graphjinSigningSecretB64(org);
    config.secrets.keystore.key = randomBytes(32).toString("base64");
    delete config.agent;
    config.sources = [{ name: "fixture", kind: "database", default: true, read_only: true, type: "postgres", host: "host.docker.internal", port: Number(port), dbname: "connector", user: "neko", access: { read: "authenticated", write: "blocked", delete: "blocked" } }];
    await writeFile(join(configRoot, "agentic.yml"), stringify(config));
    const image = (await docker("image", "inspect", process.env.OPENNEKO_CONNECTOR_TEST_IMAGE ?? "ghcr.io/open-neko/records-graphjin:latest", "--format", "{{.Id}}")).stdout.trim();
    expect((await docker("run", "--rm", "--entrypoint", "graphjin", image, "version")).stdout).toMatch(/^GraphJin \S+/);
    const reservation = createServer();
    await new Promise<void>(resolve => reservation.listen(0, "127.0.0.1", resolve));
    const reservedPort = (reservation.address() as { port: number }).port;
    await new Promise<void>(resolve => reservation.close(() => resolve()));
    await docker("run", "-d", "--name", graphjin, "--user", "0", "-e", "GO_ENV=agentic", "-p", `127.0.0.1:${reservedPort}:8080`, "-v", `${configRoot}:/config`, "-v", `${resolve("../../scripts/graphjin-supervisor.sh")}:/supervisor.sh:ro`, "--entrypoint", "/bin/sh", image, "/supervisor.sh", "serve", "--path", "/config");
    const graphjinPort = (await docker("port", graphjin, "8080/tcp")).stdout.trim().split(":").at(-1)!;
    endpoint = `http://127.0.0.1:${graphjinPort}/api/v1/graphql`;
    await ready();
    await new Promise<void>(resolve => provider.listen(0, "0.0.0.0", resolve));
    await pool().query("insert into organization(id,name) values ($1,'Connector fixture')", [org]);
    sourceId = (await pool().query("insert into data_source(org_id,kind,graphql_url,auth_mode) values ($1,'graphjin',$2,'jwt') returning id", [org, endpoint])).rows[0].id;
    service = new PackService(org, join(root, "packs"));
  }, 120_000);
  afterAll(async () => {
    const logs = await docker("logs", "--tail", "100", graphjin).catch(() => ({ stdout: "", stderr: "" }));
    await writeFile("/tmp/issue290-connector-graphjin.log", (logs.stdout + logs.stderr).replaceAll(token, "[REDACTED]"));
    await pool().end();
    await Promise.all([docker("rm", "-f", "-v", graphjin).catch(() => {}), docker("rm", "-f", "-v", database).catch(() => {})]);
    provider.close();
    vi.unstubAllEnvs();
    if (root) await rm(root, { recursive: true, force: true });
  });
  it("installs its bundled authenticated connector, survives restart, and blocks writes", async () => {
    const port = (provider.address() as { port: number }).port;
    const request = { dataSourceId: sourceId, inputs: { "service.base_url": `http://host.docker.internal:${port}` }, secrets: { "service.api_token": token } };
    await expect(service.install("service-health", { ...request, secrets: {} })).rejects.toThrow(/secret|required/);
    expect(reads).toBe(0);
    const specPath = join(root, "packs/service-health/graphjin/specs/service-health.yaml");
    const originalSpec = await readFile(specPath, "utf8");
    const malformed = parse(originalSpec);
    malformed.components.schemas = { remote: { $ref: "https://external.invalid/schema" } };
    await writeFile(specPath, stringify(malformed));
    await expect(service.install("service-health", request)).rejects.toThrow(/references must be local/);
    expect((await pool().query("select count(*) from pack_install where org_id=$1", [org])).rows[0].count).toBe("0");
    await writeFile(specPath, originalSpec);
    expect((await service.install("service-health", request)).status).toBe("installed");
    expect(authorizedReads).toBeGreaterThan(0);
    const config = await readFile(join(root, "graphjin/agentic.yml"), "utf8");
    expect(config).not.toContain(token);
    expect(config).toContain("gjsecret://");
    expect(await readFile(join(root, "graphjin/secrets.enc.yml"), "utf8")).not.toContain(token);
    expect((await service.doctor("service-health")).status).toBe("ready");
    const metric = (await pool().query("select id from metric where org_id=$1", [org])).rows[0];
    const job = (await pool().query("insert into processing_job(org_id,kind,status,trigger,trigger_payload) values ($1,'metric_refresh','running','fixture',$2) returning id", [org, JSON.stringify({ metricId: metric.id })])).rows[0].id;
    await runMetricRefresh(job, org);
    expect((await pool().query("select value from metric_snapshot where metric_id=$1", [metric.id])).rows[0].value).toBe("1");
    expect((await sweepWatchers(org, { enqueueFire: async () => {} })).checked).toBe(1);
    expect((await pool().query("select last_value,last_error from watcher where org_id=$1", [org])).rows[0]).toEqual({ last_value: 1, last_error: null });
    await docker("restart", graphjin);
    await ready();
    expect((await service.doctor("service-health")).status).toBe("ready");
    const mutation = await query("mutation { service_health_reset_health { healthy } }");
    expect(mutation.errors?.length).toBeGreaterThan(0);
    expect((await query("mutation { service_health_delete_health { healthy } }")).errors?.length).toBeGreaterThan(0);
    expect(writes).toBe(0);
    expect(reads).toBe(authorizedReads);
    await expect(service.configure("service-health", { secrets: { "service.api_token": "wrong-fixture-token" } })).rejects.toThrow(/preflight/);
    expect((await service.doctor("service-health")).status).toBe("ready");
    expect(reads).toBeGreaterThan(authorizedReads);
    await expect(service.configure("service-health", { inputs: { "service.base_url": "http://127.0.0.1:1" } })).rejects.toThrow(/preflight/);
    expect((await service.doctor("service-health")).status).toBe("ready");
    health = null;
    await expect(service.configure("service-health", { inputs: { "service.timezone": "Asia/Kolkata" } })).rejects.toThrow(/result path/);
    health = 1;
    expect((await service.doctor("service-health")).status).toBe("ready");
    const persisted = await pool().query("select config from pack_install where org_id=$1", [org]);
    expect(JSON.stringify(persisted.rows)).not.toContain(token);
    const plans = await pool().query("select plan from pack_operation where org_id=$1", [org]);
    expect(JSON.stringify(plans.rows)).not.toContain(token);
    await service.uninstall("service-health");
    const before = authorizedReads;
    const result = await query(await readFile(resolve("test/fixtures/service-health/graphjin/queries/health.gql"), "utf8"));
    expect(result.errors?.length).toBeGreaterThan(0);
    expect(authorizedReads).toBe(before);
    const applied = await applyPackGraphjinConfig({ endpoint, orgId: org, configFile: join(root, "graphjin/agentic.yml"), update: { source_patches: [{ name: "fixture", read_only: true, access: { read: "blocked", write: "blocked", delete: "blocked" } }] }, restartAfterPersist: true });
    const configFile = join(root, "graphjin/agentic.yml");
    const changed = (await readFile(configFile, "utf8")) + "\n# Later operator change\n";
    await writeFile(configFile, changed);
    await expect(applied.restore()).rejects.toThrow(/changed after pack apply/);
    expect(await readFile(configFile, "utf8")).toBe(changed);
  }, 120_000);
  it("persists uploads without activation and installs only the reviewed version, including after encrypted backup restore", async () => {
    const embedded = join(root, "empty-packs");
    await mkdir(embedded);
    const uploaded = new PackService(org, embedded);
    const entries = await packZipEntries(resolve("test/fixtures/service-health"));
    const archive = zipFixture(entries);
    const before = (await pool().query("select status from pack_install where org_id=$1", [org])).rows;
    const provenance = await uploaded.upload(archive);
    expect(await uploaded.upload(archive)).toEqual(provenance);
    expect((await uploaded.inspect("service-health")).source).toBe("uploaded");
    expect((await uploaded.list()).some(pack => pack.id === "service-health")).toBe(true);
    expect((await pool().query("select status from pack_install where org_id=$1", [org])).rows).toEqual(before);
    const port = (provider.address() as { port: number }).port;
    const request = { version: "0.1.0", dataSourceId: sourceId, inputs: { "service.base_url": `http://host.docker.internal:${port}` }, secrets: { "service.api_token": token } };
    await expect(uploaded.install("service-health", request)).rejects.toThrow(/review/);
    const review = await uploaded.review("service-health", request);
    expect(JSON.stringify(review)).not.toContain(token);
    const storedSkill = join(dirname(localConfigPath()), "agents/orgs", org, "packs/service-health/versions/0.1.0/bundle/skills/service-health-review/SKILL.md");
    const skill = await readFile(storedSkill, "utf8");
    await writeFile(storedSkill, `${skill}\nChanged after review\n`);
    await expect(uploaded.install("service-health", { ...request, reviewHash: review.reviewHash })).rejects.toThrow(/changed after upload/);
    await writeFile(storedSkill, skill);
    for (const changed of [{ actorUserId: "another-actor" }, { inputs: { ...request.inputs, "service.timezone": "Asia/Kolkata" } }, { secrets: { "service.api_token": "changed-token" } }]) {
      await expect(uploaded.install("service-health", { ...request, ...changed, reviewHash: review.reviewHash })).rejects.toThrow(/review/);
    }
    const approved = { ...request, reviewHash: review.reviewHash, idempotencyKey: "upload-first-install" };
    expect((await uploaded.install("service-health", approved)).status).toBe("installed");
    expect((await uploaded.install("service-health", approved)).status).toBe("installed");
    const installed = (await pool().query("select source,version,config from pack_install where org_id=$1", [org])).rows[0];
    expect(installed.source).toBe("uploaded");
    expect(installed.config._bundle.contentHash).toBe(provenance.contentHash);
    const next = entries.map(entry => entry.name === "service-health/pack.yaml"
      ? { ...entry, data: String(entry.data).replace("version: 0.1.0", "version: 0.2.0") }
      : entry.name.startsWith("service-health/workflows/") && entry.data
        ? { ...entry, data: String(entry.data).replace("description: Review the current provider health", "description: Review the updated provider health") } : entry);
    await uploaded.upload(zipFixture(next));
    const restarted = new PackService(org, embedded);
    expect(((await restarted.inspect("service-health")).manifest as { metadata: { version: string } }).metadata.version).toBe("0.2.0");
    const configure = await restarted.review("service-health", request, "configure");
    expect((configure.manifest as { metadata: { version: string } }).metadata.version).toBe("0.1.0");
    expect((await restarted.doctor("service-health")).status).toBe("ready");
    await expect(restarted.install("service-health", { ...request, version: undefined, reviewHash: review.reviewHash })).rejects.toThrow(/review/);
    await expect(new PackService(`${org}-other`, embedded).inspect("service-health")).rejects.toThrow();

    // Run the production encrypted config snapshot and restore code, with the
    // same config volume layout used by Docker. No alternate archive routine.
    const proof = join(root, "backup-proof");
    await mkdir(join(proof, "snapshots"), { recursive: true });
    await cp(dirname(localConfigPath()), join(proof, "snapshots/config"), { recursive: true });
    for (const name of ["graphjin", "records-graphjin", "host-config", "plugins"]) await mkdir(join(proof, "snapshots", name));
    for (const name of ["backup", "restored"]) await mkdir(join(proof, name));
    await docker("run", "--rm", "-v", `${proof}:/proof`, "-e", `PGBACKREST_REPO1_CIPHER_PASS=${randomBytes(32).toString("hex")}`, "--entrypoint", "python3", "openneko-pack-backup-test", "-c", [
      "import importlib.util,pathlib",
      "spec=importlib.util.spec_from_file_location('backup','/usr/local/bin/openneko-backup.py')",
      "b=importlib.util.module_from_spec(spec); spec.loader.exec_module(b)",
      "b.SNAPSHOT_SOURCES=pathlib.Path('/proof/snapshots')",
      "metadata=b.snapshot_configs(pathlib.Path('/proof/backup'))",
      "b.decrypt_snapshot(pathlib.Path('/proof/backup'), {'config_snapshot':metadata}, pathlib.Path('/proof/restored'))",
    ].join("\n"));
    const restored = new PackService(org, embedded, join(proof, "restored/config/agents/orgs", org, "packs"));
    expect((await restored.inspect("service-health", "0.1.0")).upload).toEqual(provenance);
    expect((await restored.review("service-health", request, "configure")).reviewHash).toBe(configure.reviewHash);
    await cp(join(proof, "restored/config"), join(proof, "fresh-process/openneko"), { recursive: true });
    // A new process must recover both the catalog and the approval signing key
    // from restored configuration, without this test process's module caches.
    const check = `import { PackService } from ${JSON.stringify(resolve("src/packs/service.ts"))};
import { pool } from ${JSON.stringify(resolve("../../packages/db/src/index.ts"))};
(async () => { try {
  const service = new PackService(${JSON.stringify(org)}, ${JSON.stringify(embedded)});
  const result = await service.review("service-health", {version:"0.1.0",dataSourceId:${JSON.stringify(sourceId)},inputs:${JSON.stringify(request.inputs)}}, "configure");
  if (result.reviewHash !== ${JSON.stringify(configure.reviewHash)}) throw new Error("restored approval identity changed");
  console.log("restored catalog and approval verified in fresh process");
} finally { await pool().end(); } })().catch(error => { console.error(error.message); process.exitCode=1; });`;
    const child = await exec("pnpm", ["exec", "tsx", "--eval", check], { env: { ...process.env, XDG_CONFIG_HOME: join(proof, "fresh-process") }, maxBuffer: 1024 * 1024 });
    expect(child.stdout).toContain("restored catalog and approval verified");
    expect((await restored.configure("service-health", { ...request, reviewHash: configure.reviewHash })).status).toBe("installed");
    expect((await restored.doctor("service-health")).status).toBe("ready");
    const upgradeRequest = { ...request, version: "0.2.0" };
    const upgrade = await restored.review("service-health", upgradeRequest, "upgrade");
    const workflow = (await pool().query("select id,description from workflow_definition where org_id=$1", [org])).rows[0];
    await pool().query("update workflow_definition set description='operator edit' where id=$1", [workflow.id]);
    await expect(restored.upgrade("service-health", { ...upgradeRequest, reviewHash: upgrade.reviewHash })).rejects.toThrow(/review/);
    await pool().query("update workflow_definition set description=$2 where id=$1", [workflow.id, workflow.description]);
    expect((await restored.upgrade("service-health", { ...upgradeRequest, reviewHash: upgrade.reviewHash })).status).toBe("installed");
    expect((await pool().query("select version from pack_install where org_id=$1", [org])).rows[0].version).toBe("0.2.0");
    expect((await pool().query("select description from workflow_definition where id=$1", [workflow.id])).rows[0].description).toBe("Review the updated provider health");
    // Candidate discovery is not needed to manage an installed pinned version.
    await rm(join(proof, "restored/config/agents/orgs", org, "packs/service-health/candidate.json"));
    expect((await restored.doctor("service-health")).status).toBe("ready");
    await restored.uninstall("service-health");
  }, 120_000);
  it.skipIf(process.env.OPENNEKO_PACK_UI_TEST !== "1")("completes custom installation through the real web API and host CLI", async () => {
    const embedded = join(root, "ui-embedded");
    await mkdir(embedded);
    const uploaded = new PackService(org, embedded);
    const admin = createServer(createAdminHandler({ packs: uploaded }));
    await new Promise<void>(resolve => admin.listen(0, "127.0.0.1", resolve));
    try {
      const port = (admin.address() as { port: number }).port;
      const providerPort = (provider.address() as { port: number }).port;
      const entries = (await packZipEntries(resolve("test/fixtures/service-health"))).map(entry => entry.name.endsWith("pack.yaml") ? { ...entry, data: String(entry.data).replace("version: 0.1.0", "version: 0.3.0") } : entry);
      const archive = join(root, "service-health.zip");
      await writeFile(archive, zipFixture(entries));
      const env = { ...process.env, WORKER_ADMIN_URL: `http://127.0.0.1:${port}`, OPENNEKO_PACK_UI_ARCHIVE: archive,
        OPENNEKO_PACK_UI_PROVIDER: `http://host.docker.internal:${providerPort}`, OPENNEKO_PACK_UI_SOURCE: sourceId, OPENNEKO_PACK_UI_TOKEN: token, OPENNEKO_PROXIED: "1" };
      const cli = join(root, "openneko");
      await exec("go", ["build", "-o", cli, "./cmd/openneko"], { cwd: resolve("../openneko"), env, maxBuffer: 1024 * 1024 });
      const upload = await exec(cli, ["--local", "pack", "upload", archive, "--output", "json"], { env });
      expect(JSON.parse(upload.stdout).version).toBe("0.3.0");
      expect((await uploaded.status("service-health"))?.status).not.toBe("installed");
      await exec(cli, ["--local", "secrets", "set", "pack.service-health", "TEST_TOKEN", token], { env });
      const installed = await exec(cli, ["--local", "pack", "install", "service-health", "--input", `service.base_url=http://host.docker.internal:${providerPort}`, "--secret-ref", "service.api_token=TEST_TOKEN", "--source-id", sourceId, "--yes", "--output", "json"], { env });
      expect(JSON.parse(installed.stdout).status).toBe("installed");
      expect(installed.stdout + installed.stderr).not.toContain(token);
      expect((await uploaded.doctor("service-health")).status).toBe("ready");
      await exec(cli, ["--local", "pack", "uninstall", "service-health", "--output", "json"], { env });
      await exec("pnpm", ["exec", "playwright", "test", "--config=playwright.packs.config.ts"], { cwd: resolve("../web"), env, maxBuffer: 4 * 1024 * 1024 }).catch(async error => {
        await writeFile("/tmp/issue290-step5-browser.log", String(error.stdout ?? "") + String(error.stderr ?? ""));
        throw new Error("Admin Packs browser proof failed; see /tmp/issue290-step5-browser.log");
      });
      expect((await uploaded.status("service-health"))?.version).toBe("0.3.0");
      expect((await uploaded.doctor("service-health")).status).toBe("ready");
      const configured = await exec(cli, ["--local", "pack", "configure", "service-health", "--input", "service.timezone=UTC", "--yes", "--output", "json"], { env });
      expect(JSON.parse(configured.stdout).status).toBe("installed");
      expect(configured.stdout + configured.stderr).not.toContain(token);
      await exec(cli, ["--local", "pack", "uninstall", "service-health", "--output", "json"], { env });
      expect((await uploaded.status("service-health"))?.status).toBe("removed");
    } finally { await new Promise<void>(resolve => admin.close(() => resolve())); }
  }, 240_000);
});
