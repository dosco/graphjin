/**
 * Per-file config-dir isolation for the whole suite.
 *
 * Modules persist state next to the deployment config (the secret-key,
 * the auth-gate marker) via XDG_CONFIG_HOME. Tests must never read the
 * machine's real ~/.config nor leak such state between test files — a
 * marker written by one file's live-gate test would flip another file's
 * requireAdminActor to fail closed.
 */
import { mkdtempSync } from "node:fs";
import { tmpdir } from "node:os";
import { join } from "node:path";

process.env.XDG_CONFIG_HOME = mkdtempSync(join(tmpdir(), "neko-test-config-"));
