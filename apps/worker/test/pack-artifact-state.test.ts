import { describe, expect, it } from "vitest";
import { canonicalHash } from "@neko/packs";
import { liveSourceStateHash, nativeArtifactStateHash, policyArtifactStateHash } from "../src/packs/artifact-state";

describe("pack materialized state", () => {
  it("ignores database timestamps but detects managed metric edits", () => {
    const original = {
      role: "owner",
      slug: "orders",
      title: "Orders",
      active: true,
      updated_at: new Date("2026-01-01"),
    };
    expect(nativeArtifactStateHash("metric", {
      ...original,
      updated_at: new Date("2026-08-20"),
    })).toBe(nativeArtifactStateHash("metric", original));
    expect(nativeArtifactStateHash("metric", {
      ...original,
      title: "Orders edited by user",
    })).not.toBe(nativeArtifactStateHash("metric", original));
  });

  it("accepts legacy policy receipts without hiding actual policy edits", () => {
    const policy = {
      name: "Magento governed store changes",
      description: "Approval required",
      applies_to_kinds: ["magento.manage_catalog"],
      applies_to_scopes: ["external"],
      mode: "approval_required",
      allowed_targets: { source: "magento_operator" },
      limits: { retry: "never" },
      priority: 10,
      enabled: false,
    };
    const legacyReceipt = nativeArtifactStateHash("policy", { ...policy, approver_role: "admin" });
    expect(policyArtifactStateHash(policy, legacyReceipt)).toBe(legacyReceipt);
    expect(policyArtifactStateHash({ ...policy, limits: { retry: "always" } }, legacyReceipt))
      .not.toBe(legacyReceipt);
    expect(policyArtifactStateHash(policy)).toBe(nativeArtifactStateHash("policy", policy));
  });

  it("hashes a live GraphJin source the same after a restart fills empty lists", () => {
    const applied = {
      name: "forum_api",
      kind: "api",
      read_only: true,
      password: "[REDACTED]",
      capabilities: { "api.write": false, "api.delete": false },
      access: { read: "public", write: "blocked", delete: "blocked", owner_column: "", public_tables: null, admin_tables: null },
      specs: { discourse: { base_url: "https://forum.example", timeout: 0, operations: { listLatestTopics: { expose_as: "latest", allowed_roles: null, defaults: null, disabled: false } } } },
    };
    const restarted = {
      ...applied,
      access: { ...applied.access, public_tables: [], admin_tables: [], blocked_tables: [] },
      specs: { discourse: { ...applied.specs.discourse, operations: { listLatestTopics: { expose_as: "latest", allowed_roles: [], defaults: {}, disabled: false } } } },
    };
    expect(liveSourceStateHash(restarted)).toBe(liveSourceStateHash(applied));
    expect(liveSourceStateHash({ ...applied, access: { ...applied.access, read: "authenticated" } }))
      .not.toBe(liveSourceStateHash(applied));
    expect(liveSourceStateHash({ ...applied, capabilities: { "api.write": false } }))
      .not.toBe(liveSourceStateHash(applied));
  });

  it("accepts a source receipt hashed before empty values were dropped", () => {
    const source = {
      name: "forum",
      kind: "api",
      read_only: true,
      access: { read: "public", public_tables: null },
      capabilities: null,
      specs: { forum: { base_url: "https://forum.example", operations: { list: { expose_as: "forum_list" } } } },
    };
    const legacyReceipt = canonicalHash({
      name: "forum",
      kind: "api",
      read_only: true,
      access: source.access,
      capabilities: null,
      specs: { forum: { base_url: "https://forum.example", operations: source.specs.forum.operations } },
    });
    expect(liveSourceStateHash(source, legacyReceipt)).toBe(legacyReceipt);
    expect(liveSourceStateHash({ ...source, read_only: false }, legacyReceipt)).not.toBe(legacyReceipt);
  });
});
