// Live validation of the agentic knowledge layer against a REAL
// graphjin sources-mode server (auth: jwt). Runs only when
// OPENNEKO_TEST_GJ_SOURCES_URL names one: the dev stack serves a different
// GraphJin on :8090, so a default address would test the wrong server.

import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  knowledgePackPaths,
  prefetchAgenticKnowledgePack,
  readKnowledgePack,
} from "../../src/knowledge-pack";
import { mintGraphjinToken } from "../../src/graphjin/token";

const BASE = process.env.OPENNEKO_TEST_GJ_SOURCES_URL?.trim() ?? "";
const GRAPHQL_URL = `${BASE}/api/v1/graphql`;
const ORG_ID = "org-gj4-live";

async function serverReachable(): Promise<boolean> {
  if (!BASE) return false;
  // Probe the GraphQL endpoint itself — /health in this fork build can
  // 500 (health-check deadline) while queries serve fine.
  try {
    const res = await fetch(GRAPHQL_URL, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ query: "query { __typename }" }),
      signal: AbortSignal.timeout(2500),
    });
    return res.status > 0;
  } catch {
    return false;
  }
}

const reachable = await serverReachable();
const describeIfLive = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn(
    `[agentic-knowledge-live] skipping: ${BASE ? `no sources-mode GraphJin at ${BASE}` : "OPENNEKO_TEST_GJ_SOURCES_URL is not set"}`,
  );
}

describeIfLive("agentic knowledge pack against live sources-mode GraphJin", () => {
  let dir: string;

  beforeAll(async () => {
    dir = await mkdtemp(join(tmpdir(), "agentic-live-"));
  });

  afterAll(async () => {
    await rm(dir, { recursive: true, force: true });
  });

  it("prefetches the slim pack with a minted service token", async () => {
    const result = await prefetchAgenticKnowledgePack({
      graphqlUrl: GRAPHQL_URL,
      token: mintGraphjinToken({ orgId: ORG_ID, userId: null, role: "service" }),
      destDir: dir,
    });
    expect(result.ok, result.error).toBe(true);

    const pack = await readKnowledgePack(knowledgePackPaths(dir));
    expect(pack.mode).toBe("agentic");
    const tables = JSON.parse(pack.tables) as { tables: Array<{ id: string }> };
    expect(tables.tables.length).toBeGreaterThan(10);
    expect(tables.tables[0].id).toMatch(/^table:/);
    const insights = JSON.parse(pack.insights) as {
      help_cards: Array<{ id: string }>;
    };
    expect(insights.help_cards.some((c) => c.id === "help:discovery")).toBe(true);
    const syntax = JSON.parse(pack.syntax) as { essentials: unknown[] };
    expect(syntax.essentials.length).toBeGreaterThan(0);

    // The slim bootstrap must actually be slim — an order of magnitude
    // under the legacy dumps (which ran to hundreds of KB).
    const totalBytes = result.files.reduce((n, f) => n + f.bytes, 0);
    expect(totalBytes).toBeLessThan(120_000);
  });

  it("a forged token cannot prefetch", async () => {
    const result = await prefetchAgenticKnowledgePack({
      graphqlUrl: GRAPHQL_URL,
      token: "aaa.bbb.ccc",
      destDir: join(dir, "forged"),
    });
    expect(result.ok).toBe(false);
  });
});
