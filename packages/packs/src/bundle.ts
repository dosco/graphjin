import { lstat, readFile, readdir, realpath } from "node:fs/promises";
import { basename, extname, join, relative, resolve, sep } from "node:path";
import { parse as parseYaml } from "yaml";
import { validateArtifactContent } from "./artifact-schema.js";
import { canonicalHash, sha256 } from "./canonicalize.js";
import { parseManifest, type SolutionPackManifest } from "./manifest.js";

export type PackArtifactKind =
  | "source"
  | "relationships"
  | "spec"
  | "saved_query"
  | "metric"
  | "workflow"
  | "watcher"
  | "action"
  | "policy"
  | "skill";

export interface PackArtifact {
  kind: PackArtifactKind;
  key: string;
  targetRef: string;
  path: string;
  hash: string;
  content: unknown;
}

export interface SolutionPackBundle {
  root: string;
  manifest: SolutionPackManifest;
  manifestHash: string;
  bundleHash: string;
  artifacts: PackArtifact[];
}

const structuredExtensions = new Set([".yaml", ".yml", ".json"]);

function stableFileKey(kind: PackArtifactKind, file: string): string {
  const stem = basename(file, extname(file)).replace(/_/g, "-");
  return `${kind}.${stem}`;
}

async function assertSafePath(root: string, candidate: string): Promise<string> {
  const rootReal = await realpath(root);
  const absolute = resolve(root, candidate);
  const lexical = relative(rootReal, absolute);
  if (lexical === ".." || lexical.startsWith(`..${sep}`) || lexical.startsWith(sep)) {
    throw new Error(`pack path escapes root: ${candidate}`);
  }
  const segments = lexical.split(sep).filter(Boolean);
  let cursor = rootReal;
  for (const segment of segments) {
    cursor = join(cursor, segment);
    const stat = await lstat(cursor);
    if (stat.isSymbolicLink()) throw new Error(`pack path cannot contain symlinks: ${candidate}`);
  }
  const resolved = await realpath(absolute);
  const physical = relative(rootReal, resolved);
  if (physical === ".." || physical.startsWith(`..${sep}`) || physical.startsWith(sep)) {
    throw new Error(`pack path resolves outside root: ${candidate}`);
  }
  return resolved;
}

async function readArtifactContent(path: string): Promise<{ content: unknown; hash: string }> {
  const raw = (await readFile(path, "utf8")).replace(/\r\n/g, "\n");
  if (structuredExtensions.has(extname(path).toLowerCase())) {
    const content = extname(path).toLowerCase() === ".json" ? JSON.parse(raw) : parseYaml(raw);
    return { content, hash: canonicalHash(content) };
  }
  return { content: raw, hash: sha256(raw) };
}

function artifactIdentity(
  kind: PackArtifactKind,
  path: string,
  content: unknown,
): { key: string; targetRef: string } {
  if (content && typeof content === "object" && !Array.isArray(content)) {
    const record = content as Record<string, unknown>;
    const key = typeof record.key === "string" ? record.key : stableFileKey(kind, path);
    const targetRef = typeof record.targetRef === "string" ? record.targetRef : key;
    return { key, targetRef };
  }
  return { key: stableFileKey(kind, path), targetRef: stableFileKey(kind, path) };
}

async function filesInDirectory(root: string, path: string): Promise<string[]> {
  const directory = await assertSafePath(root, path);
  const entries = await readdir(directory, { withFileTypes: true });
  const files: string[] = [];
  for (const entry of entries.sort((a, b) => a.name.localeCompare(b.name))) {
    if (entry.isSymbolicLink()) throw new Error(`pack directory cannot contain symlinks: ${path}/${entry.name}`);
    if (!entry.isFile()) continue;
    files.push(relative(root, join(directory, entry.name)));
  }
  return files;
}

async function artifactFromFile(
  root: string,
  kind: PackArtifactKind,
  path: string,
): Promise<PackArtifact> {
  const safePath = await assertSafePath(root, path);
  const { content, hash } = await readArtifactContent(safePath);
  validateArtifactContent(kind, content, path);
  const { key, targetRef } = artifactIdentity(kind, path, content);
  return { kind, key, targetRef, path: relative(root, safePath), hash, content };
}

async function sourceArtifacts(root: string, path: string): Promise<PackArtifact[]> {
  const artifact = await artifactFromFile(root, "source", path);
  const record = artifact.content as { sources?: unknown };
  if (!record || !Array.isArray(record.sources)) {
    throw new Error(`${path} must contain a sources array`);
  }
  return record.sources.map((source, index) => {
    if (!source || typeof source !== "object" || Array.isArray(source)) {
      throw new Error(`${path} source ${index} must be an object`);
    }
    const value = source as Record<string, unknown>;
    if (typeof value.name !== "string" || !value.name) {
      throw new Error(`${path} source ${index} requires name`);
    }
    const key = typeof value.key === "string" ? value.key : `source.${value.name}`;
    const content = Object.fromEntries(Object.entries(value).filter(([name]) => name !== "key"));
    return {
      kind: "source" as const,
      key,
      targetRef: value.name,
      path: artifact.path,
      hash: canonicalHash(content),
      content,
    };
  });
}

async function directoryArtifacts(
  root: string,
  kind: PackArtifactKind,
  path: string | undefined,
): Promise<PackArtifact[]> {
  if (!path) return [];
  const files = await filesInDirectory(root, path);
  const allowed = kind === "saved_query" ? new Set([".gql", ".graphql"]) : new Set([".yaml", ".yml"]);
  for (const file of files) {
    if (!allowed.has(extname(file).toLowerCase())) {
      throw new Error(`unsupported ${kind} artifact file: ${file}`);
    }
  }
  return Promise.all(files.map((file) => artifactFromFile(root, kind, file)));
}

async function skillArtifact(root: string, path: string): Promise<PackArtifact> {
  const skillPath = join(path, "SKILL.md");
  const artifact = await artifactFromFile(root, "skill", skillPath);
  const skillName = basename(path);
  return {
    ...artifact,
    key: `skill.${skillName}`,
    targetRef: skillName,
    hash: await hashTree(root, path),
  };
}

async function hashTree(root: string, path: string): Promise<string> {
  const directory = await assertSafePath(root, path);
  const entries = await readdir(directory, { withFileTypes: true });
  const values: Array<{ path: string; hash: string }> = [];
  for (const entry of entries.sort((a, b) => a.name.localeCompare(b.name))) {
    if (entry.isSymbolicLink()) throw new Error(`pack skill cannot contain symlinks: ${path}/${entry.name}`);
    const child = join(directory, entry.name);
    const childPath = relative(root, child);
    if (entry.isDirectory()) {
      values.push({ path: childPath, hash: await hashTree(root, childPath) });
    } else if (entry.isFile()) {
      if (extname(entry.name).toLowerCase() !== ".md") {
        throw new Error(`pack skills may contain Markdown only: ${childPath}`);
      }
      const value = await readArtifactContent(child);
      values.push({ path: childPath, hash: value.hash });
    }
  }
  return canonicalHash(values);
}

function recordArtifact(artifact: PackArtifact): Record<string, unknown> {
  if (!artifact.content || typeof artifact.content !== "object" || Array.isArray(artifact.content)) {
    throw new Error(`${artifact.kind} artifact ${artifact.path} must be an object`);
  }
  return artifact.content as Record<string, unknown>;
}

function validateArtifactReferences(artifacts: PackArtifact[]): void {
  const sourceNames = new Set(
    artifacts.filter((artifact) => artifact.kind === "source").map((artifact) => artifact.targetRef),
  );
  const savedQueries = new Set(
    artifacts
      .filter((artifact) => artifact.kind === "saved_query")
      .map((artifact) => basename(artifact.path, extname(artifact.path))),
  );
  const workflowKeys = new Set(
    artifacts.filter((artifact) => artifact.kind === "workflow").map((artifact) => artifact.key),
  );

  const requireSource = (artifact: PackArtifact, source: unknown) => {
    if (!sourceNames.has(String(source))) {
      throw new Error(`${artifact.kind} artifact ${artifact.path} references missing source ${String(source)}`);
    }
  };
  const requireQuery = (artifact: PackArtifact, query: unknown) => {
    if (!savedQueries.has(String(query))) {
      throw new Error(`${artifact.kind} artifact ${artifact.path} references missing saved query ${String(query)}`);
    }
  };

  for (const artifact of artifacts) {
    if (artifact.kind === "relationships") {
      requireSource(artifact, recordArtifact(artifact).source);
    } else if (artifact.kind === "metric") {
      const execution = recordArtifact(artifact).execution as Record<string, unknown>;
      requireSource(artifact, execution.source);
      requireQuery(artifact, execution.query);
    } else if (artifact.kind === "watcher") {
      const watcher = recordArtifact(artifact);
      if (!workflowKeys.has(String(watcher.workflow))) {
        throw new Error(
          `watcher artifact ${artifact.path} references missing workflow ${String(watcher.workflow)}`,
        );
      }
      requireQuery(artifact, watcher.query);
    } else if (artifact.kind === "action") {
      const adapter = recordArtifact(artifact).adapter as Record<string, unknown>;
      if (adapter.source !== undefined) requireSource(artifact, adapter.source);
      for (const field of ["preconditionQuery", "mutationQuery", "reconciliationQuery"] as const) {
        if (adapter[field] !== undefined) requireQuery(artifact, adapter[field]);
      }
    }
  }
}

export async function loadSolutionPack(rootInput: string): Promise<SolutionPackBundle> {
  const root = await realpath(rootInput);
  const manifestPath = await assertSafePath(root, "pack.yaml");
  const manifestRaw = await readFile(manifestPath, "utf8");
  const manifest = parseManifest(parseYaml(manifestRaw));
  const graphjin = manifest.artifacts.graphjin;

  // Validate every manifest target serially before parallel parsing. Besides
  // making failures deterministic, this guarantees a symlink in an early
  // target cannot be hidden by a racing ENOENT or parse failure elsewhere.
  for (const path of [
    graphjin?.sources,
    graphjin?.relationships,
    ...(graphjin?.specs ?? []),
    graphjin?.savedQueries,
    manifest.artifacts.metrics,
    manifest.artifacts.workflows,
    manifest.artifacts.watchers,
    manifest.artifacts.actions,
    manifest.artifacts.policies,
    ...manifest.artifacts.skills,
  ]) {
    if (path) await assertSafePath(root, path);
  }

  const artifacts = (
    await Promise.all([
      graphjin ? sourceArtifacts(root, graphjin.sources) : [],
      graphjin ? artifactFromFile(root, "relationships", graphjin.relationships).then((value) => [value]) : [],
      Promise.all((graphjin?.specs ?? []).map((path) => artifactFromFile(root, "spec", path))),
      graphjin ? directoryArtifacts(root, "saved_query", graphjin.savedQueries) : [],
      directoryArtifacts(root, "metric", manifest.artifacts.metrics),
      directoryArtifacts(root, "workflow", manifest.artifacts.workflows),
      directoryArtifacts(root, "watcher", manifest.artifacts.watchers),
      directoryArtifacts(root, "action", manifest.artifacts.actions),
      directoryArtifacts(root, "policy", manifest.artifacts.policies),
      Promise.all(manifest.artifacts.skills.map((path) => skillArtifact(root, path))),
    ])
  ).flat();

  const identities = new Set<string>();
  const targets = new Set<string>();
  for (const artifact of artifacts) {
    const identity = `${artifact.kind}:${artifact.key}`;
    const target = `${artifact.kind}:${artifact.targetRef}`;
    if (identities.has(identity)) throw new Error(`duplicate artifact key: ${identity}`);
    if (targets.has(target)) throw new Error(`duplicate artifact target: ${target}`);
    identities.add(identity);
    targets.add(target);
  }
  validateArtifactReferences(artifacts);

  artifacts.sort((a, b) => `${a.kind}:${a.key}`.localeCompare(`${b.kind}:${b.key}`));
  const manifestHash = canonicalHash(manifest);
  const bundleHash = canonicalHash({
    manifest: manifestHash,
    artifacts: artifacts.map(({ kind, key, targetRef, hash }) => ({ kind, key, targetRef, hash })),
  });
  return { root, manifest, manifestHash, bundleHash, artifacts };
}
