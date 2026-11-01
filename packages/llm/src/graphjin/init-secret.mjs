import pg from "pg";
import { buildPoolConfig, formatDatabaseUrl, resolveDatabaseTarget } from "@neko/db/connection";
import { mkdir, readFile } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import { dirname, join } from "node:path";

import {
  atomicWriteFile,
  graphjinKeystoreHasValues,
  migrateGraphjinSystemSource,
  patchGraphjinKeystoreKey,
  LIBRARY_SOURCE_ROOT,
  patchGraphjinLibrary,
  patchGraphjinSemanticSearch,
  patchGraphjinWatches,
  watcherWebhookUrl,
  patchGraphjinSourcesJwtSecret,
  reconcileGraphjinWritePolicy,
} from "./sources-config.ts";

// One-shot entrypoint for the graphjin-secret-init compose service.
//
// GraphJin sources mode signs actor tokens with a per-org secret derived as
// HMAC(deployment secret-key, "graphjin:<orgId>"). GraphJin 3.18 cannot be
// relied on to hot-reload auth keys, so the secret must be correct in
// agentic.yml BEFORE the server's first start — which previously forced
// `graphjin` to wait on a healthy worker and created a dependency cycle with
// the demo/dev overlays' `worker -> graphjin` edge. This script needs only
// the migrated database (for the org row) and the config volume (for the
// secret-key), so it runs right after neko-migrate, ahead of both sides.
//
// It reconciles UNCONDITIONALLY on every run — placeholder or stale concrete
// secret alike — so partial volume wipes, secret-key rotation, and restores
// self-heal on the next stack start. The worker's own boot pass stays as a
// second line of defense and normally finds nothing to do.

// Empty (explicitly set to "") means the stack runs an anonymous GraphJin
// config with no JWT secret to reconcile — the one-shot then only
// serializes org-row creation. Unset falls back to the packaged default.
const CONFIG_PATH =
  process.env.GRAPHJIN_SOURCES_CONFIG !== undefined
    ? process.env.GRAPHJIN_SOURCES_CONFIG.trim()
    : "/config/graphjin/agentic.yml";

// The subscriptions GraphJin in `openneko serve` reads neko-db as the
// read-only neko_subscriptions role (migration 0128). Its password is derived
// from the secret key; this sets it on the role and hands serve the role's
// URL in a file beside the GraphJin config, so serve never holds the secret
// key or the neko password. The password file stays for older serve builds.
const SUBSCRIPTIONS_PASSWORD_FILE =
  process.env.OPENNEKO_SUBSCRIPTIONS_PASSWORD_FILE?.trim() ||
  (CONFIG_PATH ? join(dirname(CONFIG_PATH), "subscriptions.password") : "");

async function reconcileSubscriptionsRole(client, target) {
  if (!SUBSCRIPTIONS_PASSWORD_FILE) return;
  const { subscriptionsRolePassword } = await import("./token.ts");
  const password = subscriptionsRolePassword();
  await client.query(`alter role neko_subscriptions with login password '${password}'`);
  const folder = dirname(SUBSCRIPTIONS_PASSWORD_FILE);
  await mkdir(folder, { recursive: true, mode: 0o700 });
  const url = formatDatabaseUrl({ ...target, user: "neko_subscriptions", password });
  await atomicWriteFile(join(folder, "subscriptions.url"), `${url}\n`, { mode: 0o600 });
  await atomicWriteFile(SUBSCRIPTIONS_PASSWORD_FILE, `${password}\n`, { mode: 0o600 });
  console.log(`[graphjin-secret-init] set the subscriptions role password in ${SUBSCRIPTIONS_PASSWORD_FILE}`);
}

const target = resolveDatabaseTarget();
const client = new pg.Client(buildPoolConfig());

await client.connect();

let orgId;
// The admin "Semantic search" switch (off with no row). Same query as
// readSemanticSearchEnabled() in @neko/db.
let semanticSearch = false;
try {
  // Same transaction-scoped advisory lock as getOrgId(); whichever process
  // takes it first creates the single org row, everyone else re-reads it.
  await client.query("begin");
  await client.query(
    "select pg_advisory_xact_lock(hashtext('openneko.organization.bootstrap'))",
  );
  const existing = await client.query(
    "select id from organization order by created_at asc, id asc limit 1",
  );
  if (existing.rows[0]?.id) {
    orgId = existing.rows[0].id;
  } else {
    orgId = randomUUID();
    await client.query(
      "insert into organization (id, name) values ($1, $2)",
      [orgId, "My Workspace"],
    );
    console.log(`[graphjin-secret-init] created organization ${orgId}`);
  }
  await client.query("commit");
  const switchRow = await client.query(
    "select enabled from llm_provider_config where scope = 'semantic-search' order by updated_at desc limit 1",
  );
  semanticSearch = switchRow.rows[0]?.enabled === true;
  await reconcileSubscriptionsRole(client, target);
} catch (e) {
  await client.query("rollback").catch(() => {});
  throw e;
} finally {
  await client.end();
}

if (!CONFIG_PATH) {
  console.log(
    `[graphjin-secret-init] no sources config for this stack; org ${orgId} ready, nothing to patch`,
  );
  process.exit(0);
}

// token.ts is dependency-light (node:crypto + @neko/secret-crypt); importing
// it here keeps the secret derivation single-sourced with the worker's.
const { graphjinKeystoreKeyB64, graphjinSigningSecretB64 } = await import("./token.ts");
const secret = graphjinSigningSecretB64(orgId);

const raw = await readFile(CONFIG_PATH, "utf8");
const migrated = migrateGraphjinSystemSource(raw);
const patched = patchGraphjinSourcesJwtSecret(migrated.content, secret);
const policy = reconcileGraphjinWritePolicy(patched.content);
// GraphJin is not running yet, so this is the one safe moment to change its
// keystore key. A replaced key moves to previous_keys and GraphJin re-seals.
const keystore = patchGraphjinKeystoreKey(
  policy.content,
  graphjinKeystoreKeyB64(),
  await graphjinKeystoreHasValues(CONFIG_PATH, policy.content),
);
// OpenNeko watchers run as GraphJin watches that call the worker back.
const watches = patchGraphjinWatches(keystore.content, watcherWebhookUrl());
// The Library is a GraphJin file source; its root must exist before start.
const library = patchGraphjinLibrary(watches.content);
const libraryRoot = join(dirname(CONFIG_PATH), LIBRARY_SOURCE_ROOT);
await mkdir(libraryRoot, { recursive: true, mode: 0o700 });
// Semantic catalog and Library search follow the admin switch; flips while
// GraphJin runs are applied live through gj_config.
const semantic = patchGraphjinSemanticSearch(library.content, semanticSearch);
if (migrated.changed || patched.changed || policy.changed || keystore.changed || watches.changed || library.changed || semantic.changed) {
  await atomicWriteFile(CONFIG_PATH, semantic.content);
}
if (semantic.changed) {
  console.log(
    `[graphjin-secret-init] set semantic search ${semanticSearch ? "on" : "off"} in ${CONFIG_PATH}`,
  );
}
if (library.changed) {
  console.log(`[graphjin-secret-init] added the Library file source to ${CONFIG_PATH}`);
}
if (watches.changed) {
  console.log(`[graphjin-secret-init] turned on GraphJin watches for OpenNeko watchers in ${CONFIG_PATH}`);
}
if (keystore.changed) {
  console.log(
    `[graphjin-secret-init] set the GraphJin keystore key derived from the OpenNeko secret key in ${CONFIG_PATH}`,
  );
}
if (policy.changed) {
  console.log(
    `[graphjin-secret-init] reconciled GraphJin write policy (mutations on, worker roles listed) in ${CONFIG_PATH}`,
  );
}
if (migrated.changed) {
  console.log(
    `[graphjin-secret-init] migrated legacy GraphJin system source in ${CONFIG_PATH}`,
  );
}
if (patched.changed) {
  console.log(
    `[graphjin-secret-init] reconciled per-org JWT secret in ${CONFIG_PATH} (org ${orgId})`,
  );
} else if (!migrated.changed) {
  console.log(
    `[graphjin-secret-init] per-org JWT secret already current in ${CONFIG_PATH} (org ${orgId})`,
  );
}
