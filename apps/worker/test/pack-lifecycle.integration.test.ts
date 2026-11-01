import { mkdtemp, mkdir, readFile, writeFile, rm, cp } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";
import { stringify, parse } from "yaml";

const state = vi.hoisted(() => ({ workspace: "", failQuery: false, queries: [] as Array<{ baseUrl: string; query: string; variables?: Record<string, unknown> }> }));
vi.mock("@neko/db/jobs", () => ({ enqueue: async () => "fixture-job", QUEUE: { METRIC_REFRESH: "metric_refresh" } }));
vi.mock("@neko/llm/work", () => ({ ensureOrgWorkspace: async () => ({ orgRoot: state.workspace, skillsRoot: join(state.workspace, "skills") }) }));
vi.mock("@neko/llm/graphjin", async importOriginal => ({
  ...await importOriginal<object>(),
  graphjinQuery: async (input: { baseUrl: string; query: string; variables?: Record<string, unknown> }) => {
    state.queries.push(input);
    if (state.failQuery) return { data: null, errors: [{ message: "fixture failure" }] };
    if (input.query.includes("gj_config")) return { data: { gj_config: { catalog_revision: "fixture", sources: parse(await readFile(process.env.OPENNEKO_GRAPHJIN_CONFIG!, "utf8")).sources } } };
    if (input.query.includes("SecretColumnCanary")) return { data: null, errors: [{ message: "blocked" }] };
    if (input.query.includes("Magento")) return { data: { sales_order: [] } };
    return { data: { health: { healthy: 1 } } };
  },
}));
// This suite exercises real Postgres/filesystem lifecycle; GraphJin transport is
// substituted. The packaged connector runtime is verified separately in step 3.
vi.mock("../src/packs/graphjin-config.js", () => ({
  applyPackGraphjinConfig: async ({ configFile, update }: { configFile: string; update: Record<string, unknown> }) => {
    const before = await readFile(configFile, "utf8");
    const config = parse(before);
    if (update.update_sources) {
      for (const source of update.update_sources as Array<Record<string, unknown>>) {
        config.sources = config.sources.filter((current: Record<string, unknown>) => current.name !== source.name);
        config.sources.push(source);
      }
    }
    if (update.tables) {
      const tables = new Map((config.tables ?? []).map((table: any) => [table.name, table]));
      for (const value of update.tables as any[]) { const { database, ...table } = value; tables.set(table.name, table); }
      config.tables = [...tables.values()];
    }
    if (update.relationships && (update.relationships as unknown[]).length) config.relationships = update.relationships;
    if (update.source_patches) {
      for (const patch of update.source_patches as Array<Record<string, unknown>>) {
        const source = config.sources.find((source: Record<string, unknown>) => source.name === patch.name);
        Object.assign(source, patch);
      }
    }
    await writeFile(configFile, stringify(config));
    return { restore: () => writeFile(configFile, before) };
  },
}));
vi.mock("../src/packs/magento-preflight.js", async importOriginal => {
  const actual = await importOriginal<typeof import("../src/packs/magento-preflight.js")>();
  return { ...actual, runMagentoPreflight: vi.fn(async () => ({
    databaseVersion: "10.6.0", databaseType: "mariadb", sqlMode: "", databaseTimezone: "UTC", tablePrefix: "",
    tableNames: [...actual.MAGENTO_ANALYTICS_TABLES], availableAnalyticsTables: [...actual.MAGENTO_ANALYTICS_TABLES], blockedTables: [],
    storeIds: [1], scopes: [{ storeId: 1, code: "default", websiteId: 1, groupId: 1, name: "Main" }],
    baseCurrency: "USD", timezone: "UTC", magentoVersion: "2.4.7", operatorReadiness: "ready",
    operatorDomains: { catalog: "ready", inventory: "ready", orders: "ready", promotions: "ready", content: "ready", customers: "ready" }, bulkConsumerReadiness: "ready",
  })) };
});
import { runMagentoPreflight } from "../src/packs/magento-preflight.js";
import { packArtifactSource } from "@neko/llm/graphjin";
import { reconcileWatcherWatches, type WatcherWatchClient } from "@neko/llm/workflows";

/** A GraphJin watch client that records each watch it is asked to create. */
function watchClient() {
  const sent: Array<{ endpoint: string; definition: Record<string, unknown> }> = [];
  const client: WatcherWatchClient = {
    upsert: async ({ endpoint, definition }) => {
      sent.push({ endpoint, definition });
      return { id: `watch:${sent.length}`, action_approval: "approved" };
    },
    approve: async ({ watchId }) => ({ id: watchId, action_approval: "approved" }),
    remove: async () => {},
    status: async () => null,
    list: async () => [],
  };
  return { sent, client };
}
import { runMetricRefresh } from "../src/jobs/metric-refresh.js";
import { buildSavedQueryVariables } from "../src/jobs/deterministic-metric.js";
import { pool } from "@neko/db";
import { PackService } from "../src/packs/service.js";

const enabled = process.env.OPENNEKO_PACK_LIFECYCLE_TEST === "1";
describe.skipIf(!enabled)("shared pack lifecycle with Postgres", () => {
  let root: string;
  let service: PackService;
  let dataSourceId: string;
  let manifest: Record<string, any>;
  const org = `pack-test-${Date.now()}`;
  beforeAll(async () => {
    root = await mkdtemp(join(tmpdir(), "pack-shared-"));
    state.workspace = join(root, "workspace");
    vi.stubEnv("XDG_CONFIG_HOME", join(root, "config"));
    vi.stubEnv("OPENNEKO_GRAPHJIN_CONFIG", join(root, "graphjin.yml"));
    await writeFile(join(root, "graphjin.yml"), "sources: []\n");
    await mkdir(join(state.workspace, "skills"), { recursive: true });
    const pack = join(root, "packs", "service-health");
    for (const dir of ["graphjin/specs", "graphjin/queries", "metrics", "workflows", "watchers", "actions", "policies", "skills/health-review"]) await mkdir(join(pack, dir), { recursive: true });
    manifest = {
      apiVersion: "openneko.app/v1", kind: "SolutionPack",
      metadata: { id: "service-health", name: "Service health", version: "0.1.0", publisher: "fixture", category: "operations" },
      compatibility: { openneko: ">=2.27.0", graphjin: { analytics: ">=3.20.0", operator: "unused" }, applications: [{ id: "service-health", editions: ["custom"], versions: "*" }], databases: [] },
      inputs: [{ key: "service.timezone", type: "timezone", default: "Asia/Kolkata" }], secrets: [],
      artifacts: { graphjin: { sources: "graphjin/sources.yaml", relationships: "graphjin/relationships.yaml", specs: ["graphjin/specs/health.yaml"], savedQueries: "graphjin/queries" }, metrics: "metrics", workflows: "workflows", watchers: "watchers", actions: "actions", policies: "policies", skills: ["skills/health-review"] },
      health: { requiredPreflight: ["queries"], readiness: { reporting: ["analytics-smoke"] }, postInstall: ["graphjin-reload"], postWriteCanary: [] },
    };
    await writeFile(join(pack, "skills/health-review/SKILL.md"), "---\nname: health-review\ndescription: Review service health\n---\nRead the health metric and report failures.\n");
    await writeFile(join(pack, "pack.yaml"), stringify(manifest));
    await writeFile(join(pack, "graphjin/sources.yaml"), stringify({ sources: [{ name: "service_health", kind: "api", base_url: "https://fixture.example.test", openapi: "graphjin/specs/health.yaml" }] }));
    await writeFile(join(pack, "graphjin/relationships.yaml"), stringify({ key: "relationships.health", targetRef: "health.relationships", source: "service_health", relationships: [] }));
    await writeFile(join(pack, "graphjin/specs/health.yaml"), stringify({ openapi: "3.0.3", paths: { "/health": { get: { operationId: "health" } } } }));
    await writeFile(join(pack, "graphjin/queries/health.gql"), "query { health { healthy } }");
    await writeFile(join(pack, "workflows/health.yaml"), stringify({ key: "workflow.health", targetRef: "health.review", name: "Health review", description: "Fixture", enabled: true, status: "active", goal: "Review health", schedule: { cron: "0 9 * * *", timezoneInput: "service.timezone", enabled: true }, outputContract: { kind: "brief", required: ["summary"] } }));
    await writeFile(join(pack, "metrics/health.yaml"), stringify({ key: "metric.health", targetRef: "health.metric", title: "Health", description: "Service health", role: "executive", chartHint: "metric", unit: "count", directionGood: "up", cadence: "hourly", calculationNote: "Reported health", execution: { mode: "saved_query", source: "service_health", query: "health", result: { kind: "scalar", path: "health.healthy" }, freshnessSeconds: 3600, variables: { timezone: { kind: "literal", value: "{{service.timezone}}" }, from: { kind: "seconds_ago", seconds: 3600 } } } }));
    await writeFile(join(pack, "watchers/health.yaml"), stringify({ key: "watcher.health", targetRef: "health.watch", name: "Health watch", description: "Watch health", workflow: "workflow.health", enabled: true, activation: "always", query: "health", valuePath: "health.healthy", operator: "lt", threshold: 1, cadenceSeconds: 3600, debounceSeconds: 0, cooldownSeconds: 0, severity: "high", dedupeKey: "health", readinessSignals: ["reporting"], variables: { timezone: { kind: "literal", value: "{{service.timezone}}" } } }));
    await pool().query("insert into organization (id,name) values ($1,'Pack fixture')", [org]);
    dataSourceId = (await pool().query("insert into data_source (org_id,kind,graphql_url) values ($1,'graphjin','http://fixture.invalid/api/v1/graphql') returning id", [org])).rows[0].id;
    service = new PackService(org, join(root, "packs"));
  });
  afterAll(async () => {
    await pool().query("delete from organization where id=$1", [org]);
    await pool().end();
    vi.unstubAllEnvs();
    await rm(root, { recursive: true, force: true });
  });
  it("runs a skills-only lifecycle without a data source or GraphJin configuration", async () => {
    const skillOrg = `${org}-skills`;
    const packRoot = join(root, "packs", "skills-only");
    const skillFile = join(packRoot, "skills/skills-only-review/SKILL.md");
    const installedSkill = join(state.workspace, "skills/skills-only-review/SKILL.md");
    const declaration = {
      ...manifest,
      metadata: { ...manifest.metadata, id: "skills-only", name: "Skills only" },
      compatibility: { openneko: ">=2.27.0", applications: [], databases: [] },
      artifacts: { skills: ["skills/skills-only-review"] },
      health: { requiredPreflight: [], readiness: {}, postInstall: [], postWriteCanary: [] },
    };
    await mkdir(join(packRoot, "skills/skills-only-review"), { recursive: true });
    await writeFile(skillFile, "---\nname: skills-only-review\ndescription: Review notes\n---\nReview the notes.\n");
    await writeFile(join(packRoot, "pack.yaml"), stringify(declaration));
    await pool().query("insert into organization (id,name) values ($1,'Skills fixture')", [skillOrg]);
    const config = process.env.OPENNEKO_GRAPHJIN_CONFIG;
    delete process.env.OPENNEKO_GRAPHJIN_CONFIG;
    const queryCount = state.queries.length;
    try {
      let skills = new PackService(skillOrg, join(root, "packs"));
      const review = await skills.review("skills-only");
      expect(review.runtime.source).toBeUndefined();
      await expect(skills.install("skills-only", { dataSourceId })).rejects.toThrow("does not use a data source");
      expect((await skills.install("skills-only", { reviewHash: review.reviewHash })).status).toBe("installed");
      expect(await readFile(installedSkill, "utf8")).toContain("Review the notes.");
      await skills.configure("skills-only", { inputs: { "service.timezone": "UTC" } });
      skills = new PackService(skillOrg, join(root, "packs"));
      expect((await skills.status("skills-only"))?.configuration.inputs["service.timezone"]).toBe("UTC");
      expect(await skills.doctor("skills-only")).toMatchObject({ status: "ready", checks: [{ id: "configuration" }] });
      declaration.metadata.version = "0.2.0";
      await writeFile(skillFile, "---\nname: skills-only-review\ndescription: Review notes\n---\nReview the updated notes.\n");
      await writeFile(join(packRoot, "pack.yaml"), stringify(declaration));
      expect((await skills.upgrade("skills-only")).version).toBe("0.2.0");
      expect(await readFile(installedSkill, "utf8")).toContain("updated notes");
      expect((await skills.uninstall("skills-only")).status).toBe("removed");
      await expect(readFile(installedSkill)).rejects.toMatchObject({ code: "ENOENT" });
      expect(state.queries).toHaveLength(queryCount);
      expect((await pool().query("select count(*) from data_source where org_id=$1", [skillOrg])).rows[0].count).toBe("0");
    } finally {
      if (config === undefined) delete process.env.OPENNEKO_GRAPHJIN_CONFIG;
      else process.env.OPENNEKO_GRAPHJIN_CONFIG = config;
      await pool().query("delete from organization where id=$1", [skillOrg]);
    }
  });

  it("installs, retries, configures, upgrades, preserves drift, compensates failure and uninstalls", async () => {
    const foreignOrg = `${org}-other`;
    await pool().query("insert into organization (id,name) values ($1,'Other fixture')", [foreignOrg]);
    try {
      const foreign = (await pool().query("insert into data_source (org_id,kind,graphql_url) values ($1,'graphjin','http://foreign.invalid') returning id", [foreignOrg])).rows[0].id;
      await expect(service.install("service-health", { dataSourceId: foreign })).rejects.toThrow(/organization/);
      await expect(service.install("service-health")).rejects.toThrow(/select.*dataSourceId/);
      expect((await pool().query("select count(*) from pack_install where org_id=$1", [org])).rows[0].count).toBe("0");
    } finally { await pool().query("delete from organization where id=$1", [foreignOrg]); }
    expect((await service.install("service-health", { dataSourceId })).status).toBe("installed");
    expect((await pool().query("select active from metric where org_id=$1", [org])).rows[0].active).toBe(true);
    expect((await pool().query("select variables_json from watcher where org_id=$1", [org])).rows[0].variables_json).toEqual({ timezone: { kind: "literal", value: "Asia/Kolkata" } });
    expect(state.queries.some(query => query.variables?.timezone === "Asia/Kolkata")).toBe(true);
    expect(await readFile(join(state.workspace, "skills/health-review/SKILL.md"), "utf8")).toContain("Read the health metric");
    expect((await service.status("service-health"))?.readiness.reporting?.status).toBe("ready");
    const first = await pool().query("select id,cron_timezone from workflow_definition where org_id=$1", [org]);
    expect(first.rows[0].cron_timezone).toBe("Asia/Kolkata");
    expect((await service.plan("service-health")).entries.every(entry => entry.action === "noop")).toBe(true);
    await service.install("service-health", { dataSourceId });
    expect((await pool().query("select count(*) from pack_operation where org_id=$1", [org])).rows[0].count).toBe("1");
    await service.configure("service-health", { inputs: { "service.timezone": "UTC" } });
    expect((await pool().query("select cron_timezone from workflow_definition where org_id=$1", [org])).rows[0].cron_timezone).toBe("UTC");
    manifest.metadata.version = "0.2.0";
    await writeFile(join(root, "packs/service-health/pack.yaml"), stringify(manifest));
    await service.upgrade("service-health");
    expect((await service.status("service-health"))?.version).toBe("0.2.0");
    await pool().query("update workflow_definition set description='Operator edit' where org_id=$1", [org]);
    await expect(service.upgrade("service-health")).rejects.toThrow(/conflict/);
    await pool().query("update workflow_definition set description='Fixture' where org_id=$1", [org]);
    const before = await readFile(join(root, "graphjin.yml"), "utf8");
    state.failQuery = true;
    await expect(service.configure("service-health", { inputs: { "service.timezone": "Europe/London" } })).rejects.toThrow(/preflight/);
    state.failQuery = false;
    expect(await readFile(join(root, "graphjin.yml"), "utf8")).toBe(before);
    expect((await service.status("service-health"))?.status).toBe("installed");
    expect((await service.doctor("service-health")).status).toBe("ready");
    const withoutGraphjin = { ...manifest, artifacts: { skills: manifest.artifacts.skills }, health: { requiredPreflight: [], readiness: {}, postInstall: [], postWriteCanary: [] } };
    await writeFile(join(root, "packs/service-health/pack.yaml"), stringify(withoutGraphjin));
    await expect(service.upgrade("service-health")).rejects.toThrow("removing GraphJin artifacts requires uninstall first");
    expect(await readFile(join(root, "graphjin.yml"), "utf8")).toBe(before);
    expect((await service.status("service-health"))?.status).toBe("installed");
    expect((await service.uninstall("service-health")).status).toBe("removed");
    await writeFile(join(root, "packs/service-health/pack.yaml"), stringify(manifest));
    expect((await pool().query("select active from metric where org_id=$1", [org])).rows[0].active).toBe(false);
    expect((await pool().query("select enabled from watcher where org_id=$1", [org])).rows[0].enabled).toBe(false);
    const last = await pool().query("select id,enabled,cron_enabled from workflow_definition where org_id=$1", [org]);
    expect(last.rows[0]).toEqual({ id: first.rows[0].id, enabled: false, cron_enabled: false });
  });
  it("pins runtime endpoints and blocks disabled/changed targets without following a new default", async () => {
    await service.install("service-health", { dataSourceId, inputs: { "service.timezone": "Asia/Kolkata" } });
    const card = (await pool().query("select id,definition_json from metric where org_id=$1", [org])).rows[0];
    expect(buildSavedQueryVariables(card.definition_json).timezone).toBe("Asia/Kolkata");
    const other = (await pool().query("insert into data_source (org_id,name,kind,graphql_url,is_default) values ($1,'other','graphjin','http://wrong.invalid/graphql',true) returning id", [org])).rows[0].id;
    expect((await packArtifactSource(org, "metric", card.id))?.id).toBe(dataSourceId);
    const job = (await pool().query("insert into processing_job (org_id,kind,status,trigger,trigger_payload) values ($1,'metric_refresh','running','test',$2) returning id", [org, JSON.stringify({ metricId: card.id })])).rows[0].id;
    await runMetricRefresh(job, org);
    expect(state.queries.at(-1)).toMatchObject({ baseUrl: "http://fixture.invalid/api/v1/graphql", variables: { timezone: "Asia/Kolkata" } });
    expect((await pool().query("select value from metric_snapshot where metric_id=$1", [card.id])).rows[0].value).toBe("1");
    // The watcher's GraphJin watch goes to the pinned source with the pack's variables.
    const watches = watchClient();
    await reconcileWatcherWatches(org, { client: watches.client, webhookUrl: "http://worker.test/admin/events/watcher" });
    expect(watches.sent).toContainEqual(expect.objectContaining({ endpoint: "http://fixture.invalid/api/v1/graphql", definition: expect.objectContaining({ variables_json: { timezone: "Asia/Kolkata" } }) }));
    await pool().query("update data_source set enabled=false where id=$1", [dataSourceId]);
    await expect(packArtifactSource(org, "metric", card.id)).rejects.toThrow(/unavailable/);
    const count = state.queries.length;
    await expect(runMetricRefresh(job, org)).rejects.toThrow(/unavailable/);
    expect(state.queries.length).toBe(count);
    await expect(service.configure("service-health")).rejects.toThrow(/unavailable/);
    await pool().query("update data_source set enabled=true,graphql_url='http://changed.invalid/graphql' where id=$1", [dataSourceId]);
    await expect(packArtifactSource(org, "metric", card.id)).rejects.toThrow(/changed/);
    await service.configure("service-health", { dataSourceId });
    expect((await packArtifactSource(org, "metric", card.id))?.graphqlUrl).toBe("http://changed.invalid/graphql");
    await service.uninstall("service-health");
    await pool().query("delete from data_source where id=$1", [other]);
  });

  it("borrows an explicit read-only database source and never revokes or takes ownership of it", async () => {
    const pack = join(root, "packs", "database-health");
    await cp(join(root, "packs", "service-health"), pack, { recursive: true });
    const declaration = structuredClone(manifest);
    declaration.metadata = { ...manifest.metadata, id: "database-health", version: "0.1.0" };
    declaration.artifacts.graphjin.specs = [];
    declaration.artifacts.skills = [];
    declaration.compatibility.databases = [{ engine: "postgres", versions: ">=14" }];
    declaration.health.requiredPreflight = ["db-connect", "db-read-only"];
    await writeFile(join(pack, "pack.yaml"), stringify(declaration));
    await writeFile(join(pack, "graphjin/sources.yaml"), stringify({ sources: [{ name: "service_health", kind: "database" }] }));
    // Distinct native resources let two packs share a physical source safely.
    for (const path of ["metrics/health.yaml", "workflows/health.yaml", "watchers/health.yaml"]) {
      const value = parse(await readFile(join(pack, path), "utf8"));
      value.targetRef = `database.${value.targetRef}`;
      if (value.name) value.name = `Database ${value.name}`;
      await writeFile(join(pack, path), stringify(value));
    }
    const physical = { name: "customer_db", kind: "database", type: "postgres", host: "fixture", port: 5432, dbname: "customer", read_only: true };
    const config = parse(await readFile(join(root, "graphjin.yml"), "utf8"));
    config.sources.push(physical);
    await writeFile(join(root, "graphjin.yml"), stringify(config));
    const sourceBindings = { "source.service_health": "customer_db" };
    await expect(service.install("database-health", { dataSourceId })).rejects.toThrow(/binding/);
    await service.install("database-health", { dataSourceId, sourceBindings });
    expect((await service.plan("database-health")).entries.every(entry => entry.action === "noop")).toBe(true);
    const reference = (await pool().query("select target_ref,metadata from pack_artifact where org_id=$1 and artifact_key='source.service_health' and metadata->>'borrowedSource'='true'", [org])).rows[0];
    expect(reference.target_ref).toBe("database-health.binding.service_health");
    expect(reference.metadata.locator).toEqual({ name: "customer_db" });
    const card = (await pool().query("select id from metric where org_id=$1 and slug='database.health.metric'", [org])).rows[0];
    expect((await packArtifactSource(org, "metric", card.id))?.id).toBe(dataSourceId);
    expect(state.queries.filter(value => !value.query.includes("gj_config")).at(-1)?.query).toContain('@database(name: "customer_db")');
    expect(await readFile(join(root, "queries/database_health_health.gql"), "utf8")).toContain('@database(name: "customer_db")');
    const job = (await pool().query("insert into processing_job (org_id,kind,status,trigger,trigger_payload) values ($1,'metric_refresh','running','test',$2) returning id", [org, JSON.stringify({ metricId: card.id })])).rows[0].id;
    await runMetricRefresh(job, org);
    expect(state.queries.at(-1)?.query).toContain('@database(name: "customer_db")');
    const watches = watchClient();
    await reconcileWatcherWatches(org, { client: watches.client, webhookUrl: "http://worker.test/admin/events/watcher" });
    expect(watches.sent).toContainEqual(expect.objectContaining({ definition: expect.objectContaining({ query: expect.stringContaining('@database(name: "customer_db")') }) }));
    const routingBefore = await readFile(join(root, "graphjin.yml"), "utf8");
    const routingChanged = parse(routingBefore);
    routingChanged.tables.find((table: any) => table.source === "customer_db").source = "wrong_database";
    await writeFile(join(root, "graphjin.yml"), stringify(routingChanged));
    await expect(runMetricRefresh(job, org)).rejects.toThrow(/routing changed/);
    await expect(service.configure("database-health")).rejects.toThrow(/routing changed/);
    await writeFile(join(root, "graphjin.yml"), routingBefore);
    const changed = parse(await readFile(join(root, "graphjin.yml"), "utf8"));
    changed.sources.find((source: { name: string }) => source.name === "customer_db").host = "changed";
    await writeFile(join(root, "graphjin.yml"), stringify(changed));
    await expect(packArtifactSource(org, "metric", card.id)).rejects.toThrow(/binding changed/);
    changed.sources = changed.sources.map((source: { name: string }) => source.name === "customer_db" ? physical : source);
    await writeFile(join(root, "graphjin.yml"), stringify(changed));
    const secondPack = join(root, "packs", "database-health-two");
    await cp(pack, secondPack, { recursive: true });
    declaration.metadata.id = "database-health-two";
    await writeFile(join(secondPack, "pack.yaml"), stringify(declaration));
    for (const file of ["metrics/health.yaml", "workflows/health.yaml", "watchers/health.yaml", "graphjin/queries/health.gql"]) await rm(join(secondPack, file));
    await writeFile(join(secondPack, "graphjin/queries/second.gql"), "query { health { healthy } }");
    await writeFile(join(secondPack, "graphjin/relationships.yaml"), stringify({ key: "relationships.second", targetRef: "second.relationships", source: "service_health", relationships: [] }));
    await service.install("database-health-two", { dataSourceId, sourceBindings });
    const rebindConfig = parse(await readFile(join(root, "graphjin.yml"), "utf8"));
    rebindConfig.sources.push({ ...physical, name: "another_db" });
    await writeFile(join(root, "graphjin.yml"), stringify(rebindConfig));
    await service.configure("database-health", { sourceBindings: { "source.service_health": "another_db" } });
    await runMetricRefresh(job, org);
    expect(state.queries.at(-1)?.query).toContain('@database(name: "another_db")');
    const reboundTables = parse(await readFile(join(root, "graphjin.yml"), "utf8")).tables;
    expect(new Set(reboundTables.map((table: any) => table.name)).size).toBe(reboundTables.length);
    await service.configure("database-health", { sourceBindings });
    await service.uninstall("database-health");
    expect((await service.doctor("database-health-two")).status).toBe("ready");
    await service.uninstall("database-health-two");
    expect(parse(await readFile(join(root, "graphjin.yml"), "utf8")).sources.find((source: { name: string }) => source.name === "customer_db")).toEqual(physical);
  });

  it("runs the full first-party Magento bundle through the same lifecycle", async () => {
    await cp(new URL("../../../packs/magento/", import.meta.url), join(root, "packs", "magento"), { recursive: true });
    expect(runMagentoPreflight).not.toHaveBeenCalled();
    const request = { dataSourceId, inputs: { "magento.base_url": "https://magento.example.test", "database.host": "fixture", "database.name": "magento" }, secrets: { "database.analytics_username": "fixture", "database.analytics_password": "fixture-password", "magento.integration_token": "fixture-token" } };
    expect((await service.install("magento", request)).status).toBe("installed");
    expect(runMagentoPreflight).toHaveBeenCalledTimes(1);
    expect((await service.plan("magento")).entries.every(entry => entry.action === "noop")).toBe(true);
    await service.install("magento", request);
    await service.configure("magento", { inputs: { "magento.timezone": "Europe/London" } });
    await service.upgrade("magento");
    expect((await service.doctor("magento")).status).toBe("ready");
    const preflight = await vi.mocked(runMagentoPreflight).mock.results[0]!.value;
    vi.mocked(runMagentoPreflight).mockResolvedValueOnce({
      ...preflight,
      operatorReadiness: "integration_token_missing",
      operatorDomains: Object.fromEntries(Object.keys(preflight.operatorDomains).map(domain => [domain, "integration_token_missing"])) as typeof preflight.operatorDomains,
    });
    const viewOnly = await service.configure("magento", { secrets: { "magento.integration_token": "" } });
    expect(viewOnly.readiness.analytics?.status).toBe("ready");
    expect(viewOnly.readiness.catalog).toEqual({ status: "blocked", reason: "integration_token_missing" });
    expect((await service.uninstall("magento")).status).toBe("removed");
    expect((await pool().query("select count(*) from metric where org_id=$1 and source='pack:magento' and active", [org])).rows[0].count).toBe("0");
  });

});
