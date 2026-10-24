import { defineConfig } from "vitest/config";

/**
 * E2E (slow tier) config. Only runs `test/e2e/**` — exercises real
 * agent subprocesses against the seeded AdventureWorks
 * Docker stack. Requires:
 *   - neko-db, graphjin, adventureworks-db containers up
 *   - At least one provider API key reachable to the worker
 *
 * Each test self-skips when its prerequisites aren't met (see
 * test/e2e/_can-run.ts), so it's safe to invoke unconditionally.
 *
 * Run with: pnpm test:e2e (or pnpm --filter @neko/worker test:e2e)
 */
export default defineConfig({
  test: {
    include: ["test/e2e/**/*.test.ts"],
    pool: "forks",
    poolOptions: { forks: { singleFork: true } },
    // Real metric_refresh runs are 30–90s; budget generously.
    testTimeout: 5 * 60_000,
    hookTimeout: 60_000,
  },
});
