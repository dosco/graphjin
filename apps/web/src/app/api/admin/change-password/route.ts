/**
 * POST /api/admin/change-password
 *
 * Bootstraps the admin's chosen DB password on first run. Steps:
 *   1. Rotate the `neko` role's password.
 *   2. Persist it to `~/.config/openneko/config.json` so every process and
 *      generated GraphJin config picks it up.
 *   3. Drain the web pool so subsequent queries use the new password.
 *   4. Fire-and-forget POST /admin/reconnect to the worker so its
 *      pg-boss singleton (which holds the OLD credentials in its own
 *      pool, separate from the web's @neko/db pool) restarts with fresh
 *      creds. WORKER_ADMIN_URL is set by Docker Compose; local dev falls
 *      back to localhost. Tolerates the worker being down — the password
 *      still rotates successfully on the web side.
 *
 * The body must contain a non-empty password. The default `"secret"` is
 * rejected — we want a real change. Length minimum is intentionally
 * loose (8 chars) since this is a host-local DB password, not a public
 * credential; the operator picks the policy.
 */

import { NextRequest, NextResponse } from "next/server";
import {
  hasCustomPassword,
  pool,
  reconnectPool,
  writeLocalConfig,
} from "@neko/db";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";

const MIN_LENGTH = 8;
const FORBIDDEN = new Set(["secret", "password", "postgres"]);

function isPlainPasswordSafe(password: string): boolean {
  // ALTER USER ... PASSWORD '...' takes a plain-text literal. Postgres
  // hashes it server-side. We refuse single quotes and backslashes so
  // the string can't escape the literal context. Most password managers
  // generate alnum + symbol passwords without these characters.
  return !/['\\\n\r\0]/.test(password);
}

async function rotateDatabaseRole(password: string): Promise<void> {
  const metadata = await pool().connect();
  try {
    // Session-level advisory lock held across BOTH the role rotation and
    // the config-file write below (released in the finally). Two
    // concurrent rotations used to interleave: ALTER order A,B but
    // config-write order B,A left the file holding one password and the
    // role the other — a full-stack lockout recoverable only out-of-band.
    await metadata.query(
      "select pg_advisory_lock(hashtext('openneko.password-rotation'))",
    );
    await metadata.query(`alter role neko with password '${password}'`);
    // Persist while still inside the critical section, so the file always
    // records the password of the LAST committed rotation.
    writeLocalConfig({ pg: { password } });
  } finally {
    await metadata
      .query("select pg_advisory_unlock(hashtext('openneko.password-rotation'))")
      .catch(() => {});
    metadata.release();
  }
}

export async function POST(request: NextRequest) {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  try {
    const body = (await request.json().catch(() => ({}))) as { password?: unknown };
    const password = typeof body.password === "string" ? body.password.trim() : "";

    if (password.length < MIN_LENGTH) {
      return NextResponse.json(
        { error: `Password must be at least ${MIN_LENGTH} characters.` },
        { status: 400 },
      );
    }
    if (FORBIDDEN.has(password.toLowerCase())) {
      return NextResponse.json(
        { error: "That password is too common. Pick something else." },
        { status: 400 },
      );
    }
    if (!isPlainPasswordSafe(password)) {
      return NextResponse.json(
        { error: "Password contains characters we can't safely apply (quotes, backslashes, newlines)." },
        { status: 400 },
      );
    }

    // ALTER ROLE takes a literal; we vetted the string above because Postgres
    // does not accept a bind parameter in this DDL position. The rotation
    // also persists the config file inside its critical section.
    await rotateDatabaseRole(password);

    // Drain the pool so subsequent queries use the new password.
    await reconnectPool();

    // Tell the worker process to restart so its pg-boss singleton
    // (created at boot with old creds) gets rebuilt. The worker may
    // legitimately not be running yet during /setup — but a SILENT drop
    // here left pg-boss on stale credentials with jobs quietly stalled,
    // so the outcome is reported to the caller and logged.
    let workerNotified = false;
    try {
      const workerAdminUrl = (process.env.WORKER_ADMIN_URL ?? "http://127.0.0.1:4100")
        .replace(/\/+$/, "");
      const res = await fetch(`${workerAdminUrl}/admin/reconnect`, {
        method: "POST",
        signal: AbortSignal.timeout(5000),
      });
      workerNotified = res.ok;
    } catch {
      workerNotified = false;
    }
    if (!workerNotified) {
      console.warn(
        "[change-password] worker reconnect notification failed; its job runner keeps the old credentials until it restarts",
      );
    }

    if (process.env.OPENNEKO_RESTART_WEB_ON_PASSWORD_CHANGE === "1") {
      setTimeout(() => process.exit(0), 250).unref();
    }

    return NextResponse.json({ ok: true, workerNotified });
  } catch (e) {
    const message = e instanceof Error ? e.message : String(e);
    return NextResponse.json({ error: message }, { status: 500 });
  }
}

/**
 * GET /api/admin/change-password — small status endpoint so the wizard
 * knows whether the password has already been changed (config file
 * has pg.password) without leaking the value.
 */
export async function GET() {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  return NextResponse.json({ changed: hasCustomPassword() });
}
