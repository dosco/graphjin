import { basename, extname } from "node:path";
import type { PackArtifact, SolutionPackBundle } from "@neko/packs";
import { packArtifactLocator } from "./artifact-state.js";
import type { MagentoPreflightResult } from "./magento-preflight.js";

export function artifactRecord(artifact: PackArtifact): Record<string, unknown> {
  if (!artifact.content || typeof artifact.content !== "object" || Array.isArray(artifact.content)) {
    throw new Error(`${artifact.kind} artifact ${artifact.path} must be an object`);
  }
  return artifact.content as Record<string, unknown>;
}

export function boundPackLocator(
  bundle: SolutionPackBundle,
  artifact: PackArtifact,
  bindings: Record<string, string>,
): Record<string, unknown> {
  if (bindings[artifact.key]) return { name: bindings[artifact.key] };
  if (artifact.kind === "relationships") {
    const source = bundle.artifacts.find(
      (value) => value.kind === "source" && artifactRecord(value).name === artifactRecord(artifact).source,
    );
    if (source && bindings[source.key]) return { source: bindings[source.key], ignorePackAliases: true };
  }
  return packArtifactLocator(artifact);
}

export function artifactLocatorFromMetadata(
  metadata: Record<string, unknown> | null | undefined,
  fallback?: PackArtifact,
): Record<string, unknown> {
  const value = metadata?.locator;
  if (value && typeof value === "object" && !Array.isArray(value)) return value as Record<string, unknown>;
  return fallback ? packArtifactLocator(fallback) : {};
}

export function findSavedQuery(bundle: SolutionPackBundle, name: string): string {
  const artifact = bundle.artifacts.find(
    (value) => value.kind === "saved_query" && basename(value.path, extname(value.path)) === name,
  );
  if (!artifact || typeof artifact.content !== "string") {
    throw new Error(`pack saved query ${name} is missing`);
  }
  return artifact.content;
}

export function operatorReadinessDetail(
  reason: MagentoPreflightResult["operatorReadiness"] | null,
): string {
  switch (reason) {
    case "integration_token_missing":
      return "View only. Add a Magento API token if you later want to allow specific, approval-required changes; store insights and automations are fully available without it.";
    case "integration_token_invalid":
      return "View only because Magento rejected the saved API token. Store insights and automations are unaffected.";
    case "acl_missing":
      return "View only because the saved API token does not have the required Magento permissions. Store insights and automations are unaffected.";
    case "graphjin_version_unsupported":
      return "View only in this version. Store insights and automations are fully available.";
    case "ready":
      return "Approved Magento changes are available. Each area follows its configured approval and automation limits.";
    case "domain_disabled":
      return "This change domain is disabled by the administrator.";
    default:
      return "View-only access could not be checked because the reporting connection is unavailable.";
  }
}
