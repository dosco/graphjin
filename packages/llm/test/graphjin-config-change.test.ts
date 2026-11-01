import { describe, expect, it, vi } from "vitest";
import {
  buildGraphjinConfigUpdate,
  graphjinConfigPatchHash,
  graphjinInputWithJsonVariables,
  graphjinInputValue,
  assertDatabaseSourcesStayReadOnly,
} from "../src/graphjin/config-change";

describe("GraphJin config change compiler", () => {
  it("builds additive source-access patches", async () => {
    const result = await buildGraphjinConfigUpdate({
      action: "set_source_access",
      source: "adventureworks",
      read: "authenticated",
      write: "blocked",
    });
    expect(result.update).toEqual({
      source_patches: [
        {
          name: "adventureworks",
          access: { read: "authenticated", write: "blocked" },
        },
      ],
    });
  });

  it("resolves a secret by name without accepting a literal value", async () => {
    const resolve = vi.fn(async () => "super-secret-value");
    const result = await buildGraphjinConfigUpdate(
      {
        action: "register_source",
        name: "warehouse",
        kind: "database",
        host: "warehouse-db",
        secretRef: "WAREHOUSE_PASSWORD",
      },
      resolve,
    );
    expect(resolve).toHaveBeenCalledWith("WAREHOUSE_PASSWORD");
    expect(result.secretName).toBe("WAREHOUSE_PASSWORD");
    expect(JSON.stringify(result.update)).toContain("super-secret-value");
    expect(JSON.stringify(result)).not.toContain("secretRef");
    const [warehouse] = (result.update as { update_sources: Array<Record<string, unknown>> })
      .update_sources;
    expect(warehouse?.read_only).toBe(true);
  });

  it("builds an API source from OpenAPI-specific fields", async () => {
    const result = await buildGraphjinConfigUpdate({
      action: "register_source",
      name: "billing-api",
      kind: ["api"],
      specAssetId: "4bc3b97b-d9bb-4d5f-908e-83ad1a4a7512",
    }, undefined, { managedOpenApiSpecsDir: "/config/specs/org/billing-api" });
    expect(result.update).toEqual({
      update_sources: [
        {
          name: "billing-api",
          kind: "api",
          specs_dir: "/config/specs/org/billing-api",
          access: {
            read: "authenticated",
            write: "blocked",
            delete: "blocked",
          },
        },
      ],
    });
  });

  it("requires an imported asset for API sources", async () => {
    await expect(
      buildGraphjinConfigUpdate({
        action: "register_source",
        name: "billing-api",
        kind: "api",
        specsDir: "/arbitrary/model/path",
      }),
    ).rejects.toThrow("managed specAssetId");
  });

  it("builds an S3 file source from storage-specific fields", async () => {
    const result = await buildGraphjinConfigUpdate({
      action: "register_source",
      name: "documents",
      kind: ["file"],
      backend: ["s3"],
      bucket: "company-documents",
      prefix: "approved/",
      region: "ap-south-1",
      endpoint: "https://storage.example.com",
    });
    expect(result.update).toMatchObject({
      update_sources: [
        {
          name: "documents",
          kind: "file",
          backend: "s3",
          bucket: "company-documents",
          prefix: "approved/",
          region: "ap-south-1",
          endpoint: "https://storage.example.com",
          read_only: true,
          max_list_page_size: 500,
          capabilities: {
            "files.list": true,
            "files.read": true,
            "files.write": false,
            "files.delete": false,
            "files.watch": false,
          },
          access: {
            read: "authenticated",
            write: "blocked",
            delete: "blocked",
          },
        },
      ],
    });
  });

  it("uses an OpenNeko-managed root for local files and forces read-only capabilities", async () => {
    const result = await buildGraphjinConfigUpdate(
      {
        action: "register_source",
        name: "documents",
        kind: "file",
        backend: "local",
        localFiles: {
          sourceName: "documents",
          ready: true,
          files: [
            {
              name: "report.csv",
              size: 12,
              contentType: "text/csv",
              checksumSha256: "a".repeat(64),
            },
          ],
          totalSize: 12,
        },
        write: "admin",
        delete: "admin",
      },
      undefined,
      { managedLocalFilesRoot: "/config/files/org_abc_documents" },
    );
    expect(result.update).toMatchObject({
      update_sources: [
        {
          name: "documents",
          root: "/config/files/org_abc_documents",
          read_only: true,
          capabilities: {
            "files.write": false,
            "files.delete": false,
            "files.watch": false,
          },
          access: {
            read: "authenticated",
            write: "blocked",
            delete: "blocked",
          },
        },
      ],
    });
  });

  it("rejects proposal-controlled local roots", async () => {
    await expect(
      buildGraphjinConfigUpdate(
        {
          action: "register_source",
          name: "documents",
          kind: "file",
          backend: "local",
          root: "/config",
          localFiles: {
            sourceName: "documents",
            ready: true,
            files: [
              {
                name: "report.csv",
                size: 12,
                contentType: "text/csv",
                checksumSha256: "a".repeat(64),
              },
            ],
            totalSize: 12,
          },
        },
        undefined,
        { managedLocalFilesRoot: "/config/files/org_abc_documents" },
      ),
    ).rejects.toThrow("managed by OpenNeko");
  });

  it("validates cloud bucket, endpoint, prefix, and presign TTL", async () => {
    await expect(
      buildGraphjinConfigUpdate({
        action: "register_source",
        name: "documents",
        kind: "file",
        backend: "s3",
        bucket: "company-documents",
        endpoint: "https://user:pass@storage.example.com",
      }),
    ).rejects.toThrow("must not contain credentials");
    await expect(
      buildGraphjinConfigUpdate({
        action: "register_source",
        name: "documents",
        kind: "file",
        backend: "gcs",
        bucket: "company_documents",
        prefix: "/private",
      }),
    ).rejects.toThrow("relative object-key prefix");
    await expect(
      buildGraphjinConfigUpdate({
        action: "register_source",
        name: "documents",
        kind: "file",
        backend: "s3",
        bucket: "company-documents",
        presignTtl: "24h",
      }),
    ).rejects.toThrow("between 1m and 1h");
  });

  it("rejects internal or unsupported source kinds", async () => {
    await expect(
      buildGraphjinConfigUpdate({
        action: "register_source",
        name: "system",
        kind: "graphjin",
      }),
    ).rejects.toThrow("kind must be database, api, or file");
  });

  it("escapes inline GraphQL input values", () => {
    expect(
      graphjinInputValue({ roles: [{ name: "admin", match: "role = \"admin\"" }] }),
    ).toBe('{ roles: [{ name: "admin", match: "role = \\"admin\\"" }] }');
  });

  it("binds dotted JSON keys through GraphQL JSON variables", () => {
    const encoded = graphjinInputWithJsonVariables({
      update_sources: [
        {
          name: "documents",
          capabilities: { "files.read": true, "files.write": false },
        },
      ],
    });
    expect(encoded.variableDefinitions).toBe("($json1: JSON!)");
    expect(encoded.literal).toContain("capabilities: $json1");
    expect(encoded.variables).toEqual({
      json1: { "files.read": true, "files.write": false },
    });
  });

  it("hashes proposals independently of object key order", () => {
    expect(
      graphjinConfigPatchHash({ action: "add_role", name: "support" }),
    ).toBe(
      graphjinConfigPatchHash({ name: "support", action: "add_role" }),
    );
  });
});

describe("assertDatabaseSourcesStayReadOnly", () => {
  const current = [
    { name: "erp", kind: "database", read_only: true },
    { name: "shop", kind: "api" },
  ];

  it("lets API sources open writes and databases stay untouched", () => {
    expect(() =>
      assertDatabaseSourcesStayReadOnly(
        { source_patches: [{ name: "shop", access: { write: "authenticated" } }] },
        current,
      ),
    ).not.toThrow();
    expect(() =>
      assertDatabaseSourcesStayReadOnly(
        { source_patches: [{ name: "erp", access: { read: "authenticated", write: "blocked" } }] },
        current,
      ),
    ).not.toThrow();
    expect(() =>
      assertDatabaseSourcesStayReadOnly(
        { update_sources: [{ name: "new", kind: "database", read_only: true, access: { write: "blocked" } }] },
        current,
      ),
    ).not.toThrow();
  });

  it("rejects every write path on a database or unknown source", () => {
    const cases: Array<[Record<string, unknown>, string]> = [
      [{ source_patches: [{ name: "erp", access: { write: "authenticated" } }] }, "access.write: authenticated"],
      [{ source_patches: [{ name: "erp", access: { delete: "admin" } }] }, "access.delete: admin"],
      [{ source_patches: [{ name: "erp", read_only: false }] }, "read_only: false"],
      [{ source_patches: [{ name: "erp", capabilities: { "api.write": true } }] }, "capabilities.api.write: true"],
      [{ source_patches: [{ name: "erp", specs: {} }] }, "specs"],
      [{ update_sources: [{ name: "fresh", kind: "database", read_only: false }] }, "read_only: false"],
      [{ source_patches: [{ name: "ghost", access: { write: "authenticated" } }] }, 'source "ghost"'],
    ];
    for (const [update, fragment] of cases) {
      expect(() => assertDatabaseSourcesStayReadOnly(update, current)).toThrow(fragment);
    }
  });
});

describe("API write proposals", () => {
  it("set_source_capabilities patches the source through update_sources", async () => {
    const result = await buildGraphjinConfigUpdate({
      action: "set_source_capabilities",
      source: "shop-api",
      apiWrite: true,
    });
    expect(result.update).toEqual({
      update_sources: [{ name: "shop-api", capabilities: { "api.write": true } }],
    });
    await expect(
      buildGraphjinConfigUpdate({ action: "set_source_capabilities", source: "shop-api" }),
    ).rejects.toThrow("needs apiWrite or apiDelete");
  });

  it("expose_api_operation writes the operation patch and demands roles", async () => {
    const result = await buildGraphjinConfigUpdate({
      action: "expose_api_operation",
      source: "shop-api",
      spec: "shop",
      operation: "createOrder",
      exposeMutation: true,
      allowedRoles: ["admin", "Member"],
      exposeAs: "shop_create_order",
    });
    expect(result.update).toEqual({
      update_sources: [
        {
          name: "shop-api",
          specs: {
            shop: {
              operations: {
                createOrder: {
                  expose_mutation: true,
                  allowed_roles: ["admin", "member"],
                  expose_as: "shop_create_order",
                },
              },
            },
          },
        },
      ],
    });
    await expect(
      buildGraphjinConfigUpdate({
        action: "expose_api_operation",
        source: "shop-api",
        spec: "shop",
        operation: "createOrder",
        exposeMutation: true,
      }),
    ).rejects.toThrow("needs allowedRoles");
  });
});

describe("enable_api_writes", () => {
  it("carries capability, exposure, and access in one update", async () => {
    const result = await buildGraphjinConfigUpdate({
      action: "enable_api_writes",
      source: "shop-api",
      spec: "shop",
      operation: "createOrder",
      allowedRoles: ["admin"],
      exposeAs: "shop_create_order",
    });
    expect(result.update).toEqual({
      update_sources: [
        {
          name: "shop-api",
          read_only: false,
          capabilities: { "api.write": true },
          specs: {
            shop: {
              operations: {
                createOrder: {
                  expose_mutation: true,
                  allowed_roles: ["admin"],
                  expose_as: "shop_create_order",
                },
              },
            },
          },
        },
      ],
      source_patches: [{ name: "shop-api", access: { write: "authenticated" } }],
    });
    await expect(
      buildGraphjinConfigUpdate({
        action: "enable_api_writes",
        source: "shop-api",
        spec: "shop",
        operation: "createOrder",
        allowedRoles: ["admin"],
        write: "public",
      }),
    ).rejects.toThrow("write must be authenticated or admin");
  });

  it("is refused on a database source by the invariant", () => {
    expect(() =>
      assertDatabaseSourcesStayReadOnly(
        {
          update_sources: [{ name: "erp", capabilities: { "api.write": true } }],
          source_patches: [{ name: "erp", access: { write: "authenticated" } }],
        },
        [{ name: "erp", kind: "database" }],
      ),
    ).toThrow('database source "erp" stays read-only');
  });
});
