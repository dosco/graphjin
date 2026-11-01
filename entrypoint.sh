#!/bin/sh
# Container entrypoint. Seeds the config files Neko expects, then exec's
# the real CMD. Existing files win by default so first-run setup changes
# (DB password rotation, encryption key) survive container restarts.
#
# Required env to bootstrap from a fresh container:
#   NEKO_PG_HOST          Postgres host (or socket path starting with /)
#   NEKO_PG_PASSWORD      Postgres user password
#   NEKO_SECRET_KEY       base64 32-byte at-rest encryption key
#
# Optional env (sane defaults baked in):
#   NEKO_PG_PORT          default 5432
#   NEKO_PG_USER          default neko
#   NEKO_PG_DATABASE      default neko
#   NEKO_PG_SSLMODE       default unset (no TLS); set to "require" for
#                         managed Postgres with self-signed CAs
#                         (e.g. Cloud SQL public IP, RDS, etc.)
#
# When no env vars are set, the app falls back to its local dev
# defaults (host=localhost, password=secret, database=neko). The /setup
# wizard handles password rotation in that mode.
set -eu

CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/openneko"
mkdir -p \
  "$CONFIG_DIR" \
  "${XDG_CONFIG_HOME:-$HOME/.config}/graphjin" \
  "${XDG_CACHE_HOME:-$HOME/.cache}" \
  "${XDG_DATA_HOME:-$HOME/.local/share}" \
  "${XDG_STATE_HOME:-$HOME/.local/state}" \
  "${TMPDIR:-/tmp}"

OPENNEKO_CONFIG_DIR="$CONFIG_DIR" node <<'JS'
const crypto = require("node:crypto");
const fs = require("node:fs");
const path = require("node:path");

const configDir = process.env.OPENNEKO_CONFIG_DIR;
const force = process.env.OPENNEKO_FORCE_CONFIG === "1";
const configPath = path.join(configDir, "config.json");
const secretPath = path.join(configDir, "secret-key");

function readJson(file) {
  try {
    return JSON.parse(fs.readFileSync(file, "utf8"));
  } catch {
    return {};
  }
}

function writePrivate(file, value, flag = "w") {
  fs.writeFileSync(file, value, { encoding: "utf8", flag });
  fs.chmodSync(file, 0o600);
}

const envPg = {};
if (process.env.NEKO_PG_HOST) envPg.host = process.env.NEKO_PG_HOST;
if (process.env.NEKO_PG_PORT) envPg.port = Number(process.env.NEKO_PG_PORT);
if (process.env.NEKO_PG_USER) envPg.user = process.env.NEKO_PG_USER;
if (process.env.NEKO_PG_PASSWORD !== undefined) envPg.password = process.env.NEKO_PG_PASSWORD;
if (process.env.NEKO_PG_DATABASE) envPg.database = process.env.NEKO_PG_DATABASE;
if (process.env.NEKO_PG_SSLMODE) envPg.sslmode = process.env.NEKO_PG_SSLMODE;

if (Object.keys(envPg).length > 0) {
  const current = readJson(configPath);
  const next = {
    ...current,
    pg: { ...(current.pg && typeof current.pg === "object" ? current.pg : {}) },
  };

  for (const [key, value] of Object.entries(envPg)) {
    if (force || next.pg[key] === undefined || next.pg[key] === null || next.pg[key] === "") {
      next.pg[key] = value;
    }
  }

  if (JSON.stringify(current) !== JSON.stringify(next)) {
    writePrivate(configPath, `${JSON.stringify(next, null, 2)}\n`);
  }
}

if (force || !fs.existsSync(secretPath)) {
  const value = process.env.NEKO_SECRET_KEY || crypto.randomBytes(32).toString("base64");
  try {
    writePrivate(secretPath, value, force ? "w" : "wx");
  } catch (e) {
    if (e.code !== "EEXIST") throw e;
  }
}
JS

# Migrations: a dedicated `neko-migrate` compose service (ghcr.io/open-neko/neko-cli)
# runs `openneko migrate` once before web/worker start, gated via
# `depends_on: neko-migrate: service_completed_successfully`. So web and worker
# can trust the schema is in place by the time their entrypoints run and we don't
# repeat the migrate here. Bare-Docker users (no compose orchestration) need to
# run `openneko migrate` against the DB themselves before starting these images.

exec "$@"
