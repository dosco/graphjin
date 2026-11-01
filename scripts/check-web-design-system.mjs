#!/usr/bin/env node

import { execFileSync } from "node:child_process";
import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const webSource = resolve(repoRoot, "apps/web/src");
const canonicalUi = "apps/web/src/components/ui/";

function git(args) {
  return execFileSync("git", args, {
    cwd: repoRoot,
    encoding: "utf8",
    stdio: ["ignore", "pipe", "pipe"],
  }).trim();
}

function walk(directory) {
  return readdirSync(directory).flatMap((entry) => {
    const path = resolve(directory, entry);
    return statSync(path).isDirectory() ? walk(path) : [path];
  });
}

function changedFiles() {
  const explicitBase = process.env.UI_SYSTEM_BASE_SHA?.trim();
  const branchBase = process.env.GITHUB_BASE_REF?.trim();
  const base = explicitBase || (branchBase ? `origin/${branchBase}` : "origin/main");
  let mergeBase;
  try {
    mergeBase = git(["merge-base", "HEAD", base]);
  } catch {
    throw new Error(
      `Could not resolve the UI design-system base (${base}). Fetch the base branch or set UI_SYSTEM_BASE_SHA.`,
    );
  }
  const tracked = git([
    "diff",
    "--name-only",
    "--diff-filter=ACMR",
    mergeBase,
    "--",
    "apps/web/src",
  ]).split("\n");
  const untracked = git([
    "ls-files",
    "--others",
    "--exclude-standard",
    "--",
    "apps/web/src",
  ]).split("\n");
  return [...new Set([...tracked, ...untracked].filter(Boolean))];
}

function requestedFiles() {
  const fileArgument = process.argv.find((argument) => argument.startsWith("--files="));
  if (fileArgument) return fileArgument.slice("--files=".length).split(",").filter(Boolean);
  if (process.argv.includes("--all")) {
    return walk(webSource).map((path) => relative(repoRoot, path));
  }
  return changedFiles();
}

function lineAt(source, offset) {
  return source.slice(0, offset).split("\n").length;
}

function addMatch(violations, source, file, rule, match, guidance) {
  violations.push({
    file,
    line: lineAt(source, match.index ?? 0),
    rule,
    excerpt: match[0].replace(/\s+/g, " ").trim().slice(0, 140),
    guidance,
  });
}

function inspect(file) {
  if (!file.endsWith(".tsx") || file.startsWith(canonicalUi)) return [];
  const absolute = resolve(repoRoot, file);
  if (!existsSync(absolute)) return [];
  const source = readFileSync(absolute, "utf8");
  const violations = [];

  for (const match of source.matchAll(/<(button|input|select|textarea|details|summary)\b[\s\S]*?>/g)) {
    if (/data-ui-bespoke-reason\s*=\s*["'][^"']+["']/.test(match[0])) continue;
    addMatch(
      violations,
      source,
      file,
      "shared-control",
      match,
      "Use the matching component in @/components/ui. A genuinely unique interaction needs data-ui-bespoke-reason and review evidence.",
    );
  }

  for (const match of source.matchAll(/\bconst\s+[A-Z0-9_]*(?:INPUT|LABEL|FIELD|HELP|BUTTON|CHECKBOX)[A-Z0-9_]*\s*=/g)) {
    addMatch(
      violations,
      source,
      file,
      "page-local-control-style",
      match,
      "Move control styling into an existing or new shared UI primitive.",
    );
  }

  for (const match of source.matchAll(/#[0-9a-fA-F]{3,8}\b|(?:rgb|hsl)a?\s*\(/g)) {
    addMatch(
      violations,
      source,
      file,
      "hard-coded-color",
      match,
      "Use an existing semantic color token. Add a token centrally when the design system genuinely needs a new role.",
    );
  }

  for (const match of source.matchAll(/<(?:span|div)\b[^>]*className\s*=\s*["'][^"']*rounded-full[^"']*(?:uppercase|tracking-)[^"']*["'][^>]*>/g)) {
    addMatch(
      violations,
      source,
      file,
      "bespoke-status-pill",
      match,
      "Use the shared Badge component for compact status.",
    );
  }

  return violations;
}

let files;
try {
  files = requestedFiles().filter(
    (file) => file.startsWith("apps/web/src/") && file.endsWith(".tsx"),
  );
} catch (error) {
  console.error(`[design-system] ${error instanceof Error ? error.message : String(error)}`);
  process.exit(1);
}

const violations = files.flatMap(inspect);
if (violations.length > 0) {
  console.error("[design-system] UI drift detected:\n");
  for (const violation of violations) {
    console.error(`${violation.file}:${violation.line} [${violation.rule}] ${violation.excerpt}`);
    console.error(`  ${violation.guidance}`);
  }
  process.exit(1);
}

console.log(`[design-system] ${files.length} changed UI file${files.length === 1 ? "" : "s"} follow the shared-control contract.`);
