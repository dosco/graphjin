import { describe, expect, it } from "vitest";
import { newRequestId } from "@/lib/request-id";

const UUID_V4 = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

describe("newRequestId", () => {
  it("uses crypto.randomUUID on a secure page", () => {
    expect(newRequestId({ randomUUID: () => "secure-id", getRandomValues: crypto.getRandomValues.bind(crypto) })).toBe("secure-id");
  });

  it("builds a version 4 UUID on a plain HTTP page, where randomUUID is missing", () => {
    const insecure = { getRandomValues: crypto.getRandomValues.bind(crypto) };
    const first = newRequestId(insecure);
    expect(first).toMatch(UUID_V4);
    expect(newRequestId(insecure)).not.toBe(first);
  });
});
