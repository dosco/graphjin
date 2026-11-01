import { execFileSync, spawn, type ChildProcess } from "node:child_process";
import { mkdtempSync } from "node:fs";
import { createServer as createHttpServer, type Server } from "node:http";
import { rm, writeFile } from "node:fs/promises";
import { createServer } from "node:net";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it, vi } from "vitest";

const configHome = await vi.hoisted(async () => {
  const [{ mkdtempSync: make }, os, path] = await Promise.all([import("node:fs"), import("node:os"), import("node:path")]);
  const home = make(path.join(os.tmpdir(), "gj-parity-xdg-"));
  process.env.XDG_CONFIG_HOME = home;
  return home;
});

import { and, data_source, db, eq, organization, pool, watcher } from "@neko/db";
import { graphjinKeystoreKeyB64, graphjinSigningSecretB64 } from "@neko/llm/graphjin";
import { patchGraphjinWatches } from "@neko/llm/graphjin/sources-config";
import {
  deleteWatcher,
  getWatcher,
  graphjinWatcherWatchClient,
  receiveWatcherEvent,
  reconcileWatcherWatches,
  resetWatcher,
  saveWorkflow,
  saveWorkflowWithTrigger,
  setWatcherEnabled,
  upsertWatcher,
  watcherForGraphjinWatch,
  watcherWebhookSignatureValid,
} from "@neko/llm/workflows";
import { createAdminHandler } from "../src/admin-server.js";

/**
 * Parity: the scenarios of OpenNeko's former watcher poller tests
 * (packages/llm/test/integration/watchers.test.ts and the eval lifecycle
 * cases w01/w02), run against GraphJin watches. Values change in a real
 * database between GraphJin's polls, events arrive through the signed
 * webhook, and the worker applies the condition. Where the old tests moved
 * the poller's clock, this waits for GraphJin's real polls (10s here) and
 * moves only the worker's clock for debounce windows.
 *
 * Set OPENNEKO_TEST_GRAPHJIN_BIN to run it.
 */
const bin = process.env.OPENNEKO_TEST_GRAPHJIN_BIN;
const reachable = bin ? await pool().query("select 1").then(() => true, () => false) : false;
const POLL_SECONDS = 10;

async function freePort(): Promise<number> {
  return new Promise((resolve) => {
    const server = createServer();
    server.listen(0, "127.0.0.1", () => {
      const port = (server.address() as { port: number }).port;
      server.close(() => resolve(port));
    });
  });
}

async function until<T>(check: () => Promise<T | null | undefined | false>, label: string, ms = 45_000): Promise<T> {
  const deadline = Date.now() + ms;
  for (;;) {
    const value = await check();
    if (value) return value;
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${label}`);
    await new Promise((resolve) => setTimeout(resolve, 250));
  }
}

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

(reachable ? describe : describe.skip)("watcher parity on GraphJin watches (live)", () => {
  const orgId = `gj-parity-${Date.now().toString(36)}`;
  const dir = mkdtempSync(join(tmpdir(), "gj-parity-"));
  const dbPath = join(dir, "metrics.db");
  let child: ChildProcess | null = null;
  let admin: Server | null = null;
  let endpoint = "";
  let webhookUrl = "";
  let logs = "";
  let clockOffsetMs = 0;
  const fires: Array<{ watcherId: string; value: unknown; workflowId: string; triggerKind: string }> = [];
  const deps = () => ({ webhookUrl, minIntervalSeconds: POLL_SECONDS });

  const sqlite = (sql: string) => execFileSync("sqlite3", [dbPath, sql]);
  const setValue = (name: string, value: number) =>
    sqlite(`INSERT INTO metrics (name, account_id, value) VALUES ('${name}', '${orgId}', ${value}) ON CONFLICT(name) DO UPDATE SET value = excluded.value;`);
  const query = (name: string) => `query metric { metrics(where: { name: { eq: "${name}" } }) { value } }`;
  const row = async (id: string) => (await db().select().from(watcher).where(eq(watcher.id, id)))[0]!;
  const firesFor = (id: string) => fires.filter((fire) => fire.watcherId === id);
  const polledAt = async (id: string) => {
    const current = await row(id);
    if (!current.graphjin_watch_id) return null;
    const status = await graphjinWatcherWatchClient.status({ endpoint, orgId, watchId: current.graphjin_watch_id });
    return status?.last_polled_at ? new Date(status.last_polled_at).getTime() : null;
  };
  /** Waits for GraphJin to poll the watch after `after`, then for its event to land. */
  const nextPoll = async (id: string, after: number) => {
    const at = await until(async () => {
      const polled = await polledAt(id);
      return polled !== null && polled > after ? polled : null;
    }, `a poll of ${id} after ${after}: ${JSON.stringify(await graphjinWatcherWatchClient.status({ endpoint, orgId, watchId: (await row(id)).graphjin_watch_id! }).catch((e) => String(e)))}`);
    await sleep(1_500);
    return at;
  };
  const valueLands = (id: string, value: number) =>
    until(async () => (await row(id)).last_value === value, `value ${value} for ${id}\n${logs}`);

  beforeAll(async () => {
    const [port, adminPort] = [await freePort(), await freePort()];
    endpoint = `http://127.0.0.1:${port}/api/v1/graphql`;
    webhookUrl = `http://localhost:${adminPort}/admin/events/watcher`;
    admin = createHttpServer(createAdminHandler({
      watcherEvents: {
        verify: (raw, signature, watchId) => watcherWebhookSignatureValid(raw, signature, watchId),
        receive: async ({ watchId, event }) => {
          const found = await watcherForGraphjinWatch(watchId);
          if (!found) return { accepted: false };
          const { fired } = await receiveWatcherEvent(found, event, {
            now: () => new Date(Date.now() + clockOffsetMs),
            enqueueFire: async (payload) => {
              fires.push({
                watcherId: String(payload.triggerPayload.watcherId),
                value: payload.triggerPayload.value,
                workflowId: payload.workflowId,
                triggerKind: payload.triggerKind,
              });
            },
          });
          return { accepted: true, fired };
        },
      },
    }));
    await new Promise<void>((resolve) => admin!.listen(adminPort, "127.0.0.1", resolve));

    sqlite(`CREATE TABLE metrics (name TEXT PRIMARY KEY, account_id TEXT NOT NULL, value REAL NOT NULL);
      CREATE TABLE broken_metrics (name TEXT PRIMARY KEY, account_id TEXT NOT NULL, value REAL NOT NULL);
      INSERT INTO broken_metrics VALUES ('broken-at-runtime', '${orgId}', 1);`);
    const base = `app_name: OpenNeko watcher parity
mode: agentic
host_port: 127.0.0.1:${port}
production: true
disable_production_security: true
log_level: warn
artifacts:
  enabled: false
watches:
  enabled: false
  runner: off
tasks:
  enabled: false
secrets:
  keystore:
    key: "${graphjinKeystoreKeyB64()}"
system:
  capabilities:
    catalog.read: true
    raw_graphql.mutate: true
  root_access:
    gj_catalog: authenticated
auth:
  type: jwt
  jwt:
    secret: "${graphjinSigningSecretB64(orgId)}"
identity:
  user_id_claim: sub
  role_claims: [role]
  namespace_claim: org_id
  admin_roles: [admin]
roles:
  - name: service
sources:
  - name: shop
    kind: database
    type: sqlite
    path: ${dbPath}
    default: true
    read_only: true
    access:
      read: account
      namespace_column: account_id
`;
    await writeFile(join(dir, "agentic.yml"), patchGraphjinWatches(base, webhookUrl).content);
    child = spawn(bin!, ["serve", "--path", dir], { env: { ...process.env, GO_ENV: "agentic" }, stdio: ["ignore", "pipe", "pipe"] });
    child.stdout?.on("data", (chunk) => (logs += String(chunk)));
    child.stderr?.on("data", (chunk) => (logs += String(chunk)));
    await until(async () => {
      if (child?.exitCode !== null) throw new Error(`GraphJin exited:\n${logs}`);
      return fetch(endpoint, { method: "POST", headers: { "content-type": "application/json" }, body: '{"query":"{ __typename }"}' }).then(() => true, () => false);
    }, `GraphJin start\n${logs}`);
    await db().insert(organization).values({ id: orgId, name: "Watcher parity" });
    await db().insert(data_source).values({ org_id: orgId, kind: "graphjin", graphql_url: `http://127.0.0.1:${port}`, name: "shop", is_default: true });
  }, 120_000);

  afterAll(async () => {
    if (child && child.exitCode === null) {
      const running = child;
      await new Promise<void>((resolve) => {
        running.once("exit", () => resolve());
        running.kill("SIGTERM");
      });
    }
    await new Promise<void>((resolve) => (admin ? admin.close(() => resolve()) : resolve()));
    await db().delete(organization).where(eq(organization.id, orgId));
    await rm(dir, { recursive: true, force: true });
    await rm(configHome, { recursive: true, force: true });
  });

  // watchers.test.ts: "fires the linked workflow when the condition holds, debounced"
  it("fires the linked workflow when the condition holds, debounced", async () => {
    const { workflow } = await saveWorkflow({ orgId, name: "Refund spike response", steps: [{ id: "s1", description: "investigate refunds" }] });
    setValue("refund-rate", 4.2);
    const created = await upsertWatcher({
      orgId,
      workflowId: workflow.id,
      name: "refund-rate",
      query: query("refund-rate"),
      valuePath: "metrics.0.value",
      op: "gt",
      threshold: 2,
      cadenceSeconds: 60,
      debounceSeconds: 3600,
      severity: "high",
    });
    await db().update(watcher).set({ cadence_seconds: POLL_SECONDS }).where(eq(watcher.id, created.id));
    expect(await reconcileWatcherWatches(orgId, deps()), logs).toMatchObject({ synced: 1, failed: 0 });

    // First check fires once.
    await valueLands(created.id, 4.2);
    await until(async () => firesFor(created.id).length === 1, "first fire");
    expect(firesFor(created.id)[0]).toMatchObject({ workflowId: workflow.id, triggerKind: "watcher", value: 4.2 });
    const first = (await polledAt(created.id))!;

    // Cadence not elapsed: not checked again.
    await sleep(3_000);
    expect(await polledAt(created.id)).toBe(first);

    // Cadence elapsed but inside debounce: checked, no fire.
    const second = await nextPoll(created.id, first);
    expect(second - first).toBeGreaterThanOrEqual((POLL_SECONDS - 1) * 1000);
    expect(firesFor(created.id)).toHaveLength(1);
    // The watcher's last check is GraphJin's last poll.
    await reconcileWatcherWatches(orgId, deps());
    expect((await row(created.id)).last_checked_at!.getTime()).toBeGreaterThanOrEqual(second);
  }, 120_000);

  // watchers.test.ts: "reports every change, including a return to an earlier value, and skips repeats"
  it("reports every change, including a return to an earlier value, and skips repeats", async () => {
    const { workflow } = await saveWorkflow({ orgId, name: "Stock response", steps: [{ id: "s1", description: "check stock" }] });
    const values = [120, 125, 120, 120];
    const ids: Record<string, string> = {};
    for (const [op, threshold] of [["changed", null], ["gt", 100]] as const) {
      const name = `stock-${op}`;
      setValue(name, values[0]!);
      const created = await upsertWatcher({
        orgId,
        workflowId: workflow.id,
        name,
        query: query(name),
        valuePath: "metrics.0.value",
        op,
        ...(threshold === null ? {} : { threshold }),
        cadenceSeconds: 60,
        debounceSeconds: 0,
      });
      await db().update(watcher).set({ cadence_seconds: POLL_SECONDS }).where(eq(watcher.id, created.id));
      ids[op] = created.id;
    }
    expect(await reconcileWatcherWatches(orgId, deps())).toMatchObject({ failed: 0 });
    for (const id of Object.values(ids)) await valueLands(id, values[0]!);
    for (let i = 1; i < values.length; i++) {
      const before = Math.max(...(await Promise.all(Object.values(ids).map(async (id) => (await polledAt(id)) ?? 0))));
      for (const op of Object.keys(ids)) setValue(`stock-${op}`, values[i]!);
      for (const id of Object.values(ids)) {
        if (values[i] !== values[i - 1]) await valueLands(id, values[i]!);
        else await nextPoll(id, before);
      }
    }
    expect(firesFor(ids.changed!).map((fire) => fire.value)).toEqual([125, 120]);
    expect(firesFor(ids.gt!).map((fire) => fire.value)).toEqual([120, 125, 120]);
  }, 240_000);

  // watchers.test.ts: "query errors are recorded and never fire"
  it("query errors are recorded and never fire", async () => {
    const { workflow } = await saveWorkflow({ orgId, name: "Broken response", steps: [{ id: "s1", description: "never runs" }] });
    // A query GraphJin refuses outright.
    const refused = await upsertWatcher({ orgId, workflowId: workflow.id, name: "broken-watch", query: "query broken { nope { value } }", valuePath: "nope.0.value", op: "gt", threshold: 0, cadenceSeconds: 60 });
    // A query that fails later, once its table is gone.
    const later = await upsertWatcher({ orgId, workflowId: workflow.id, name: "broken-at-runtime", query: `query later { broken_metrics(where: { name: { eq: "broken-at-runtime" } }) { value } }`, valuePath: "broken_metrics.0.value", op: "gt", threshold: 100, cadenceSeconds: 60 });
    await db().update(watcher).set({ cadence_seconds: POLL_SECONDS }).where(eq(watcher.id, later.id));
    const result = await reconcileWatcherWatches(orgId, deps());
    expect(result.failed).toBeGreaterThanOrEqual(1);
    expect((await row(refused.id)).last_error).toMatch(/nope/u);
    await valueLands(later.id, 1);
    const firstPoll = (await polledAt(later.id))!;
    sqlite("DROP TABLE broken_metrics;");
    await until(async () => {
      await reconcileWatcherWatches(orgId, deps());
      return (await row(later.id)).last_error;
    }, `runtime error for broken-at-runtime\n${logs}`, 60_000);
    expect((await polledAt(later.id))!).toBeGreaterThan(firstPoll);
    expect(firesFor(refused.id)).toHaveLength(0);
    expect(firesFor(later.id)).toHaveLength(0);
  }, 120_000);

  // watchers.test.ts: "saveWorkflowWithTrigger persists a watch trigger" -- and it runs.
  it("saveWorkflowWithTrigger persists a watch trigger that GraphJin then runs", async () => {
    setValue("coverage", 9);
    const result = await saveWorkflowWithTrigger({
      orgId,
      name: "Stock coverage response",
      steps: [{ id: "s1", description: "reorder" }],
      triggers: { watch: { query: query("coverage"), value_path: "metrics.0.value", op: "lt", threshold: 14 } },
    });
    expect(result.triggerError).toBeUndefined();
    expect(result.watcher).toMatchObject({ name: "Stock coverage response", op: "lt", threshold: 14, workflowId: result.workflow.id });
    await db().update(watcher).set({ cadence_seconds: POLL_SECONDS }).where(eq(watcher.id, result.watcher!.id));
    await reconcileWatcherWatches(orgId, deps());
    await until(async () => firesFor(result.watcher!.id).length === 1, `coverage fire\n${logs}`);
    expect(firesFor(result.watcher!.id)[0]).toMatchObject({ workflowId: result.workflow.id, value: 9 });
  }, 120_000);

  // eval case w02: create, fire, cadence, debounce, refire, update, disable, reset, delete.
  it("runs the watcher lifecycle", async () => {
    const { workflow } = await saveWorkflow({ orgId, name: "Threshold response", steps: [{ id: "investigate", description: "Investigate the threshold" }] });
    setValue("threshold", 12);
    const created = await upsertWatcher({ orgId, workflowId: workflow.id, name: "threshold", query: query("threshold"), valuePath: "metrics.0.value", op: "gt", threshold: 10, cadenceSeconds: 60, debounceSeconds: 3_600, severity: "high" });
    expect(created.enabled && created.op === "gt").toBe(true);
    await db().update(watcher).set({ cadence_seconds: POLL_SECONDS }).where(eq(watcher.id, created.id));
    await reconcileWatcherWatches(orgId, deps());

    // firstFire, evidencePreserved
    await until(async () => firesFor(created.id).length === 1, "first fire");
    expect(firesFor(created.id)[0]).toMatchObject({ workflowId: workflow.id, value: 12 });
    // cadenceHonored
    const first = (await polledAt(created.id))!;
    await sleep(3_000);
    expect(await polledAt(created.id)).toBe(first);
    // debounceHonored: a new value inside the window does not fire.
    setValue("threshold", 13);
    await valueLands(created.id, 13);
    expect(firesFor(created.id)).toHaveLength(1);
    // refiresAfterWindow
    clockOffsetMs = 3_662_000;
    setValue("threshold", 14);
    await valueLands(created.id, 14);
    await until(async () => firesFor(created.id).length === 2, "refire");
    clockOffsetMs = 0;

    // updated: the same watcher, and the same GraphJin watch with the new cadence.
    const watchId = (await row(created.id)).graphjin_watch_id;
    const updated = await upsertWatcher({ orgId, workflowId: workflow.id, name: "threshold", query: query("threshold"), valuePath: "metrics.0.value", op: "gte", threshold: 11, cadenceSeconds: 120, debounceSeconds: 3_600, severity: "critical" });
    expect(updated).toMatchObject({ id: created.id, op: "gte", threshold: 11, severity: "critical" });
    await reconcileWatcherWatches(orgId, deps());
    expect((await row(created.id)).last_error).toBeNull();
    expect((await row(created.id)).graphjin_watch_id).toBe(watchId);

    // disabled: GraphJin stops polling it and nothing fires.
    expect((await setWatcherEnabled(orgId, created.id, false))?.enabled).toBe(false);
    expect((await reconcileWatcherWatches(orgId, deps())).removed).toBeGreaterThanOrEqual(1);
    expect(await graphjinWatcherWatchClient.status({ endpoint, orgId, watchId: watchId! })).toBeNull();
    setValue("threshold", 99);
    await sleep((POLL_SECONDS + 2) * 1000);
    expect((await row(created.id)).last_value).toBe(14);
    expect(firesFor(created.id)).toHaveLength(2);

    // reset
    expect(await resetWatcher(orgId, created.id)).toMatchObject({ lastCheckedAt: null, lastFiredAt: null, lastValue: null, lastError: null });

    // deleted: an enabled watcher's GraphJin watch goes with it.
    setValue("short-lived", 1);
    const doomed = await upsertWatcher({ orgId, workflowId: workflow.id, name: "short-lived", query: query("short-lived"), valuePath: "metrics.0.value", op: "gt", threshold: 0, cadenceSeconds: 60 });
    await reconcileWatcherWatches(orgId, deps());
    const doomedWatch = (await row(doomed.id)).graphjin_watch_id!;
    expect(await deleteWatcher(orgId, doomed.id)).toBe(true);
    expect(await getWatcher(orgId, doomed.id)).toBeNull();
    await reconcileWatcherWatches(orgId, deps());
    expect(await graphjinWatcherWatchClient.status({ endpoint, orgId, watchId: doomedWatch })).toBeNull();
    expect((await db().select().from(watcher).where(and(eq(watcher.org_id, orgId), eq(watcher.name, "short-lived")))).length).toBe(0);
  }, 240_000);
});
