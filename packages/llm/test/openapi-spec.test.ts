import { describe, expect, it } from "vitest";
import {
  importOpenApiSpecFromUrl,
  inspectOpenApiSpec,
  isPublicAddress,
  managedOpenApiSpecFilename,
} from "../src/graphjin/openapi-spec";

const PETS = `
openapi: 3.0.3
info:
  title: Pet Service
  version: 1.2.0
servers:
  - url: /api/v3
paths:
  /pets:
    get:
      responses:
        '200': { description: ok }
    post:
      responses:
        '201': { description: created }
components:
  securitySchemes:
    apiKey:
      type: apiKey
      in: header
      name: X-API-Key
`;

describe("managed OpenAPI inspection", () => {
  it("normalizes a relative server URL and summarizes operations", () => {
    const result = inspectOpenApiSpec({
      content: PETS,
      originalName: "pets.yaml",
      sourceUrl: "https://example.com/specs/openapi.yaml",
    });
    expect(result.title).toBe("Pet Service");
    expect(result.baseUrl).toBe("https://example.com/api/v3");
    expect(result.operationCount).toBe(2);
    expect(result.readOperationCount).toBe(1);
    expect(result.mutatingOperationCount).toBe(1);
    expect(result.authSchemes).toEqual(["apiKey (apiKey)"]);
    expect(result.content).toContain("https://example.com/api/v3");
    expect(result.checksumSha256).toMatch(/^[a-f0-9]{64}$/);
  });

  it("uses an explicit base URL for uploaded documents", () => {
    const result = inspectOpenApiSpec({
      content: PETS,
      originalName: "pets.yaml",
      baseUrl: "https://pets.example.net/v2/",
    });
    expect(result.baseUrl).toBe("https://pets.example.net/v2");
  });

  it("rejects external references", () => {
    expect(() =>
      inspectOpenApiSpec({
        content: JSON.stringify({
          openapi: "3.0.3",
          info: { title: "Pets", version: "1" },
          servers: [{ url: "https://pets.example.net" }],
          paths: {
            "/pets": {
              get: { responses: { "200": { description: "ok" } } },
            },
          },
          components: {
            schemas: { Pet: { $ref: "https://example.com/pet.yaml" } },
          },
        }),
        originalName: "pets.yaml",
        baseUrl: "https://pets.example.net",
      }),
    ).toThrow(/external OpenAPI references/);
  });

  it("classifies private, loopback, link-local, and documentation addresses", () => {
    for (const address of [
      "127.0.0.1",
      "10.10.1.2",
      "169.254.169.254",
      "192.168.1.1",
      "203.0.113.8",
      "::1",
      "0:0:0:0:0:0:0:1",
      "fd00::1",
      "fe80::1",
      "2001:db8::1",
    ]) {
      expect(isPublicAddress(address), address).toBe(false);
    }
    expect(isPublicAddress("8.8.8.8")).toBe(true);
    expect(isPublicAddress("2606:4700:4700::1111")).toBe(true);
  });

  it("accepts hosted imports only through credential-free HTTPS URLs", async () => {
    await expect(
      importOpenApiSpecFromUrl({ url: "http://example.com/openapi.yaml" }),
    ).rejects.toThrow(/must use HTTPS/);
    await expect(
      importOpenApiSpecFromUrl({
        url: "https://user:password@example.com/openapi.yaml",
      }),
    ).rejects.toThrow(/must not include credentials/);
    await expect(
      importOpenApiSpecFromUrl({
        url: "https://example.com/openapi.yaml?token=private",
      }),
    ).rejects.toThrow(/query string or fragment/);
  });

  it("builds stable isolated filenames without accepting path syntax", () => {
    const path = managedOpenApiSpecFilename("org/acme", "Pet Store / Main");
    expect(path).toMatch(/^org_[a-f0-9]{12}_pet_store_main\.yaml$/);
    expect(managedOpenApiSpecFilename("org/acme", "Pet Store / Main")).toBe(path);
  });
});
