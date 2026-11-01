import { mkdtemp, mkdir, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  projectWorkflowApiBatchRecord,
  runCompiledWorkflowApiBatch,
  workflowApiCsvCell,
} from "../src/workflows/api-batch";
import { resolveWorkflowApiWorkspacePath } from "../src/workflows/api-admission";
import type { CompiledWorkflowBatchContract } from "../src/workflows/api-contract";

const contract: CompiledWorkflowBatchContract = {
  version: 1,
  compiled: true,
  compiler: "workflow",
  recordsField: "records",
  columns: [
    { name: "Name", path: "account.name" },
    { name: "Formula", path: "formula" },
    { name: "Fallback", path: "missing", default: "n/a" },
  ],
};

let temporaryRoot = "";

beforeEach(async () => {
  temporaryRoot = await mkdtemp(join(tmpdir(), "openneko-workflow-api-"));
  vi.stubEnv("OPENNEKO_HOST_WEB_DEV", "1");
  vi.stubEnv("OPENNEKO_AGENT_HOME", temporaryRoot);
  vi.stubEnv("NODE_ENV", "development");
  vi.stubEnv("OPENNEKO_STACK_MODE", "host");
});

afterEach(async () => {
  vi.unstubAllEnvs();
  await rm(temporaryRoot, { recursive: true, force: true });
});

async function stage(lines: string[]): Promise<string> {
  const relativePath = join("runs", "run-1", "api-batch-input.ndjson");
  const absolutePath = resolveWorkflowApiWorkspacePath("org-1", relativePath);
  await mkdir(dirname(absolutePath), { recursive: true });
  await writeFile(absolutePath, `${lines.join("\n")}\n`, "utf8");
  return relativePath;
}

describe("compiled workflow API batch", () => {
  it("projects only compiler-approved paths", () => {
    expect(
      projectWorkflowApiBatchRecord(
        { account: { name: "Acme", hidden: "ignore" }, formula: 42 },
        contract,
      ),
    ).toEqual(["Acme", "42", "n/a"]);
  });

  it("quotes RFC 4180 cells and neutralizes spreadsheet formulas", () => {
    expect(workflowApiCsvCell("plain")).toBe("plain");
    expect(workflowApiCsvCell("a,b")).toBe('"a,b"');
    expect(workflowApiCsvCell('say "hi"')).toBe('"say ""hi"""');
    expect(workflowApiCsvCell("=1+1")).toBe("'=1+1");
    expect(workflowApiCsvCell("\t=1+1")).toBe("'\t=1+1");
  });

  it("streams NDJSON into one bounded CSV artifact with durable progress", async () => {
    const inputFilePath = await stage([
      JSON.stringify({ account: { name: "Acme" }, formula: "=1+1" }),
      JSON.stringify({ account: { name: "Globex, Inc." }, formula: "safe" }),
    ]);
    const progress: Array<Record<string, unknown>> = [];
    const result = await runCompiledWorkflowApiBatch({
      orgId: "org-1",
      workRunId: "run-1",
      inputFilePath,
      contract,
      acceptedRecords: 2,
      chunkSize: 1,
      maxInputBytes: 100_000,
      maxArtifactBytes: 100_000,
      onProgress: async (item) => {
        progress.push(item);
      },
    });

    const csv = await readFile(
      resolveWorkflowApiWorkspacePath("org-1", result.artifactPath),
      "utf8",
    );
    expect(csv).toBe(
      "Name,Formula,Fallback\r\nAcme,'=1+1,n/a\r\n\"Globex, Inc.\",safe,n/a\r\n",
    );
    expect(result.progress).toMatchObject({
      stage: "completed",
      acceptedRows: 2,
      processedRows: 2,
      finalRows: 2,
      chunkCount: 2,
    });
    expect(progress.at(-1)).toEqual(result.progress);
  });

  it("fails closed and removes a partial artifact when output exceeds its bound", async () => {
    const inputFilePath = await stage([
      JSON.stringify({ account: { name: "x".repeat(200) }, formula: "safe" }),
    ]);
    await expect(
      runCompiledWorkflowApiBatch({
        orgId: "org-1",
        workRunId: "run-1",
        inputFilePath,
        contract,
        acceptedRecords: 1,
        chunkSize: 1,
        maxInputBytes: 100_000,
        maxArtifactBytes: 64,
      }),
    ).rejects.toMatchObject({ code: "artifact_limit_exceeded", status: 413 });

    await expect(
      readFile(
        resolveWorkflowApiWorkspacePath(
          "org-1",
          join("runs", "run-1", "artifacts", "api-result.csv"),
        ),
      ),
    ).rejects.toMatchObject({ code: "ENOENT" });
  });
});
