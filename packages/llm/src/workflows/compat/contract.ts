import { createHash, randomBytes } from "node:crypto";
import { canonicalJson } from "../../canonical-json";
import { WORKFLOW_API_INPUT_FILE } from "../api-contract";

export const RECKON_TOKEN_HEADER = "x-webhook-token";
export const RECKON_IDEMPOTENCY_HEADER = "idempotency-key";
export const RECKON_BODY_MAX_BYTES = 256 * 1024;
export const RECKON_MAX_BATCH_RECORDS = 1_000;
export const RECKON_WAIT_TIMEOUT_DEFAULT_MS = 120_000;
export const RECKON_WAIT_TIMEOUT_MAX_MS = 600_000;
export const RECKON_SYNC_WAITERS = 5;

export type ReckonExecutionMode = "single" | "batch";

/** Reckon returns the same 404 for an unknown workflow and a disabled webhook. */
export class ReckonWebhookError extends Error {
  constructor(
    readonly status: number,
    readonly body: Record<string, unknown>,
    readonly retryAfterSeconds?: number,
  ) {
    super(String(body.error ?? "reckon webhook error"));
    this.name = "ReckonWebhookError";
  }
}

export function reckonNotFound(): ReckonWebhookError {
  return new ReckonWebhookError(404, { error: "not found" });
}

export function parseReckonIdempotencyKey(value: string | null): string | null {
  const key = value?.trim() ?? "";
  if (!key || key.length > 200 || !/^[A-Za-z0-9._:-]+$/.test(key)) return null;
  return key;
}

export function parseReckonMode(value: string | null): ReckonExecutionMode | null {
  if (value === null || value === "" || value === "single") return "single";
  if (value === "batch") return "batch";
  return null;
}

export function reckonTokenMatches(presented: string, expectedSha256: string): boolean {
  const digest = createHash("sha256").update(presented).digest();
  const expected = Buffer.from(expectedSha256, "hex");
  if (digest.length !== expected.length) return false;
  let mismatch = 0;
  for (let i = 0; i < digest.length; i += 1) mismatch |= digest[i]! ^ expected[i]!;
  return mismatch === 0;
}

export function sha256Hex(value: string): string {
  return createHash("sha256").update(value).digest("hex");
}

export function filterTriggerParams(
  body: Record<string, unknown>,
  allow: string[] | null,
): Record<string, unknown> {
  if (!allow) return body;
  return Object.fromEntries(Object.entries(body).filter(([key]) => allow.includes(key)));
}

/** Reckon counts the largest array anywhere in the body, not just the top level. */
export function largestArrayLength(value: unknown): number {
  let largest = 0;
  const pending: unknown[] = [value];
  while (pending.length > 0) {
    const candidate = pending.pop();
    if (Array.isArray(candidate)) {
      largest = Math.max(largest, candidate.length);
      for (const child of candidate) pending.push(child);
    } else if (candidate && typeof candidate === "object") {
      for (const child of Object.values(candidate as Record<string, unknown>)) pending.push(child);
    }
  }
  return largest;
}

export function reckonRequestFingerprint(
  mode: ReckonExecutionMode,
  params: Record<string, unknown>,
): string {
  return sha256Hex(canonicalJson({ executionMode: mode, params }));
}

const ULID_ALPHABET = "0123456789ABCDEFGHJKMNPQRSTVWXYZ";

/** Reckon run ids are ULIDs, and callers store them, so keep the shape. */
export function ulid(now = Date.now()): string {
  let time = "";
  let remaining = now;
  for (let i = 0; i < 10; i += 1) {
    time = ULID_ALPHABET[remaining % 32]! + time;
    remaining = Math.floor(remaining / 32);
  }
  const random = randomBytes(16);
  let suffix = "";
  for (let i = 0; i < 16; i += 1) suffix += ULID_ALPHABET[random[i]! % 32]!;
  return time + suffix;
}

export function reckonResultUsage(input: { baseUrl: string; workflowId: string; runId: string }) {
  const baseUrl = input.baseUrl.replace(/\/+$/, "");
  const resultUrl = `/api/hooks/${input.workflowId}/result/${input.runId}`;
  const absoluteUrl = `${baseUrl}${resultUrl}`;
  const tokenInstruction = "<same webhook token used to start the run>";
  return {
    resultUrl,
    resultRequest: {
      method: "GET" as const,
      url: absoluteUrl,
      headers: { "X-Webhook-Token": tokenInstruction },
      curl: `curl -H 'X-Webhook-Token: ${tokenInstruction}' '${absoluteUrl}'`,
    },
  };
}

export function reckonArtifactName(mode: ReckonExecutionMode): string {
  return mode === "batch" ? "result.csv" : "result.json";
}

export function buildReckonSeedMessage(input: {
  mode: ReckonExecutionMode;
  params: Record<string, unknown>;
  startedAt: Date;
  batchChunkSize: number;
}): string {
  const artifact = reckonArtifactName(input.mode);
  const lines = [
    `[webhook run started at ${input.startedAt.toISOString()}] Begin executing the workflow.`,
    "",
    "Trigger parameters (JSON):",
    "```json",
    JSON.stringify(input.params, null, 2),
    "```",
    `The same parameters are saved as JSON in $RUN_DIR/${WORKFLOW_API_INPUT_FILE}. Read large inputs from that file.`,
    "",
    `[output] Write your single deliverable to $ARTIFACT_DIR/${artifact}. It is the only file the caller is served.`,
    "Keep every intermediate and row-level file under $RUN_DIR. Never print row data into the reply.",
  ];
  if (input.mode === "batch") {
    lines.push(
      "",
      `[batch] Execute the complete workflow in chunks of at most ${input.batchChunkSize.toLocaleString("en-US")} records.`,
      "Pass intermediate datasets between steps by file path, and publish exactly one CSV artifact.",
    );
  }
  lines.push("", "Reply with a short receipt only: counts and the artifact name.");
  return lines.join("\n");
}

export const RECKON_SEED_KEY = "openneko_reckon_webhook";

export function reckonSeedPayload(input: {
  mode: ReckonExecutionMode;
  params: Record<string, unknown>;
  message: string;
}): Record<string, unknown> {
  return {
    [RECKON_SEED_KEY]: { mode: input.mode, message: input.message },
    params: input.params,
  };
}

export function reckonSeedMessageFrom(payload: Record<string, unknown> | null): string | null {
  const seed = payload?.[RECKON_SEED_KEY];
  if (!seed || typeof seed !== "object") return null;
  const message = (seed as { message?: unknown }).message;
  return typeof message === "string" && message.trim() ? message : null;
}

export type ReckonRunStatus = "queued" | "running" | "completed" | "error" | "aborted" | "needs_input";

export function reckonRunStatus(status: string): ReckonRunStatus {
  switch (status) {
    case "queued":
      return "queued";
    case "running":
      return "running";
    case "completed":
      return "completed";
    case "cancelled":
      return "aborted";
    case "needs_input":
      return "needs_input";
    default:
      return "error";
  }
}
