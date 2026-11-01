import {
  canonicalHash,
  DEFAULT_MAGENTO_CAPS,
  type SolutionPackBundle,
} from "@neko/packs";
import { artifactRecord } from "./pack-artifacts.js";
import type { MagentoPreflightResult } from "./magento-preflight.js";
import { magentoGraphjinTables } from "./magento-source-policy.js";

function graphjinRelationships(
  bundle: SolutionPackBundle,
  available: string[],
): Record<string, unknown>[] {
  const artifact = bundle.artifacts.find((value) => value.kind === "relationships");
  if (!artifact) return [];
  const relationships = artifactRecord(artifact).relationships as Array<Record<string, unknown>>;
  const tables = new Set(available);
  return relationships
    .filter((relationship) =>
      tables.has(String(relationship.left).split(".")[0]) &&
      tables.has(String(relationship.right).split(".")[0]),
    )
    .map((relationship) => ({
      from: `magento_analytics:${String(relationship.left)}`,
      to: `magento_analytics:${String(relationship.right)}`,
    }));
}

export function magentoCapsFromInputs(inputs: Record<string, unknown>) {
  return {
    ...DEFAULT_MAGENTO_CAPS,
    maxRowsPerChangeset: Number(inputs["magento.max_rows_per_changeset"]),
    maxPriceDeltaPercent: Number(inputs["magento.max_price_delta_percent"]),
    maxDiscountPercent: Number(inputs["magento.max_discount_percent"]),
    maxCouponCount: Number(inputs["magento.max_coupon_count"]),
    maxProjectedExposure: Number(inputs["magento.max_projected_exposure"]),
    maxDailyAutoActions: Number(inputs["magento.max_daily_auto_actions"]),
    skuCooldownSeconds: Number(inputs["magento.sku_cooldown_seconds"]),
  };
}

function magentoV2OperationExposure(bundle: SolutionPackBundle): Record<string, unknown> {
  const operations: Record<string, unknown> = {};
  for (const artifact of bundle.artifacts.filter((value) => value.kind === "action")) {
    const adapter = artifactRecord(artifact).adapter;
    if (!adapter || typeof adapter !== "object" || Array.isArray(adapter)) continue;
    const definition = adapter as Record<string, unknown>;
    if (definition.kind !== "magento_changeset" && definition.kind !== "magento_governed_operation") continue;
    const declared = definition.operations;
    if (!declared || typeof declared !== "object" || Array.isArray(declared)) continue;
    for (const operation of Object.values(declared as Record<string, unknown>)) {
      if (!operation || typeof operation !== "object" || Array.isArray(operation)) continue;
      const value = operation as Record<string, unknown>;
      const operationId = String(value.operationId ?? "");
      const mutationRoot = String(value.mutationRoot ?? "");
      const exposeAs = mutationRoot.replace(/^magento_operator_v2_/, "");
      if (!operationId || !mutationRoot || exposeAs === mutationRoot) {
        throw new Error(`Magento operation ${operationId || "<unknown>"} has an invalid V2 mutation root`);
      }
      const next = {
        expose_mutation: true,
        allowed_roles: Number(value.defaultClass) === 2
          ? ["magento_ops_executor", "magento_sensitive_executor"]
          : ["magento_sensitive_executor"],
        expose_as: exposeAs,
      };
      const current = operations[operationId];
      if (current && canonicalHash(current) !== canonicalHash(next)) {
        throw new Error(`Magento operation ${operationId} has conflicting V2 exposure`);
      }
      operations[operationId] = next;
    }
  }
  return operations;
}

export function magentoGraphjinUpdate(
  inputs: Record<string, unknown>,
  secrets: Record<string, string>,
  preflight: MagentoPreflightResult,
  bundle: SolutionPackBundle,
  retiredSourceNames: string[] = [],
): Record<string, unknown> {
  const integrationToken = secrets["magento.integration_token"];
  const writeEnabled = Boolean(integrationToken);
  const auth = integrationToken
    ? { auth: { scheme: "bearer", token: integrationToken } }
    : {};
  return {
    roles: [
      { name: "magento_ops_executor", comment: "Short-lived Magento executor for approved automations" },
      { name: "magento_sensitive_executor", comment: "Short-lived Magento executor minted after administrator approval" },
    ],
    update_sources: [
      {
        name: "magento_analytics",
        kind: "database",
        default: false,
        type: preflight.databaseType,
        host: String(inputs["database.host"]),
        port: Number(inputs["database.port"]),
        dbname: String(inputs["database.name"]),
        user: secrets["database.analytics_username"],
        password: secrets["database.analytics_password"],
        read_only: true,
        analytics_mode: true,
        capabilities: { "data.read": true, "data.write": false, "schema.read": true, "schema.write": false },
        access: { read: "authenticated", write: "blocked", delete: "blocked", blocked_tables: preflight.blockedTables },
      },
      {
        name: "magento_operator",
        kind: "api",
        default: false,
        specs_dir: "/config/specs",
        specs: {
          "magento-operator-v1": {
            base_url: String(inputs["magento.base_url"]),
            ...auth,
            operations: { magentoAddInternalOrderComment: { expose_mutation: false, allowed_roles: [] } },
          },
          "magento-operator-v2": {
            base_url: String(inputs["magento.base_url"]),
            ...auth,
            operations: magentoV2OperationExposure(bundle),
          },
        },
        read_only: !writeEnabled,
        capabilities: { "api.read": true, "api.write": writeEnabled, "api.delete": writeEnabled },
        access: {
          read: "authenticated",
          write: writeEnabled ? "authenticated" : "blocked",
          delete: writeEnabled ? "authenticated" : "blocked",
        },
      },
    ],
    ...(retiredSourceNames.length > 0
      ? {
          source_patches: retiredSourceNames.map((name) => ({
            name,
            read_only: true,
            access: { read: "blocked", write: "blocked", delete: "blocked" },
          })),
        }
      : {}),
    tables: magentoGraphjinTables(preflight.tablePrefix, preflight.availableAnalyticsTables),
    relationships: graphjinRelationships(bundle, preflight.availableAnalyticsTables),
  };
}
