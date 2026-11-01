import { createReadStream } from "node:fs";
import { mkdir, open, stat, unlink } from "node:fs/promises";
import { dirname, join } from "node:path";
import { createInterface } from "node:readline";
import {
  WorkflowApiError,
  type CompiledWorkflowBatchColumn,
  type CompiledWorkflowBatchContract,
} from "./api-contract";
import { resolveWorkflowApiWorkspacePath } from "./api-admission";

function readPath(
  row: Record<string, unknown>,
  path: string,
): unknown {
  let value: unknown = row;
  for (const part of path.split(".")) {
    if (!value || typeof value !== "object" || Array.isArray(value)) {
      return undefined;
    }
    value = (value as Record<string, unknown>)[part];
  }
  return value;
}

function scalarValue(
  row: Record<string, unknown>,
  column: CompiledWorkflowBatchColumn,
): string {
  const raw = readPath(row, column.path) ?? column.default ?? "";
  if (raw === null) return "";
  if (typeof raw === "string") return raw;
  if (typeof raw === "number" || typeof raw === "boolean") return String(raw);
  return JSON.stringify(raw);
}

/** RFC 4180 quoting plus spreadsheet-formula neutralization. */
export function workflowApiCsvCell(value: string): string {
  const neutralized = /^[=+\-@\t\r]/.test(value) ? `'${value}` : value;
  return /[",\r\n]/.test(neutralized)
    ? `"${neutralized.replaceAll('"', '""')}"`
    : neutralized;
}

export function projectWorkflowApiBatchRecord(
  row: Record<string, unknown>,
  contract: CompiledWorkflowBatchContract,
): string[] {
  return contract.columns.map((column) => scalarValue(row, column));
}

export type WorkflowApiBatchProgress = {
  stage: "running" | "completed";
  acceptedRows: number;
  processedRows: number;
  finalRows: number;
  chunkCount: number;
  artifactBytes: number;
};

/**
 * Execute the workflow's compiled projection without a model call. Input and
 * output remain streams on the shared run filesystem; only one record and one
 * chunk's counters are held in memory at a time. Skills are neither inspected
 * nor required here.
 */
export async function runCompiledWorkflowApiBatch(input: {
  orgId: string;
  workRunId: string;
  inputFilePath: string;
  contract: CompiledWorkflowBatchContract;
  acceptedRecords: number;
  chunkSize: number;
  maxInputBytes: number;
  maxArtifactBytes: number;
  onProgress?: (progress: WorkflowApiBatchProgress) => Promise<void>;
}): Promise<{
  artifactPath: string;
  progress: WorkflowApiBatchProgress;
}> {
  const sourcePath = resolveWorkflowApiWorkspacePath(
    input.orgId,
    input.inputFilePath,
  );
  const sourceInfo = await stat(sourcePath);
  if (!sourceInfo.isFile() || sourceInfo.size > input.maxInputBytes) {
    throw new WorkflowApiError(
      "batch_input_unavailable",
      "The retained batch input is unavailable or exceeds its bound.",
      410,
    );
  }
  const artifactPath = join(
    "runs",
    input.workRunId,
    "artifacts",
    "api-result.csv",
  );
  const outputPath = resolveWorkflowApiWorkspacePath(input.orgId, artifactPath);
  await mkdir(dirname(outputPath), { recursive: true, mode: 0o700 });
  const output = await open(outputPath, "wx", 0o600);
  let artifactBytes = 0;
  let processedRows = 0;
  let chunkCount = 0;

  const writeBounded = async (text: string): Promise<void> => {
    const bytes = Buffer.byteLength(text, "utf8");
    if (artifactBytes + bytes > input.maxArtifactBytes) {
      throw new WorkflowApiError(
        "artifact_limit_exceeded",
        "The batch result exceeds this workflow's artifact limit.",
        413,
      );
    }
    await output.write(text);
    artifactBytes += bytes;
  };

  try {
    await writeBounded(
      `${input.contract.columns.map((column) => workflowApiCsvCell(column.name)).join(",")}\r\n`,
    );
    const lines = createInterface({
      input: createReadStream(sourcePath, { encoding: "utf8" }),
      crlfDelay: Infinity,
    });
    for await (const line of lines) {
      if (!line.trim()) continue;
      let parsed: unknown;
      try {
        parsed = JSON.parse(line);
      } catch {
        throw new WorkflowApiError(
          "invalid_batch_storage",
          "The retained batch input could not be decoded.",
          500,
        );
      }
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
        throw new WorkflowApiError(
          "invalid_batch_storage",
          "A retained batch record is not a JSON object.",
          500,
        );
      }
      const projected = projectWorkflowApiBatchRecord(
        parsed as Record<string, unknown>,
        input.contract,
      );
      await writeBounded(
        `${projected.map((value) => workflowApiCsvCell(value)).join(",")}\r\n`,
      );
      processedRows += 1;
      if (
        processedRows % input.chunkSize === 0 ||
        processedRows === input.acceptedRecords
      ) {
        chunkCount += 1;
        await input.onProgress?.({
          stage: "running",
          acceptedRows: input.acceptedRecords,
          processedRows,
          finalRows: processedRows,
          chunkCount,
          artifactBytes,
        });
      }
    }
    if (processedRows !== input.acceptedRecords) {
      throw new WorkflowApiError(
        "batch_record_mismatch",
        "The retained batch record count no longer matches admission.",
        500,
      );
    }
    const progress: WorkflowApiBatchProgress = {
      stage: "completed",
      acceptedRows: input.acceptedRecords,
      processedRows,
      finalRows: processedRows,
      chunkCount,
      artifactBytes,
    };
    await input.onProgress?.(progress);
    return { artifactPath, progress };
  } catch (error) {
    await output.close().catch(() => undefined);
    await unlink(outputPath).catch(() => undefined);
    throw error;
  } finally {
    await output.close().catch(() => undefined);
  }
}
