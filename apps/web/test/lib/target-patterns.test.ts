import { describe, expect, it } from "vitest";
import { parseTargetPatterns } from "@/lib/target-patterns";

describe("parseTargetPatterns", () => {
  it("splits lines and commas, trims, and drops duplicates", () => {
    expect(parseTargetPatterns(" https://hooks.acme.com/* \n\nabc, abc")).toEqual({
      ok: true,
      patterns: ["https://hooks.acme.com/*", "abc"],
    });
  });

  it("lowercases email domains and strips a leading @", () => {
    expect(parseTargetPatterns("Acme.com\n@Partner.com\n*.EU.acme.com", "email_domain")).toEqual({
      ok: true,
      patterns: ["acme.com", "partner.com", "*.eu.acme.com"],
    });
  });

  it("rejects something that is not a domain", () => {
    for (const bad of ["ops@acme.com", "acme", "*acme.com", "https://acme.com"]) {
      const result = parseTargetPatterns(bad, "email_domain");
      expect(result.ok).toBe(false);
    }
  });

  it("rejects non-strings and patterns with spaces", () => {
    expect(parseTargetPatterns([1]).ok).toBe(false);
    expect(parseTargetPatterns(["a b"]).ok).toBe(false);
    expect(parseTargetPatterns(3).ok).toBe(false);
  });
});
