import { describe, expect, it } from "vitest";
import {
  COLUMNAR_MARKER,
  compactJson,
  expandJson,
} from "../src/work/tool-output/compact-json";

/** GraphJin-shaped payload: a GraphQL data envelope with a row array. */
function graphjinRows(n: number): string {
  const products = Array.from({ length: n }, (_, i) => ({
    productid: i + 1,
    name: `Product ${i + 1}`,
    reorderpoint: 25,
    quantity: i * 3,
    discontinued: false,
  }));
  return JSON.stringify({ data: { products } }, null, 2);
}

describe("compactJson — losslessness (round-trip)", () => {
  const fixtures: Record<string, string> = {
    "graphjin rows": graphjinRows(50),
    "top-level array": JSON.stringify(
      Array.from({ length: 10 }, (_, i) => ({ a: i, b: `r${i}`, c: null })),
    ),
    "nested arrays": JSON.stringify({
      regions: [
        { region: "APAC", revenue: 1200, orders: 34 },
        { region: "EMEA", revenue: 980, orders: 21 },
        { region: "AMER", revenue: 2100, orders: 56 },
        { region: "LATAM", revenue: 410, orders: 9 },
      ],
      meta: { generatedAt: "2026-06-17", currency: "USD" },
    }),
    "ragged objects (not tabularized)": JSON.stringify([
      { a: 1, b: 2 },
      { a: 3 },
      { a: 5, b: 6, c: 7 },
    ]),
    "objects with nested values (not tabularized)": JSON.stringify([
      { id: 1, tags: ["x", "y"] },
      { id: 2, tags: ["z"] },
      { id: 3, tags: [] },
    ]),
    scalar: JSON.stringify({ ok: true, count: 0 }),
    "plain string (non-JSON)": "ERROR: connection refused at host db:5432",
    "empty array": JSON.stringify({ data: { products: [] } }),
  };

  for (const [name, raw] of Object.entries(fixtures)) {
    it(`${name} expands back to the original value`, () => {
      const { text } = compactJson(raw);
      // Non-JSON passes through verbatim; JSON must round-trip exactly.
      let original: unknown;
      try {
        original = JSON.parse(raw);
      } catch {
        expect(text).toBe(raw);
        return;
      }
      const restored = expandJson(JSON.parse(text));
      expect(restored).toEqual(original);
    });
  }
});

describe("compactJson — when it transforms", () => {
  it("columnarizes a uniform array of >= 3 flat objects", () => {
    const result = compactJson(graphjinRows(50));
    expect(result.format).toBe("columnar");
    expect(result.transformed).toBe(true);
    expect(result.compactedBytes).toBeLessThan(result.originalBytes);
    expect(result.text).toContain(COLUMNAR_MARKER);
    // Each column key appears once, not once per row.
    const occurrences = result.text.split('"productid"').length - 1;
    expect(occurrences).toBe(1);
  });

  it("delivers a large reduction on row-heavy output", () => {
    const result = compactJson(graphjinRows(200));
    const pct =
      (result.originalBytes - result.compactedBytes) / result.originalBytes;
    expect(pct).toBeGreaterThan(0.5);
  });

  it("only strips whitespace when there is no uniform array (minified)", () => {
    const raw = JSON.stringify({ ok: true, value: 42, label: "hi" }, null, 2);
    const result = compactJson(raw);
    expect(result.format).toBe("minified");
    expect(result.compactedBytes).toBeLessThan(result.originalBytes);
    expect(result.text).not.toContain(COLUMNAR_MARKER);
  });
});

describe("compactJson — never regresses or corrupts", () => {
  it("non-JSON input is returned verbatim as passthrough", () => {
    const raw = "graphjin: 12 rows returned in 4ms";
    const result = compactJson(raw);
    expect(result.format).toBe("passthrough");
    expect(result.transformed).toBe(false);
    expect(result.text).toBe(raw);
  });

  it("already-minimal JSON that can't shrink stays passthrough", () => {
    const raw = '{"a":1}';
    const result = compactJson(raw);
    expect(result.text).toBe(raw);
    expect(result.compactedBytes).toBeLessThanOrEqual(result.originalBytes);
  });

  it("a short array (< 3 rows) is not tabularized", () => {
    const raw = JSON.stringify([
      { a: 1, b: 2 },
      { a: 3, b: 4 },
    ]);
    const result = compactJson(raw);
    expect(result.text).not.toContain(COLUMNAR_MARKER);
  });

  it("preserves exact numeric values (financial-data safety)", () => {
    const rows = [
      { sku: "A", revenue: 1234567.89, qty: 3 },
      { sku: "B", revenue: 0.01, qty: 0 },
      { sku: "C", revenue: -42.5, qty: 99999 },
    ];
    const result = compactJson(JSON.stringify({ data: { rows } }));
    const restored = expandJson(JSON.parse(result.text)) as {
      data: { rows: typeof rows };
    };
    expect(restored.data.rows).toEqual(rows);
  });
});

// Locks in the behaviors observed against real AdventureWorks GraphJin output:
// flat row arrays columnarize for a large win; rows carrying a nested object
// fall back to minify-only (the documented limitation); date strings survive.
describe("compactJson — real GraphJin shapes (regression)", () => {
  // Mirrors `query { productinventory { productid locationid shelf bin quantity modifieddate } }`.
  const flatInventory = JSON.stringify({
    data: {
      productinventory: Array.from({ length: 60 }, (_, i) => ({
        productid: i + 1,
        locationid: (i % 3) * 5 + 1,
        shelf: ["A", "B", "C"][i % 3],
        bin: i % 30,
        quantity: 100 + i,
        modifieddate: "2025-08-08T00:00:00",
      })),
    },
  });

  // Mirrors the nested join: each row carries a `product` object.
  const nestedInventory = JSON.stringify({
    data: {
      productinventory: Array.from({ length: 60 }, (_, i) => ({
        productid: i + 1,
        quantity: 100 + i,
        product: {
          name: `Product ${i + 1}`,
          productnumber: `PN-${1000 + i}`,
          reorderpoint: 750,
          safetystocklevel: 1000,
        },
      })),
    },
  });

  it("flat inventory rows columnarize with a >50% reduction, losslessly", () => {
    const r = compactJson(flatInventory);
    expect(r.format).toBe("columnar");
    expect(r.compactedBytes / r.originalBytes).toBeLessThan(0.5);
    expect(expandJson(JSON.parse(r.text))).toEqual(JSON.parse(flatInventory));
  });

  it("nested-object rows never columnarize, and round-trip losslessly", () => {
    const r = compactJson(nestedInventory);
    expect(r.format).not.toBe("columnar");
    expect(r.text).not.toContain(COLUMNAR_MARKER);
    expect(expandJson(JSON.parse(r.text))).toEqual(JSON.parse(nestedInventory));
  });

  it("nested output with whitespace falls back to minify-only (no columnar)", () => {
    // GraphJin/`graphjin cli` stdout that is pretty-printed: the only win is
    // whitespace removal — the nested `product` object blocks columnarization.
    const pretty = JSON.stringify(JSON.parse(nestedInventory), null, 2);
    const r = compactJson(pretty);
    expect(r.format).toBe("minified");
    expect(r.text).not.toContain(COLUMNAR_MARKER);
    expect(r.compactedBytes).toBeLessThan(r.originalBytes);
    expect(expandJson(JSON.parse(r.text))).toEqual(JSON.parse(nestedInventory));
  });

  it("preserves ISO date strings exactly through the columnar round-trip", () => {
    const r = compactJson(flatInventory);
    const restored = expandJson(JSON.parse(r.text)) as {
      data: { productinventory: Array<{ modifieddate: string }> };
    };
    expect(restored.data.productinventory[0].modifieddate).toBe("2025-08-08T00:00:00");
  });
});
