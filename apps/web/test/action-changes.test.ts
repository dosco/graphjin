import { describe, expect, it } from "vitest";
import { describeActionChanges, diffWords, humanizeKey, trimDiffContext } from "@/lib/action-changes";

const attrs = (pairs: Record<string, string>) =>
  Object.entries(pairs).map(([attribute_code, value]) => ({ attribute_code, value }));

describe("describeActionChanges", () => {
  it("returns only the fields that differ between before and after", () => {
    const sets = describeActionChanges({
      preview: {
        rows: [
          {
            entity_ref: "SKU-1",
            before: { sku: "SKU-1", custom_attributes: attrs({ color: "Light Grey", url_key: "sku-1" }) },
            after: { sku: "SKU-1", custom_attributes: attrs({ color: "Light Pink" }) },
          },
        ],
      },
    });
    expect(sets).toEqual([
      { entity: "SKU-1", changes: [{ key: "color", label: "Color", before: "Light Grey", after: "Light Pink" }] },
    ]);
  });

  it("falls back to after-only fields when the payload has no preview", () => {
    const sets = describeActionChanges({
      rows: [{ entity_ref: "SKU-1", body: { product: { sku: "SKU-1", price: 10 } } }],
    });
    expect(sets?.[0].changes).toEqual([{ key: "product.price", label: "Price", before: null, after: "10" }]);
  });

  it("returns null for a payload without rows", () => {
    expect(describeActionChanges({ message: "hi" })).toBeNull();
  });
});

describe("humanizeKey", () => {
  it("writes sentence case and keeps acronyms", () => {
    expect(humanizeKey("marina_color")).toBe("Marina color");
    expect(humanizeKey("skuId")).toBe("SKU ID");
  });
});

describe("diffWords", () => {
  it("marks the changed word and trims long unchanged runs", () => {
    const before = "one two three four five six seven eight nine ten eleven grey twelve";
    const after = "one two three four five six seven eight nine ten eleven pink twelve";
    const tokens = trimDiffContext(diffWords(before, after), 3);
    expect(tokens.map((t) => t.kind)).toEqual(["same", "removed", "added", "same"]);
    expect(tokens[0].text.startsWith("…")).toBe(true);
    expect(tokens[1].text.trim()).toBe("grey");
    expect(tokens[2].text.trim()).toBe("pink");
  });
});
