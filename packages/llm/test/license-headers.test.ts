import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { dirname, join } from "node:path";
import { describe, expect, it } from "vitest";

const pkgRoot = join(dirname(fileURLToPath(import.meta.url)), "..");
const FORBIDDEN_LICENSE_MARKER = "LicenseRef-" + "OpenNeko-Commercial";

const FILES = [
  "src/workflows/audit-chain.ts",
  "src/work/behavior-monitor.ts",
  "src/work/authz.ts",
  "src/work/memory.ts",
  "src/graphjin/token.ts",
];

describe("license headers", () => {
  it.each(FILES)("%s does not carry a non-Apache project license marker", (rel) => {
    expect(readFileSync(join(pkgRoot, rel), "utf8")).not.toContain(FORBIDDEN_LICENSE_MARKER);
  });
});
