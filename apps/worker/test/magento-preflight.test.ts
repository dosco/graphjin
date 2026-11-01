import { afterEach, describe, expect, it, vi } from "vitest";
import {
  MAGENTO_ANALYTICS_TABLES,
  bulkConsumerReadiness,
  magentoOperatorReadiness,
  readMagentoVersion,
  supportedMagentoDatabase,
} from "../src/packs/magento-preflight.js";

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("Magento version preflight", () => {
  it("keeps a host-gateway address when Magento redirects to localhost", async () => {
    const request = vi.fn<typeof fetch>().mockResolvedValueOnce(
      new Response(null, {
        status: 302,
        headers: { location: "http://localhost:8080/magento_version" },
      }),
    );
    vi.stubGlobal("fetch", request);
    const hostHeaderRequest = vi
      .fn()
      .mockResolvedValue({ status: 200, text: "Magento/2.4 (Community)" });

    await expect(
      readMagentoVersion("http://host.docker.internal:8080", hostHeaderRequest),
    ).resolves.toBe("2.4.x");
    expect(hostHeaderRequest).toHaveBeenCalledWith(
      "http://host.docker.internal:8080/magento_version",
      "localhost:8080",
    );
  });

  it("rejects a version outside the supported 2.4 family", async () => {
    vi.stubGlobal("fetch", vi.fn<typeof fetch>().mockResolvedValue(new Response("Magento/2.3.7", { status: 200 })));

    await expect(readMagentoVersion("https://store.example.com")).rejects.toThrow(
      "did not report a supported 2.4.x version",
    );
  });
});

describe("Magento database compatibility", () => {
  it("requires the operational customer and order support tables", () => {
    expect(MAGENTO_ANALYTICS_TABLES).toEqual(
      expect.arrayContaining([
        "customer_entity",
        "customer_address_entity",
        "sales_order_address",
        "sales_order_status_history",
        "sales_order_payment",
      ]),
    );
  });

  it("accepts the declared MariaDB and MySQL floors", () => {
    expect(supportedMagentoDatabase("10.6.18-MariaDB").type).toBe("mariadb");
    expect(supportedMagentoDatabase("8.0.39").type).toBe("mysql");
  });

  it("rejects versions below the manifest contract", () => {
    expect(() => supportedMagentoDatabase("10.5.27-MariaDB")).toThrow(/MariaDB 10\.6\+/);
    expect(() => supportedMagentoDatabase("5.7.44")).toThrow(/MySQL 8\.0\+/);
  });
});

describe("Magento bulk consumer readiness", () => {
  it("uses the latest completed run instead of a future pending schedule", async () => {
    const query = vi.fn().mockResolvedValue([[{
      status: "success",
      executed_at: new Date(),
    }]]);

    await expect(bulkConsumerReadiness({ query } as never, "")).resolves.toBe("ready");
    expect(query.mock.calls[0]?.[0]).toContain("status = 'success'");
    expect(query.mock.calls[0]?.[0]).toContain("ORDER BY executed_at DESC");
  });
});

describe("Magento operator readiness", () => {
  const input = {
    host: "db",
    port: 3306,
    database: "magento",
    username: "analytics",
    password: "secret",
    tablePrefix: "",
    baseUrl: "https://store.example.com",
    storeCode: "default",
    integrationToken: "integration-token",
    customersEnabled: false,
  };

  it("reports ACL readiness per domain and keeps customers disabled", async () => {
    const request = vi.fn<typeof fetch>().mockImplementation(async (url) =>
      new Response("{}", { status: String(url).includes("salesRules") ? 403 : 200 }),
    );
    const result = await magentoOperatorReadiness(input, request);
    expect(result).toEqual({
      overall: "acl_missing",
      domains: {
        catalog: "ready",
        inventory: "ready",
        orders: "ready",
        promotions: "acl_missing",
        content: "ready",
        customers: "domain_disabled",
      },
    });
    expect(request).toHaveBeenCalledTimes(5);
  });

  it("distinguishes an invalid token from a missing token", async () => {
    await expect(magentoOperatorReadiness(
      { ...input, integrationToken: null },
      vi.fn<typeof fetch>(),
    )).resolves.toMatchObject({ overall: "integration_token_missing" });
    await expect(magentoOperatorReadiness(
      input,
      vi.fn<typeof fetch>().mockResolvedValue(new Response("", { status: 401 })),
    )).resolves.toMatchObject({ overall: "integration_token_invalid" });
  });
});
