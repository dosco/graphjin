import { describe, expect, it } from "vitest";
import { normalizePublicUrl } from "@/lib/public-url-settings";

describe("normalizePublicUrl", () => {
  it("keeps the origin and drops a trailing slash", () => {
    expect(normalizePublicUrl(" https://openneko.example.com/ ")).toBe("https://openneko.example.com");
    expect(normalizePublicUrl("http://10.0.0.5:4000")).toBe("http://10.0.0.5:4000");
  });

  it("keeps a proxy path prefix", () => {
    expect(normalizePublicUrl("https://proxy.example.com/openneko/")).toBe("https://proxy.example.com/openneko");
  });

  it("rejects other schemes, queries, and credentials", () => {
    expect(normalizePublicUrl("ftp://openneko.example.com")).toBeNull();
    expect(normalizePublicUrl("https://openneko.example.com/?a=1")).toBeNull();
    expect(normalizePublicUrl("https://user:pass@openneko.example.com")).toBeNull();
    expect(normalizePublicUrl("openneko.example.com")).toBeNull();
  });
});
