import { describe, expect, it } from "vitest";
import { matchesListSearch } from "@/lib/list-search";

describe("list search", () => {
  it("matches across fields, ignores accents, and tolerates one typo", () => {
    expect(matchesListSearch("stock play", "Stockout response", "Playbook")).toBe(true);
    expect(matchesListSearch("cafe", "Caf\u00e9 policy")).toBe(true);
    expect(matchesListSearch("stockot", "Stockout response")).toBe(true);
    expect(matchesListSearch("invoice", "Stockout response")).toBe(false);
  });
});
