import { canonicalHash } from "./canonicalize.js";
import type { PackArtifact, PackArtifactKind, SolutionPackBundle } from "./bundle.js";

export type PackPlanAction = "create" | "update" | "noop" | "conflict" | "retire";

export interface InstalledPackArtifact {
  kind: PackArtifactKind;
  key: string;
  targetRef: string;
  desiredHash: string;
  lastAppliedHash: string;
  currentHash: string | null;
  ownership: "managed" | "modified" | "detached" | "retired";
}

export interface PackPlanEntry {
  action: PackPlanAction;
  kind: PackArtifactKind;
  key: string;
  targetRef: string;
  desiredHash: string | null;
  currentHash: string | null;
  reason?: "user_modified" | "target_missing" | "removed_from_pack" | "detached";
}

export interface PackPlan {
  packId: string;
  version: string;
  bundleHash: string;
  entries: PackPlanEntry[];
  hash: string;
}

function desiredEntry(artifact: PackArtifact, installed?: InstalledPackArtifact): PackPlanEntry {
  const base = {
    kind: artifact.kind,
    key: artifact.key,
    targetRef: artifact.targetRef,
    desiredHash: artifact.hash,
    currentHash: installed?.currentHash ?? null,
  };
  if (!installed) return { action: "create", ...base };
  if (installed.ownership === "detached") return { action: "conflict", ...base, reason: "detached" };
  if (installed.ownership === "retired") {
    if (installed.currentHash !== null && installed.currentHash !== installed.lastAppliedHash) {
      return { action: "conflict", ...base, reason: "user_modified" };
    }
    return { action: "update", ...base };
  }
  if (installed.currentHash === null) return { action: "conflict", ...base, reason: "target_missing" };
  if (installed.currentHash !== installed.lastAppliedHash) {
    return { action: "conflict", ...base, reason: "user_modified" };
  }
  if (installed.desiredHash === artifact.hash) return { action: "noop", ...base };
  return { action: "update", ...base };
}

export function planPack(
  bundle: SolutionPackBundle,
  installedArtifacts: InstalledPackArtifact[],
): PackPlan {
  const installed = new Map(
    installedArtifacts.map((artifact) => [`${artifact.kind}:${artifact.key}`, artifact]),
  );
  const entries = bundle.artifacts.map((artifact) => {
    const identity = `${artifact.kind}:${artifact.key}`;
    const entry = desiredEntry(artifact, installed.get(identity));
    installed.delete(identity);
    return entry;
  });
  for (const artifact of installed.values()) {
    if (artifact.ownership === "detached" || artifact.ownership === "retired") continue;
    const modified = artifact.currentHash !== artifact.lastAppliedHash;
    entries.push({
      action: modified ? "conflict" : "retire",
      kind: artifact.kind,
      key: artifact.key,
      targetRef: artifact.targetRef,
      desiredHash: null,
      currentHash: artifact.currentHash,
      reason: modified ? "user_modified" : "removed_from_pack",
    });
  }
  entries.sort((a, b) => `${a.kind}:${a.key}`.localeCompare(`${b.kind}:${b.key}`));
  const plan = {
    packId: bundle.manifest.metadata.id,
    version: bundle.manifest.metadata.version,
    bundleHash: bundle.bundleHash,
    entries,
  };
  return { ...plan, hash: canonicalHash(plan) };
}
