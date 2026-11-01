import { defineConfig } from "vitest/config";
import { fileURLToPath } from "node:url";

const here = (rel: string) => fileURLToPath(new URL(rel, import.meta.url));

export default defineConfig({
  test: {
    include: ["test/**/*.test.ts"],
    setupFiles: [here("./test/_helpers/setup-config-isolation.ts")],
    // One fork per test file: each file gets a fresh module graph, a fresh
    // @neko/db pool, and a fresh vi.mock registry. Without this, files
    // sharing a fork inherited a singleton pool that earlier files'
    // afterAll hooks had already .end()-ed.
    pool: "forks",
    poolOptions: { forks: { singleFork: false } },
    testTimeout: 10_000,
    alias: {
      // Match the Next.js TS path alias from tsconfig.json.
      "@/": `${here("./src/")}`,
      // server-only would otherwise throw at import time outside Next.
      "server-only": here("./test/stubs/server-only.ts"),
    },
  },
});
