#!/usr/bin/env node
// Decide which PR-check jobs a change needs.
//
// Reads changed file paths on stdin, one per line, relative to the repository
// root. Prints lines for $GITHUB_OUTPUT:
//
//   packages=["apps/web","packages/llm",...]   pnpm packages to typecheck,
//                                              build, and test
//   design_system=true|false                   one line per Docker or browser job
//   ...
//
// Usage:
//   git diff --name-only BASE HEAD | node scripts/ci-affected.mjs --base BASE [--head HEAD]
//   node scripts/ci-affected.mjs --all
//
// Rules:
//   - A package runs when it, or a workspace package it depends on, changes.
//   - db/ (migrations, GraphJin configs, records schemas, seeds) runs the
//     database package and everything that uses it. packs/ runs the web app,
//     the worker, and the packs package.
//   - Each Docker or browser job runs when the code it builds or drives
//     changes. See JOBS below.
//   - Lockfile, root config, and CI plumbing run everything.
//   - With --base, a package.json whose only change is its version (a
//     release-please bump) runs nothing.
//   - graphjin/ has its own workflow; here it runs only the GraphJin
//     integration job, since the openneko binary carries GraphJin. Docs run
//     nothing.

import { readFileSync, readdirSync, existsSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { join } from "node:path";

const root = new URL("..", import.meta.url).pathname;

// Workspace packages, from pnpm-workspace.yaml (apps/* and packages/*).
const packages = new Map(); // name -> dir
for (const group of ["apps", "packages"]) {
  for (const entry of readdirSync(join(root, group), { withFileTypes: true })) {
    const dir = `${group}/${entry.name}`;
    const manifest = join(root, dir, "package.json");
    if (!entry.isDirectory() || !existsSync(manifest)) continue;
    packages.set(JSON.parse(readFileSync(manifest, "utf8")).name, dir);
  }
}

// dir -> dirs of packages that depend on it, directly or not.
const dependents = new Map([...packages.values()].map((dir) => [dir, new Set()]));
for (const dir of packages.values()) {
  const manifest = JSON.parse(readFileSync(join(root, dir, "package.json"), "utf8"));
  const deps = { ...manifest.dependencies, ...manifest.devDependencies };
  for (const [name, spec] of Object.entries(deps)) {
    if (String(spec).startsWith("workspace:") && packages.has(name)) {
      dependents.get(packages.get(name)).add(dir);
    }
  }
}
function withDependents(dir) {
  const out = new Set([dir]);
  const queue = [dir];
  while (queue.length) {
    for (const next of dependents.get(queue.shift()) ?? []) {
      if (!out.has(next)) {
        out.add(next);
        queue.push(next);
      }
    }
  }
  return out;
}

// Docker and browser jobs. Each runs when a path matches one of its prefixes,
// or when one of its packages is picked.
const JOBS = {
  design_system: {
    paths: ["packs/", "db/migrations/", "apps/openneko/assets/migrations/", "scripts/check-web-design-system.mjs"],
    packages: ["apps/web"],
  },
  graphjin_integration: {
    // GraphJin builds from graphjin/ into the openneko binary.
    paths: ["db/graphjin/", "db/records/", "Dockerfile", "graphjin/", "scripts/build-openneko.sh"],
    packages: ["packages/records"],
  },
  records_recovery: {
    paths: ["db/", "compose.yml", "Dockerfile", "scripts/test-records-recovery.sh"],
    packages: [],
  },
  embedding_image: {
    paths: ["apps/embedding/", "Dockerfile"],
    packages: [],
  },
  librarian_image: {
    paths: ["apps/librarian/"],
    packages: [],
  },
  lazy_service: {
    paths: ["apps/lazy-service/", "go.work"],
    packages: [],
  },
};

// Changes here run everything.
const EVERYTHING = [
  "package.json",
  "pnpm-lock.yaml",
  "pnpm-workspace.yaml",
  "tsconfig.json",
  "tsconfig.base.json",
  "patches/",
  ".github/workflows/pr-checks.yml",
  "scripts/ci-affected.mjs",
];

// Changes here run nothing in this workflow.
const IGNORED = [
  "graphjin/website/",
  "graphjin/docs/",
  "graphjin/benchmark/",
  "graphjin/training/",
  "graphjin/notes/",
  "docs/",
  ".github/",
  "deploy/",
];

const picked = new Set();
const jobs = Object.fromEntries(Object.keys(JOBS).map((job) => [job, false]));
let all = process.argv.includes("--all");
const baseIndex = process.argv.indexOf("--base");
const base = baseIndex > 0 ? process.argv[baseIndex + 1] : "";
const headIndex = process.argv.indexOf("--head");
const head = headIndex > 0 ? process.argv[headIndex + 1] : "HEAD";

// True when a package.json changed only its "version" line.
function versionOnly(path) {
  if (!base || !path.endsWith("package.json")) return false;
  const diff = execFileSync("git", ["diff", "-U0", base, head, "--", path], { cwd: root, encoding: "utf8" });
  const changed = diff
    .split("\n")
    .filter((line) => /^[+-]/.test(line) && !/^(\+\+\+|---) /.test(line));
  return changed.length > 0 && changed.every((line) => /^[+-]\s*"version":/.test(line));
}

function matches(path, prefixes) {
  return prefixes.some((prefix) => (prefix.endsWith("/") ? path.startsWith(prefix) : path === prefix));
}

function classify(path) {
  if (versionOnly(path)) return;
  if (matches(path, EVERYTHING)) {
    all = true;
    return;
  }
  if (path.endsWith(".md") || matches(path, IGNORED)) return;

  for (const dir of packages.values()) {
    if (path.startsWith(`${dir}/`)) for (const d of withDependents(dir)) picked.add(d);
  }
  // Tests read migrations, GraphJin configs, records schemas, and seeds.
  if (matches(path, ["db/", "apps/openneko/assets/migrations/"])) {
    for (const d of withDependents("packages/db")) picked.add(d);
  }
  // Tests load the shipped packs.
  if (path.startsWith("packs/")) {
    for (const d of ["apps/web", "apps/worker", "packages/packs"]) picked.add(d);
  }
  if (path.startsWith("evals/")) {
    for (const d of withDependents("packages/evals")) picked.add(d);
  }
  for (const [job, rule] of Object.entries(JOBS)) {
    if (matches(path, rule.paths)) jobs[job] = true;
  }
}

if (!all) {
  const input = readFileSync(0, "utf8");
  for (const line of input.split("\n")) {
    const path = line.trim();
    if (path) classify(path);
  }
}

if (all) {
  for (const dir of packages.values()) picked.add(dir);
  for (const job of Object.keys(jobs)) jobs[job] = true;
}
for (const [job, rule] of Object.entries(JOBS)) {
  if (rule.packages.some((dir) => picked.has(dir))) jobs[job] = true;
}

// Only packages with a test script get a test job; keep a stable order.
const testable = [...packages.values()]
  .filter((dir) => picked.has(dir))
  .filter((dir) => JSON.parse(readFileSync(join(root, dir, "package.json"), "utf8")).scripts?.test)
  .sort();

console.log(`packages=${JSON.stringify(testable)}`);
for (const [job, run] of Object.entries(jobs)) console.log(`${job}=${run}`);
