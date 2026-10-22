// The current library design, driven through OpenNeko's own code: librarian
// extraction, distillation into concepts with the org's primary model, concept
// embeddings, and the search the agent's neko_library.search tool runs.
//
//   ingest <corpusDir> <orgId> <modelOrgId>   build the concept library, print a JSON report
//   serve <orgId>                             answer {"query","limit"} JSON lines on stdin
//   reset <orgId>                             delete the scratch org and everything in it
//
// Run from apps/worker with scripts/dev-env.sh sourced (database, librarian
// and embedding service on loopback).

import { copyFile, mkdir, readFile, readdir, rm, stat } from "node:fs/promises";
import { createHash } from "node:crypto";
import { join } from "node:path";
import { createInterface } from "node:readline";
import { pool } from "@neko/db";
import {
  createLibraryDocument,
  extractDirectText,
  fetchLibrarianExtraction,
  getOrgAgentRoot,
  libraryExtractionKind,
  librarianServiceUrl,
  pollLibrarianExtraction,
  runEmbeddingIndexJob,
  runLibraryDistill,
  searchLibraryByContext,
  submitLibrarianExtraction,
  type ExtractOutcome,
} from "@neko/llm";

async function ensureOrg(orgId: string, modelOrgId: string): Promise<void> {
  const client = await pool().connect();
  try {
    await client.query(`insert into organization (id, name) values ($1, $1) on conflict (id) do nothing`, [orgId]);
    // Distillation uses the org's primary model; reuse the model org's settings.
    await client.query(
      `insert into llm_provider_config (org_id, scope, provider, model, enabled, config, secrets)
       select $1, scope, provider, model, enabled, config, secrets
       from llm_provider_config where org_id = $2 and scope = 'primary'
       on conflict (org_id, scope) do nothing`,
      [orgId, modelOrgId],
    );
  } finally {
    client.release();
  }
}

async function extract(absolutePath: string, filename: string): Promise<ExtractOutcome> {
  if (libraryExtractionKind(filename) === "text") return extractDirectText(absolutePath);
  const baseUrl = librarianServiceUrl();
  if (!baseUrl) throw new Error("NEKO_LIBRARIAN_URL is not configured");
  const taskId = await submitLibrarianExtraction({ absolutePath, filename, baseUrl });
  for (let attempt = 0; attempt < 600; attempt++) {
    const status = await pollLibrarianExtraction(baseUrl, taskId);
    if (status.state === "success") return fetchLibrarianExtraction(baseUrl, taskId);
    if (status.state === "failure") return { ok: false, reason: status.error ?? "librarian failed" } as ExtractOutcome;
    await new Promise((r) => setTimeout(r, 1000));
  }
  throw new Error(`librarian timed out on ${filename}`);
}

async function ingest(corpusDir: string, orgId: string, modelOrgId: string): Promise<void> {
  await ensureOrg(orgId, modelOrgId);
  const filesDir = join(corpusDir, "files");
  const uploads = join(getOrgAgentRoot(orgId), "library", "uploads", "eval");
  await mkdir(uploads, { recursive: true });
  const report: Record<string, unknown>[] = [];
  for (const filename of (await readdir(filesDir)).sort()) {
    const started = Date.now();
    const entry: Record<string, unknown> = { file: filename };
    try {
      await copyFile(join(filesDir, filename), join(uploads, filename));
      const bytes = await readFile(join(uploads, filename));
      const { document } = await createLibraryDocument({
        orgId,
        userId: null,
        filename,
        relativePath: join("library", "uploads", "eval", filename),
        contentHash: createHash("sha256").update(bytes).digest("hex"),
        sizeBytes: (await stat(join(uploads, filename))).size,
      });
      const result = await runLibraryDistill({
        orgId,
        documentId: document.id,
        force: true,
        extract: ({ absolutePath }) => extract(absolutePath, filename),
      });
      for (const concept of result.concepts) {
        await runEmbeddingIndexJob({ kind: "concept", orgId, id: concept.id });
      }
      Object.assign(entry, { status: result.status, concepts: result.concepts.map((c) => c.path) });
    } catch (error) {
      entry.error = error instanceof Error ? error.message : String(error);
    }
    entry.seconds = Math.round((Date.now() - started) / 100) / 10;
    report.push(entry);
    console.error(`[concepts] ${filename}: ${JSON.stringify(entry)}`);
  }
  console.log(JSON.stringify({ orgId, files: report }));
}

async function serve(orgId: string): Promise<void> {
  const lines = createInterface({ input: process.stdin });
  for await (const line of lines) {
    if (!line.trim()) continue;
    try {
      const { query, limit } = JSON.parse(line) as { query: string; limit?: number };
      const results = await searchLibraryByContext({ orgId, userId: null, query, limit: limit ?? 5 });
      // The same fields neko_library.search returns to the agent.
      const out = results.map((r) => ({
        path: r.concept.path,
        type: r.concept.type,
        title: r.concept.title,
        description: r.concept.description,
        status: r.concept.status,
        layer: r.layer,
        score: r.score,
        sources: r.concept.sources.map((s) => s.resource),
        body: r.concept.body,
      }));
      process.stdout.write(`${JSON.stringify({ ok: true, results: out })}\n`);
    } catch (error) {
      process.stdout.write(`${JSON.stringify({ ok: false, error: error instanceof Error ? error.message : String(error) })}\n`);
    }
  }
}

async function reset(orgId: string): Promise<void> {
  if (!orgId.startsWith("doc-eval-")) throw new Error("reset only deletes doc-eval-* orgs");
  const client = await pool().connect();
  try {
    await client.query(`delete from organization where id = $1`, [orgId]);
  } finally {
    client.release();
  }
  await rm(getOrgAgentRoot(orgId), { recursive: true, force: true });
}

const [command, ...args] = process.argv.slice(2);
const run = { ingest: () => ingest(args[0]!, args[1]!, args[2]!), serve: () => serve(args[0]!), reset: () => reset(args[0]!) }[command ?? ""];
if (!run) {
  console.error("usage: concepts.ts ingest <corpusDir> <orgId> <modelOrgId> | serve <orgId> | reset <orgId>");
  process.exit(2);
}
run().then(
  async () => {
    await pool().end();
    process.exit(0);
  },
  (error) => {
    console.error(error);
    process.exit(1);
  },
);
