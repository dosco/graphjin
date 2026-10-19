/**
 * Capability detection for the slow E2E tier.
 *
 * The E2E suite seeds its own throwaway org — it does NOT depend on the
 * dev's `/setup` state. This file just decides which (agent backend,
 * primary provider) combinations the host can exercise.
 *
 * Plan matrix:
 *   - Ax × Google Gemini (GEMINI_API_KEY + `openshell` CLI)
 *   - Ax × Anthropic     (ANTHROPIC_API_KEY + `openshell` CLI)
 *
 * Both keys are independently optional — set one or both. The suite
 * blocks (skips entirely) only when GraphJin or `graphjin` CLI is missing,
 * since those are required by every plan.
 */

import { spawnSync } from "node:child_process";

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
  /** Extra binary the run needs. `graphjin` is checked separately. */
  binary: "openshell";
};

const PLAN_SPECS: PlanSpec[] = [
  {
    id: "ax+gemini",
    backend: "ax",
    primaryProvider: "google-gemini",
    primaryModel: "gemini-3.6-flash",
    envVar: "GEMINI_API_KEY",
    binary: "openshell",
  },
  {
    id: "ax+anthropic",
    backend: "ax",
    primaryProvider: "anthropic",
    primaryModel: "claude-opus-4-7",
    envVar: "ANTHROPIC_API_KEY",
    binary: "openshell",
  },
];

function hasOnPath(cmd: string): boolean {
  return spawnSync("which", [cmd], { stdio: "ignore" }).status === 0;
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
    if (!hasOnPath(spec.binary)) {
      console.warn(
        `[e2e] \`${spec.binary}\` CLI not on PATH — skipping plan ${spec.id}`,
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
      "[e2e] no runnable plans — set GEMINI_API_KEY and/or ANTHROPIC_API_KEY with OpenShell installed",
    );
  }

  return { runnable, blockingReasons: [], graphqlUrl };
}
