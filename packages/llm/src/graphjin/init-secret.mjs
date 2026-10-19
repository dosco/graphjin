import pg from "pg";
import { readFile } from "node:fs/promises";
import { randomUUID } from "node:crypto";
import { homedir } from "node:os";
import { join } from "node:path";

import {
  atomicWriteFile,
  graphjinKeystoreHasValues,
  migrateGraphjinSystemSource,
  patchGraphjinKeystoreKey,
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

function configBase() {
  const xdg = process.env.XDG_CONFIG_HOME?.trim();
  return xdg && xdg.length > 0
    ? xdg
    : join(process.env.HOME || homedir(), ".config");
}

async function readLocalConfig() {
  const paths = [
    join(configBase(), "openneko", "config.json"),
    join(configBase(), "neko", "config.json"),
  ];
  for (const path of paths) {
    try {
      const parsed = JSON.parse(await readFile(path, "utf8"));
      if (parsed && typeof parsed === "object") return parsed;
    } catch {
      // Missing or malformed local config; try the next path.
    }
  }
  return {};
}

const localConfig = await readLocalConfig();
const localPg =
  localConfig.pg && typeof localConfig.pg === "object" ? localConfig.pg : {};
if (typeof localPg.password === "string" && localPg.password.startsWith("enc:")) {
  const { maybeDecryptSecret } = await import("@neko/secret-crypt");
  localPg.password = maybeDecryptSecret(localPg.password);
}
// OPENNEKO_PG_ENV_OVERRIDE=1 puts set environment values ahead of
// config.json, as @neko/db does for host processes in development.
const envFirst = process.env.OPENNEKO_PG_ENV_OVERRIDE?.trim() === "1";
const setting = (fromConfig, envName, fallback) => {
  const fromEnv = process.env[envName]?.trim() || undefined;
  return (envFirst ? fromEnv ?? fromConfig : fromConfig ?? fromEnv) ?? fallback;
};
const sslmode = setting(localPg.sslmode, "NEKO_PG_SSLMODE", undefined);
const client = new pg.Client({
  host: setting(localPg.host, "NEKO_PG_HOST", "localhost"),
  port: Number(setting(localPg.port, "NEKO_PG_PORT", 5432)),
  user: setting(localPg.user, "NEKO_PG_USER", "neko"),
  password: setting(localPg.password, "NEKO_PG_PASSWORD", "secret"),
  database: setting(localPg.database, "NEKO_PG_DATABASE", "neko"),
  ssl: sslmode === "require" ? { rejectUnauthorized: false } : undefined,
});

await client.connect();

let orgId;
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
if (migrated.changed || patched.changed || policy.changed || keystore.changed) {
  await atomicWriteFile(CONFIG_PATH, keystore.content);
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
