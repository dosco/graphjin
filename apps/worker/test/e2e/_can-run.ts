/**
 * Capability detection for the slow E2E tier.
 *
 * The E2E suite seeds its own throwaway org — it does NOT depend on the
 * dev's `/setup` state. This file just decides which (agent backend,
 * primary provider) combinations the host can exercise.
 *
 * Plan matrix:
 *   - Ax × Google Gemini (GEMINI_API_KEY)
 *   - Ax × Anthropic     (ANTHROPIC_API_KEY)
 *
 * Both keys are independently optional — set one or both. The suite
 * blocks (skips entirely) when GraphJin, the `graphjin` CLI or the agent
 * runtime (`openneko serve` at OPENNEKO_SERVE_SOCKET, started by
 * `pnpm dev:agent`) is missing, since every plan needs them.
 */

import { spawnSync } from "node:child_process";
import { statSync } from "node:fs";

export type AgentBackendId = "ax";
export type PrimaryProviderId = "anthropic" | "google-gemini";

export type RunPlan = {
  /** Stable id used for log lines and the comparison column header. */
  id: string;
  backend: AgentBackendId;
  primaryProvider: PrimaryProviderId;
  primaryModel: string;
  /** Resolved API key value from env. */
  apiKey: string;
};

const DEFAULT_GRAPHQL_URL = "http://localhost:8080/api/v1/graphql";

type PlanSpec = {
  id: string;
  backend: AgentBackendId;
  primaryProvider: PrimaryProviderId;
  primaryModel: string;
  envVar: "GEMINI_API_KEY" | "ANTHROPIC_API_KEY";
};

const PLAN_SPECS: PlanSpec[] = [
  {
    id: "ax+gemini",
    backend: "ax",
    primaryProvider: "google-gemini",
    primaryModel: "gemini-3.6-flash",
    envVar: "GEMINI_API_KEY",
  },
  {
    id: "ax+anthropic",
    backend: "ax",
    primaryProvider: "anthropic",
    primaryModel: "claude-opus-4-7",
    envVar: "ANTHROPIC_API_KEY",
  },
];

function hasOnPath(cmd: string): boolean {
  return spawnSync("which", [cmd], { stdio: "ignore" }).status === 0;
}

/** The agent runner sends every turn to `openneko serve` over this socket. */
function serveSocketReason(): string | null {
  const socket = process.env.OPENNEKO_SERVE_SOCKET?.trim();
  if (!socket) return "OPENNEKO_SERVE_SOCKET is not set (source scripts/dev-env.sh)";
  try {
    if (statSync(socket).isSocket()) return null;
  } catch {
    // Reported below.
  }
  return `openneko serve is not listening on ${socket} (start it with pnpm dev:agent)`;
}

function readEnv(name: string): string | null {
  const v = process.env[name]?.trim();
  return v && v.length > 0 ? v : null;
}

export function e2eGraphqlUrl(): string {
  // Allow override for non-default GraphJin host/port without requiring
  // edits to test source. Local dev's compose stack always exposes 8080.
  return process.env.NEKO_E2E_GRAPHQL_URL?.trim() || DEFAULT_GRAPHQL_URL;
}

async function graphqlReachable(
  url: string,
): Promise<{ ok: true } | { ok: false; reason: string }> {
  try {
    const res = await fetch(url, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ query: "{ __typename }" }),
    });
    if (!res.ok) return { ok: false, reason: `HTTP ${res.status} ${res.statusText}` };
    return { ok: true };
  } catch (e) {
    return { ok: false, reason: e instanceof Error ? e.message : String(e) };
  }
}

export type CanRunResult = {
  /** Plans the host can exercise. May be empty. */
  runnable: RunPlan[];
  /** When non-empty, the suite cannot run at all — log + skip. */
  blockingReasons: string[];
  /** Cached for the test to reuse without a second probe. */
  graphqlUrl: string;
};

export async function detectRunnablePlans(): Promise<CanRunResult> {
  const blocking: string[] = [];
  const graphqlUrl = e2eGraphqlUrl();

  const reach = await graphqlReachable(graphqlUrl);
  if (!reach.ok) {
    blocking.push(
      `GraphQL endpoint ${graphqlUrl} unreachable: ${reach.reason}. Bring up the docker stack: docker compose -f compose.yml -f compose.adventureworks.yml up -d`,
    );
  }

  if (!hasOnPath("graphjin")) {
    blocking.push("`graphjin` CLI not on PATH (brew install dosco/tap/graphjin)");
  }

  const serve = serveSocketReason();
  if (serve) blocking.push(serve);

  if (blocking.length > 0) {
    return { runnable: [], blockingReasons: blocking, graphqlUrl };
  }

  const runnable: RunPlan[] = [];
  for (const spec of PLAN_SPECS) {
    const apiKey = readEnv(spec.envVar);
    if (!apiKey) {
      console.warn(
        `[e2e] ${spec.envVar} not set — skipping plan ${spec.id}`,
      );
      continue;
    }
    runnable.push({
      id: spec.id,
      backend: spec.backend,
      primaryProvider: spec.primaryProvider,
      primaryModel: spec.primaryModel,
      apiKey,
    });
  }

  if (runnable.length === 0) {
    console.warn(
      "[e2e] no runnable plans — set GEMINI_API_KEY and/or ANTHROPIC_API_KEY",
    );
  }

  return { runnable, blockingReasons: [], graphqlUrl };
}
