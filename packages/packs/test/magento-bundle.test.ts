import { mkdtemp, readFile, symlink, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { parse as parseYaml, stringify as stringifyYaml } from "yaml";
import { loadSolutionPack } from "../src/bundle.js";
import { validateArtifactContent } from "../src/artifact-schema.js";
import { canonicalJson } from "../src/canonicalize.js";
import { parseManifest } from "../src/manifest.js";
import { planPack } from "../src/planner.js";

const here = dirname(fileURLToPath(import.meta.url));
const magentoRoot = resolve(here, "../../../packs/magento");

describe("Magento solution pack", () => {
  it("loads the complete pack with stable, globally unique identities", async () => {
    const bundle = await loadSolutionPack(magentoRoot);
    expect(bundle.manifest.metadata.id).toBe("magento");
    expect(bundle.manifest.secrets.find((value) => value.key === "magento.integration_token")?.required).toBe(false);

    const count = (kind: string) => bundle.artifacts.filter((artifact) => artifact.kind === kind).length;
    expect(count("source")).toBe(2);
    expect(count("metric")).toBe(13);
    expect(count("workflow")).toBe(7);
    const securityWorkflow = bundle.artifacts.find((artifact) => artifact.key === "workflow.security_advisory_check");
    expect(securityWorkflow?.content).toMatchObject({
      enabled: true,
      schedule: { cron: "0 6 * * *", timezoneInput: "UTC", enabled: true },
    });
    expect(count("watcher")).toBe(6);
    expect(count("action")).toBe(8);
    expect(count("policy")).toBe(2);
    expect(count("skill")).toBe(9);
    expect(
      bundle.artifacts
        .filter((artifact) => artifact.kind === "skill")
        .map((artifact) => artifact.targetRef)
        .sort(),
    ).toEqual([
      "magento-check-inventory",
      "magento-diagnose-platform-health",
      "magento-investigate-order",
      "magento-investigate-refunds",
      "magento-manage-catalog",
      "magento-review-performance",
      "magento-run-promotions",
      "magento-security-advisories",
      "magento-triage-fulfillment",
    ]);
    expect(bundle.artifacts.some((artifact) => artifact.targetRef === "magento-ops")).toBe(false);
    expect(new Set(bundle.artifacts.map((artifact) => `${artifact.kind}:${artifact.key}`)).size).toBe(
      bundle.artifacts.length,
    );
  });

  it("discovers every artifact without filtering on optional write readiness", async () => {
    const bundle = await loadSolutionPack(magentoRoot);
    const plan = planPack(bundle, []);
    expect(plan.entries.every((entry) => entry.action === "create")).toBe(true);
    expect(plan.entries.some((entry) => entry.key === "action.manage_catalog")).toBe(true);
    expect(plan.entries.some((entry) => entry.key === "action.financial_handoff")).toBe(true);
    expect(plan.entries.some((entry) => entry.key === "policy.governed_store_changes")).toBe(true);
    expect(plan.entries.some((entry) => entry.key === "source.magento_operator")).toBe(true);
  });

  it("keeps internal numeric risk labels out of operator-facing artifacts", async () => {
    const bundle = await loadSolutionPack(magentoRoot);
    const operatorFacing = bundle.artifacts.filter((artifact) =>
      ["action", "policy", "skill", "workflow", "watcher"].includes(artifact.kind),
    );
    for (const artifact of operatorFacing) {
      expect(JSON.stringify(artifact.content), artifact.path).not.toMatch(/\bclass\s*[012]\b/i);
    }
  });

  it("produces the same manifest and bundle hashes on repeated loads", async () => {
    const first = await loadSolutionPack(magentoRoot);
    const second = await loadSolutionPack(magentoRoot);
    expect(second.manifestHash).toBe(first.manifestHash);
    expect(second.bundleHash).toBe(first.bundleHash);
  });

  it("declares the operator source with isolated executor roles", async () => {
    const bundle = await loadSolutionPack(magentoRoot);
    const operator = bundle.artifacts.find((artifact) => artifact.key === "source.magento_operator");
    expect(operator?.content).toMatchObject({
      read_only: false,
      capabilities: { "api.write": true, "api.delete": true },
      access: { write: "authenticated", delete: "authenticated" },
      readiness: {
        write_roles: ["magento_ops_executor", "magento_sensitive_executor"],
      },
    });
  });

  it("keeps handoff-only and Magento administration endpoints out of the V2 spec", async () => {
    const spec = await readFile(
      join(magentoRoot, "graphjin/specs/magento-operator-v2.yaml"),
      "utf8",
    );
    const document = parseYaml(spec) as {
      paths: Record<string, Record<string, {
        operationId?: string;
        responses?: Record<string, unknown>;
      }>>;
    };
    const pathNames = Object.keys(document.paths).join("\n");
    const operationIds = Object.values(document.paths)
      .flatMap((path) => Object.values(path).map((operation) => operation.operationId ?? ""))
      .join("\n");
    expect(pathNames).not.toMatch(/refund|creditmemo|returns|\/integration|\/admin/i);
    expect(operationIds).toContain("magentoBulkUpdateProducts");
    expect(operationIds).toContain("magentoUpdateCustomer");
    expect(
      document.paths["/rest/{storeCode}/async/bulk/V1/products/bySku"]?.put?.responses,
    ).toHaveProperty("202");
  });

  it("keeps every governed action bound to a checked-in V2 operation and GraphJin root", async () => {
    const bundle = await loadSolutionPack(magentoRoot);
    const document = parseYaml(await readFile(
      join(magentoRoot, "graphjin/specs/magento-operator-v2.yaml"),
      "utf8",
    )) as { paths: Record<string, Record<string, { operationId?: string }>> };
    const operationIds = new Set(
      Object.values(document.paths)
        .flatMap((path) => Object.values(path).map((operation) => operation.operationId))
        .filter((value): value is string => Boolean(value)),
    );
    for (const artifact of bundle.artifacts.filter((value) => value.kind === "action")) {
      const adapter = (artifact.content as { adapter?: { operations?: Record<string, {
        operationId: string;
        mutationRoot: string;
      }> } }).adapter;
      for (const operation of Object.values(adapter?.operations ?? {})) {
        expect(operationIds, `${artifact.path}:${operation.operationId}`).toContain(operation.operationId);
        const snake = operation.operationId
          .replace(/([a-z0-9])([A-Z])/g, "$1_$2")
          .toLowerCase();
        expect(operation.mutationRoot).toBe(`magento_operator_v2_${snake}`);
      }
    }
  });

  it("exposes operational customer data while retaining secret-only denials", async () => {
    const bundle = await loadSolutionPack(magentoRoot);
    const analytics = bundle.artifacts.find((artifact) => artifact.key === "source.magento_analytics");
    expect(analytics?.content).toMatchObject({
      allowlist: {
        tables: expect.arrayContaining([
          "customer_entity",
          "customer_address_entity",
          "sales_order_address",
          "sales_order_status_history",
          "sales_order_payment",
        ]),
      },
      blocklist: {
        tables: expect.not.arrayContaining([
          "customer_entity",
          "customer_address_entity",
          "sales_order_payment",
        ]),
        columns: expect.arrayContaining([
          "password_hash",
          "rp_token",
          "protect_code",
          "cc_number_enc",
        ]),
      },
    });
    expect((analytics?.content as { blocklist?: { columns?: string[] } })?.blocklist?.columns)
      .not.toEqual(expect.arrayContaining(["email", "telephone", "customer_firstname"]));
  });
});

describe("manifest and bundle safety", () => {
  it("canonicalizes object keys without reordering arrays", () => {
    expect(canonicalJson({ z: 1, a: [{ y: 2, x: 1 }, 3] })).toBe(
      '{"a":[{"x":1,"y":2},3],"z":1}',
    );
  });

  it("rejects unknown manifest fields", async () => {
    const raw = parseYaml(await readFile(join(magentoRoot, "pack.yaml"), "utf8"));
    raw.installScript = "./install.sh";
    expect(() => parseManifest(raw)).toThrow(/unrecognized key/i);
  });

  it("rejects manifest path traversal before reading artifacts", async () => {
    const raw = parseYaml(await readFile(join(magentoRoot, "pack.yaml"), "utf8"));
    raw.artifacts.metrics = "../metrics";
    expect(() => parseManifest(raw)).toThrow(/pack root/);
  });

  it("rejects symlinks in a pack path", async () => {
    const temporary = await mkdtemp(join(tmpdir(), "openneko-pack-"));
    const manifest = parseYaml(await readFile(join(magentoRoot, "pack.yaml"), "utf8"));
    manifest.artifacts.graphjin.sources = "sources.yaml";
    await writeFile(join(temporary, "pack.yaml"), stringifyYaml(manifest));
    await symlink(join(magentoRoot, "graphjin/sources.yaml"), join(temporary, "sources.yaml"));
    await expect(loadSolutionPack(temporary)).rejects.toThrow(/symlinks/);
  });

  it("rejects malformed source identities and unsupported watcher operators", () => {
    expect(() => validateArtifactContent("source", {
      sources: [{ key: "bad key", name: "magento", kind: "database" }],
    }, "sources.yaml")).toThrow(/invalid source artifact/);
    expect(() => validateArtifactContent("watcher", {
      key: "watcher.test",
      targetRef: "magento.watcher.test",
      name: "Test",
      description: "Test",
      workflow: "workflow.test",
      enabled: false,
      activation: "admin",
      query: "test",
      valuePath: "value",
      operator: "neq",
      threshold: 0,
      cadenceSeconds: 60,
      debounceSeconds: 0,
      cooldownSeconds: 0,
      severity: "low",
      dedupeKey: "test",
    }, "watcher.yaml")).toThrow(/invalid watcher artifact/);
  });
});

describe("drift-aware planning", () => {
  it("is idempotent for unchanged installed artifacts", async () => {
    const bundle = await loadSolutionPack(magentoRoot);
    const installed = bundle.artifacts.map((artifact) => ({
      kind: artifact.kind,
      key: artifact.key,
      targetRef: artifact.targetRef,
      desiredHash: artifact.hash,
      lastAppliedHash: artifact.hash,
      currentHash: artifact.hash,
      ownership: "managed" as const,
    }));
    expect(planPack(bundle, installed).entries.every((entry) => entry.action === "noop")).toBe(true);
  });

  it("preserves a user-modified artifact as a conflict", async () => {
    const bundle = await loadSolutionPack(magentoRoot);
    const desired = bundle.artifacts.find((artifact) => artifact.kind === "metric");
    expect(desired).toBeDefined();
    const plan = planPack(bundle, [
      {
        kind: desired!.kind,
        key: desired!.key,
        targetRef: desired!.targetRef,
        desiredHash: desired!.hash,
        lastAppliedHash: desired!.hash,
        currentHash: "user-edited-hash",
        ownership: "managed",
      },
    ]);
    expect(plan.entries.find((entry) => entry.key === desired!.key)).toMatchObject({
      action: "conflict",
      reason: "user_modified",
    });
  });

  it("reclaims an unchanged retired artifact on reinstall", async () => {
    const bundle = await loadSolutionPack(magentoRoot);
    const desired = bundle.artifacts.find((artifact) => artifact.kind === "metric")!;
    const plan = planPack(bundle, [
      {
        kind: desired.kind,
        key: desired.key,
        targetRef: desired.targetRef,
        desiredHash: desired.hash,
        lastAppliedHash: "disabled-state",
        currentHash: "disabled-state",
        ownership: "retired",
      },
    ]);
    expect(plan.entries.find((entry) => entry.key === desired.key)).toMatchObject({
      action: "update",
    });
  });

  it("preserves a modified retired artifact on reinstall", async () => {
    const bundle = await loadSolutionPack(magentoRoot);
    const desired = bundle.artifacts.find((artifact) => artifact.kind === "metric")!;
    const plan = planPack(bundle, [
      {
        kind: desired.kind,
        key: desired.key,
        targetRef: desired.targetRef,
        desiredHash: desired.hash,
        lastAppliedHash: "disabled-state",
        currentHash: "edited-after-uninstall",
        ownership: "retired",
      },
    ]);
    expect(plan.entries.find((entry) => entry.key === desired.key)).toMatchObject({
      action: "conflict",
      reason: "user_modified",
    });
  });

  it("retires the prototype umbrella skill while adding focused operator skills", async () => {
    const bundle = await loadSolutionPack(magentoRoot);
    const plan = planPack(bundle, [{
      kind: "skill",
      key: "skill.magento-ops",
      targetRef: "magento-ops",
      desiredHash: "prototype-skill",
      lastAppliedHash: "prototype-skill",
      currentHash: "prototype-skill",
      ownership: "managed",
    }]);

    expect(plan.entries.find((entry) => entry.key === "skill.magento-ops")).toMatchObject({
      action: "retire",
      reason: "removed_from_pack",
    });
    expect(
      plan.entries.filter((entry) => entry.kind === "skill" && entry.action === "create"),
    ).toHaveLength(9);
  });

  it("does not repeatedly retire an artifact already removed by an earlier upgrade", async () => {
    const bundle = await loadSolutionPack(magentoRoot);
    const plan = planPack(bundle, [{
      kind: "saved_query",
      key: "saved_query.removed-in-prior-version",
      targetRef: "saved_query.removed-in-prior-version",
      desiredHash: "old-definition",
      lastAppliedHash: "retired-state",
      currentHash: null,
      ownership: "retired",
    }]);
    expect(plan.entries.some((entry) => entry.key === "saved_query.removed-in-prior-version")).toBe(false);
  });
});
