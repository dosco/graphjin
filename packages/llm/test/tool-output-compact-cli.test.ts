import { spawnSync } from "node:child_process";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { compactJson, expandJson } from "../src/work/tool-output/compact-json";

const CLI = fileURLToPath(
  new URL("../src/work/tool-output/compact-cli.mjs", import.meta.url),
);

function runCli(input: string): string {
  const r = spawnSync("node", [CLI], { input, encoding: "utf8" });
  if (r.status !== 0) throw new Error(`compact-cli exited ${r.status}: ${r.stderr}`);
  return r.stdout;
}

function graphjinRows(n: number): string {
  return JSON.stringify({
    data: {
      productinventory: Array.from({ length: n }, (_, i) => ({
        productid: i + 1,
        locationid: (i % 3) * 5 + 1,
        shelf: ["A", "B", "C"][i % 3],
        bin: i % 30,
        quantity: 100 + i,
        modifieddate: "2025-08-08T00:00:00",
      })),
    },
  });
}

// The wrapper runs this CLI; if it ever diverges from compactJson, the model
// sees a different shape than the prompt/instrumentation describe. Pin them.
describe("compact-cli.mjs matches compactJson byte-for-byte", () => {
  const fixtures: Record<string, string> = {
    "graphjin flat rows": graphjinRows(80),
    "nested rows (no columnar)": JSON.stringify({
      data: {
        productinventory: Array.from({ length: 20 }, (_, i) => ({
          productid: i,
          product: { name: `p${i}`, reorderpoint: 750 },
        })),
      },
    }),
    "scalar object": JSON.stringify({ ok: true, count: 3 }),
    "short array": JSON.stringify([{ a: 1 }, { a: 2 }]),
    "non-JSON": "ERROR: table not found: foo",
    "empty": "",
    "graphjin error envelope": JSON.stringify({
      errors: [{ message: "unauthorized" }],
    }),
  };

  for (const [name, raw] of Object.entries(fixtures)) {
    it(name, () => {
      expect(runCli(raw)).toBe(compactJson(raw).text);
    });
  }
});

describe("compact-cli.mjs output is lossless and a real reduction on rows", () => {
  it("columnarizes flat rows and round-trips back to the original", () => {
    const raw = graphjinRows(100);
    const out = runCli(raw);
    expect(out).toContain("__neko_cols__");
    expect(out.length).toBeLessThan(raw.length * 0.6);
    expect(expandJson(JSON.parse(out))).toEqual(JSON.parse(raw));
  });

  it("passes non-JSON through verbatim", () => {
    expect(runCli("not json at all")).toBe("not json at all");
  });
});
