import { readFile, readdir, writeFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { parse, stringify } from "yaml";

const sourcePath = fileURLToPath(
  new URL("../../../evals/SEMANTICS.md", import.meta.url),
);
const outputPath = fileURLToPath(
  new URL("../../../evals/semantics.yaml", import.meta.url),
);
const evalRoot = fileURLToPath(new URL("../../../evals", import.meta.url));

async function yamlFiles(root: string): Promise<string[]> {
  const result: string[] = [];
  for (const entry of await readdir(root, { withFileTypes: true })) {
    const path = `${root}/${entry.name}`;
    if (entry.isDirectory()) result.push(...(await yamlFiles(path)));
    else if (/\.ya?ml$/u.test(entry.name)) result.push(path);
  }
  return result;
}

const evalCovered = new Set<string>();
for (const path of await yamlFiles(evalRoot)) {
  if (path === outputPath) continue;
  let document: Record<string, unknown>;
  try {
    document = parse(await readFile(path, "utf8")) as Record<string, unknown>;
  } catch {
    continue;
  }
  if (document.schema_version === "openneko.eval.case/v1") {
    for (const semantic of Array.isArray(document.semantics)
      ? document.semantics
      : []) {
      if (typeof semantic === "string") evalCovered.add(semantic);
    }
  }
  if (document.schema_version === "openneko.eval/v1") {
    for (const variant of Array.isArray(document.variants)
      ? document.variants
      : []) {
      const dataPath = (variant as { data_path?: unknown }).data_path;
      if (dataPath === "graphjin-direct") evalCovered.add("DATA-DIRECT");
      if (dataPath === "graphjin-agent") evalCovered.add("DATA-DELEGATED");
      if (dataPath === "planner-host-execute") {
        evalCovered.add("DATA-PLANNER-EXECUTE");
      }
    }
  }
}
const ownerBySection: Record<number, string[]> = {
  1: ["packages/llm/src/agent-backend.ts", "apps/worker/src"],
  2: ["packages/llm/src/graphjin", "packages/llm/src/work"],
  3: ["packages/llm/src", "apps/worker/src/jobs"],
  4: ["packages/llm/src/work"],
  5: ["packages/llm/src/workflows", "apps/worker/src/jobs"],
  6: ["packages/channels", "apps/worker/src/channels"],
  7: ["packages/records"],
  8: ["packages/llm/src/work", "packages/records/src/policy"],
  9: ["apps/worker/src", "apps/openneko/internal"],
};
const categoryBySection: Record<number, string> = {
  1: "runtime-backend",
  2: "data-calculation",
  3: "product-output",
  4: "work-context",
  5: "workflow-automation",
  6: "channels-plugins",
  7: "records",
  8: "security-governance",
  9: "operability-recovery",
};

const source = await readFile(sourcePath, "utf8");
let section = 0;
const entries: Array<{
  id: string;
  category: string;
  description: string;
  verification: string;
  disposition: "declared" | "eval";
  owners: string[];
}> = [];
for (const line of source.split("\n")) {
  const heading = /^##\s+(\d+)\./u.exec(line);
  if (heading) section = Number(heading[1]);
  const row = /^\|\s*`([A-Z0-9][A-Z0-9._-]*)`\s*\|\s*(.*?)\s*\|\s*(.*?)\s*\|\s*$/u.exec(
    line,
  );
  if (!row || !categoryBySection[section] || !ownerBySection[section]) continue;
  entries.push({
    id: row[1]!,
    category: categoryBySection[section]!,
    description: row[2]!,
    verification: row[3]!,
    disposition: evalCovered.has(row[1]!) ? "eval" : "declared",
    owners: [...ownerBySection[section]!],
  });
}
if (entries.length < 100) {
  throw new Error(`semantic extraction found only ${entries.length} entries`);
}
const duplicates = entries.filter(
  (entry, index) => entries.findIndex((candidate) => candidate.id === entry.id) !== index,
);
if (duplicates.length) {
  throw new Error(`duplicate semantics: ${duplicates.map((entry) => entry.id).join(", ")}`);
}
const known = new Set(entries.map((entry) => entry.id));
const unknownCoverage = [...evalCovered].filter((id) => !known.has(id));
if (unknownCoverage.length) {
  throw new Error(
    `cases/configs reference unknown semantics: ${unknownCoverage.join(", ")}`,
  );
}
await writeFile(
  outputPath,
  stringify({
    schema_version: "openneko.eval.semantics/v1",
    generated_from: "evals/SEMANTICS.md",
    entries,
  }),
  "utf8",
);
