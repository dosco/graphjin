/**
 * Durable memory of the auth gate, shared by the proxy and lib/auth.
 *
 * Both ask the worker whether an SSO plugin is installed, and both used to
 * treat "worker unreachable" as "no plugin" — which meant every worker boot
 * or restart window silently dropped the sign-in gate and admitted
 * unauthenticated requests as the solo operator. The fix: whenever a live
 * answer arrives, persist it next to the deployment config. When the worker
 * is unreachable, the last persisted answer decides — an install that has
 * ever seen SSO live fails CLOSED (sessions keep verifying locally; new
 * sign-ins wait for the worker), while an install that never configured SSO
 * keeps the solo-operator behavior.
 *
 * The two callers also kept independent 1s caches, only one of which was
 * invalidated when SSO settings changed; they both register their reset
 * here so one invalidation clears everything.
 */

import { mkdirSync, readFileSync, renameSync, writeFileSync } from "node:fs";
import { homedir } from "node:os";
import { join } from "node:path";

export type PersistedAuthProvider = {
  pluginName: string;
  [key: string]: unknown;
};

export type AuthGateMarker = { provider: PersistedAuthProvider | null };

function markerPath(): string {
  const xdg = process.env.XDG_CONFIG_HOME?.trim();
  const base = xdg && xdg.length > 0 ? xdg : join(process.env.HOME || homedir(), ".config");
  return join(base, "openneko", "auth-gate.json");
}

let cachedMarker: AuthGateMarker | null | undefined;

/** Last persisted gate answer; null when no answer was ever persisted. */
export function readAuthGateMarker(): AuthGateMarker | null {
  if (cachedMarker !== undefined) return cachedMarker;
  try {
    const parsed = JSON.parse(readFileSync(markerPath(), "utf8")) as AuthGateMarker;
    cachedMarker =
      parsed && typeof parsed === "object" && "provider" in parsed ? parsed : null;
  } catch {
    cachedMarker = null;
  }
  return cachedMarker;
}

/**
 * Persist a live gate answer. Best-effort: a read-only config volume must
 * never break the request path — the marker then just stays stale.
 */
export function writeAuthGateMarker(marker: AuthGateMarker): void {
  const previous = readAuthGateMarker();
  const unchanged =
    previous !== null &&
    (previous.provider?.pluginName ?? null) === (marker.provider?.pluginName ?? null);
  cachedMarker = marker;
  if (unchanged) return;
  try {
    const path = markerPath();
    mkdirSync(join(path, ".."), { recursive: true });
    const tmp = `${path}.tmp-${process.pid}`;
    writeFileSync(tmp, JSON.stringify(marker), { encoding: "utf8", mode: 0o600 });
    renameSync(tmp, path);
  } catch {
    // Best-effort by design.
  }
}

const cacheResets: Array<() => void> = [];

/** Callers with an in-memory gate cache register its reset here. */
export function registerAuthGateCacheReset(reset: () => void): void {
  cacheResets.push(reset);
}

/** Clear every registered gate cache plus the marker's in-process copy. */
export function resetAuthGateCaches(): void {
  cachedMarker = undefined;
  for (const reset of cacheResets) reset();
}
