import { beforeEach, describe, expect, it } from "vitest";
import {
  _clearDiscoveryPathwaysCacheForTesting,
  buildDiscoveryPathwaysSection,
  getDiscoveryPathways,
} from "../src/discovery-pathways";

const INSIGHTS = JSON.stringify({
  hub_tables: [{ table: "salesorderheader" }, { name: "customer" }],
  query_templates: [
    { title: "Revenue by month", description: "sum over order totals" },
  ],
});

beforeEach(() => _clearDiscoveryPathwaysCacheForTesting());

describe("discovery pathways (GJ3)", () => {
  it("derives role-shaped pathways from insights", () => {
    const p = getDiscoveryPathways({
      orgId: "o1",
      role: "CFO",
      intent: "gross margin trend",
      insightsJson: INSIGHTS,
    });
    expect(p.entrypoints).toEqual(["salesorderheader", "customer"]);
    expect(p.seedSearches[0]).toBe("gross margin trend");
    expect(p.seedSearches).toContain("margin by product");
    expect(p.queryTemplates[0]).toEqual({
      title: "Revenue by month",
      note: "sum over order totals",
    });
  });

  it("caches per (org, role, intent)", () => {
    const a = getDiscoveryPathways({
      orgId: "o1",
      role: "CEO",
      insightsJson: INSIGHTS,
    });
    const b = getDiscoveryPathways({
      orgId: "o1",
      role: "CEO",
      insightsJson: "{}", // ignored: cache hit
    });
    expect(b).toBe(a);
    const c = getDiscoveryPathways({
      orgId: "o1",
      role: "CRO",
      insightsJson: "{}",
    });
    expect(c).not.toBe(a);
  });

  it("degrades to role seeds on malformed insights", () => {
    const p = getDiscoveryPathways({
      orgId: "o2",
      role: "COO",
      insightsJson: "not json",
    });
    expect(p.entrypoints).toEqual([]);
    expect(p.seedSearches.length).toBeGreaterThan(0);
  });

  it("renders a warm-start section, empty when nothing to say", () => {
    const section = buildDiscoveryPathwaysSection({
      entrypoints: ["t1"],
      seedSearches: ["s1"],
      queryTemplates: [],
    });
    expect(section).toContain("<discovery-pathways>");
    expect(section).toContain("- t1");
    expect(section).toContain('- "s1"');
    expect(
      buildDiscoveryPathwaysSection({
        entrypoints: [],
        seedSearches: [],
        queryTemplates: [],
      }),
    ).toBe("");
  });
});
