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
  const home = make(path.join(os.tmpdir(), "gj-watchers-xdg-"));
  process.env.XDG_CONFIG_HOME = home;
  return home;
});

import { data_source, db, eq, organization, pool, watcher } from "@neko/db";
import { graphjinKeystoreKeyB64, graphjinSigningSecretB64 } from "@neko/llm/graphjin";
import { patchGraphjinWatches } from "@neko/llm/graphjin/sources-config";
import {
  graphjinWatcherWatchClient,
  receiveWatcherEvent,
  reconcileWatcherWatches,
  saveWorkflow,
  upsertWatcher,
  watcherForGraphjinWatch,
  watcherWebhookSignatureValid,
} from "@neko/llm/workflows";
import { createAdminHandler } from "../src/admin-server.js";

/**
 * A watcher end to end on a GraphJin binary: reconcile creates a polled
 * gj_watch, GraphJin runs the aggregate and sends a keystore-signed webhook
 * to the worker's admin route, and the condition fires the workflow. Set
 * OPENNEKO_TEST_GRAPHJIN_BIN to run it.
 */
const bin = process.env.OPENNEKO_TEST_GRAPHJIN_BIN;
const reachable = bin ? await pool().query("select 1").then(() => true, () => false) : false;

async function freePort(): Promise<number> {
  return new Promise((resolve) => {
    const server = createServer();
    server.listen(0, "127.0.0.1", () => {
      const port = (server.address() as { port: number }).port;
      server.close(() => resolve(port));
    });
  });
}

async function until<T>(check: () => Promise<T | null | undefined | false>, label: string, ms = 60_000): Promise<T> {
  const deadline = Date.now() + ms;
  for (;;) {
    const value = await check();
    if (value) return value;
    if (Date.now() > deadline) throw new Error(`timed out waiting for ${label}`);
    await new Promise((resolve) => setTimeout(resolve, 500));
  }
}

(reachable ? describe : describe.skip)("watchers on GraphJin watches (live)", () => {
  const orgId = `gj-watch-${Date.now().toString(36)}`;
  const dir = mkdtempSync(join(tmpdir(), "gj-watchers-"));
  const dbPath = join(dir, "shop.db");
  let child: ChildProcess | null = null;
  let admin: Server | null = null;
  let endpoint = "";
  let webhookUrl = "";
  let logs = "";
  const fires: Array<Record<string, unknown>> = [];
  const received: Array<{ status: number }> = [];

  beforeAll(async () => {
    const [port, adminPort] = [await freePort(), await freePort()];
    endpoint = `http://127.0.0.1:${port}/api/v1/graphql`;
    webhookUrl = `http://localhost:${adminPort}/admin/events/watcher`;

    // The worker's admin route, wired as index.ts wires it.
    admin = createHttpServer(createAdminHandler({
      watcherEvents: {
        verify: (raw, signature, watchId) => watcherWebhookSignatureValid(raw, signature, watchId),
        receive: async ({ watchId, event }) => {
          const row = await watcherForGraphjinWatch(watchId);
          if (!row) return { accepted: false };
          const { fired } = await receiveWatcherEvent(row, event, {
            enqueueFire: async (payload) => {
              fires.push(payload);
            },
          });
          return { accepted: true, fired };
        },
      },
    }));
    admin.on("request", (_req, res) => res.on("finish", () => received.push({ status: res.statusCode })));
    await new Promise<void>((resolve) => admin!.listen(adminPort, "127.0.0.1", resolve));

    execFileSync("sqlite3", [dbPath, `CREATE TABLE orders (id INTEGER PRIMARY KEY, account_id TEXT NOT NULL, status TEXT NOT NULL);
      INSERT INTO orders VALUES (1, '${orgId}', 'late'), (2, 'other', 'late');`]);
    // The customer config as shipped, then patched as graphjin-secret-init patches it.
    const base = `app_name: OpenNeko watcher test
mode: agentic
host_port: 127.0.0.1:${port}
production: true
disable_production_security: true
log_level: warn
subs_poll_duration: 1s
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

    await db().insert(organization).values({ id: orgId, name: "Watcher live" });
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

  it("polls the aggregate, delivers signed events, and fires on the threshold", async () => {
    const { workflow } = await saveWorkflow({ orgId, name: "Late orders", steps: [{ id: "s1", description: "chase late orders" }] });
    await upsertWatcher({
      orgId,
      workflowId: workflow.id,
      name: "late-orders",
      query: `query late_orders { orders(where: { status: { eq: "late" } }) { count_id } }`,
      valuePath: "orders.0.count_id",
      op: "gt",
      threshold: 1,
      cadenceSeconds: 60,
      debounceSeconds: 0,
    });

    const result = await reconcileWatcherWatches(orgId, { webhookUrl });
    const [row] = await db().select().from(watcher).where(eq(watcher.org_id, orgId));
    expect(result, `${row?.last_error}\n${logs}`).toEqual({ synced: 1, removed: 0, failed: 0 });
    expect(row?.graphjin_watch_id).toMatch(/^watch:/);

    // The first poll sees one late order for this org (the other org's row is
    // filtered by the namespace): the value is recorded, the condition is not met.
    await until(async () => {
      const [current] = await db().select().from(watcher).where(eq(watcher.org_id, orgId));
      return current?.last_value === 1;
    }, `first event\n${logs}`);
    expect(fires).toHaveLength(0);
    expect(received.every((r) => r.status === 202)).toBe(true);

    // A second late order crosses the threshold on a later poll. The poll
    // interval is 60 seconds, so restart the watch to poll now.
    execFileSync("sqlite3", [dbPath, `INSERT INTO orders VALUES (3, '${orgId}', 'late');`]);
    await db().update(watcher).set({ cadence_seconds: 61 }).where(eq(watcher.org_id, orgId));
    expect(await reconcileWatcherWatches(orgId, { webhookUrl })).toMatchObject({ synced: 1 });
    await until(async () => fires.length > 0, "fire", 90_000).catch(async (error) => {
      const [current] = await db().select().from(watcher).where(eq(watcher.org_id, orgId));
      const watch = await graphjinWatcherWatchClient.status({ endpoint, orgId, watchId: current!.graphjin_watch_id! }).catch((e) => String(e));
      throw new Error(`${error.message}: received=${JSON.stringify(received)} row=${JSON.stringify({ last_value: current?.last_value, last_error: current?.last_error, last_checked_at: current?.last_checked_at })} watch=${JSON.stringify(watch)}\n${logs}`);
    });
    expect(fires[0]).toMatchObject({ workflowId: workflow.id, triggerKind: "watcher", triggerPayload: { value: 2 } });
  }, 240_000);

  it("rejects an event that is not signed with the watch's secret", async () => {
    const [row] = await db().select().from(watcher).where(eq(watcher.org_id, orgId));
    const body = JSON.stringify({ watch: { id: row!.graphjin_watch_id }, event: { id: "we:forged", watch_id: row!.graphjin_watch_id, data_json: { orders: [{ count_id: 99 }] } } });
    const response = await fetch(webhookUrl, {
      method: "POST",
      headers: { "content-type": "application/json", "idempotency-key": "we:forged", "x-graphjin-signature": "sha256=00" },
      body,
    });
    expect(response.status).toBe(401);
  });
});
