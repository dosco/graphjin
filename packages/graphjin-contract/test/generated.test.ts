import { execFileSync } from "node:child_process";
import { mkdtempSync, readFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { describe, expect, it } from "vitest";

const root = resolve(import.meta.dirname, "..");

describe("generated contract", () => {
  it("matches GraphJin's Go types and config schema", () => {
    const out = join(mkdtempSync(join(tmpdir(), "graphjin-contract-")), "generated.ts");
    execFileSync(
      "go",
      ["run", ".", resolve(root, "../../serv/config.schema.json"), out],
      { cwd: join(root, "generate"), env: { ...process.env, GOWORK: "off" }, stdio: "pipe" },
    );
    expect(readFileSync(join(root, "src/generated.ts"), "utf8")).toBe(readFileSync(out, "utf8"));
  }, 600_000);
});
