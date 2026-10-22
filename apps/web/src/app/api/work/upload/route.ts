import { NextResponse } from "next/server";
import { saveLibrarySourceFile } from "@neko/llm/library/source-files";
import { createLibraryDocument } from "@neko/llm/work";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";
import { ALLOWED_UPLOAD_EXTENSIONS, MAX_UPLOAD_SIZE, saveWorkUpload } from "@/lib/work-files";
import { getAuthorizedWorkThread } from "@/lib/work-thread-auth";

export const runtime = "nodejs";

export async function POST(request: Request) {
  const contentLength = Number(request.headers.get("content-length") ?? 0);
  if (contentLength && contentLength > MAX_UPLOAD_SIZE + 4096) {
    return NextResponse.json(
      { error: `File is over ${Math.round(MAX_UPLOAD_SIZE / (1024 * 1024))} MB.` },
      { status: 413 },
    );
  }

  const form = await request.formData().catch(() => null);
  if (!form) {
    return NextResponse.json({ error: "Invalid multipart body" }, { status: 400 });
  }

  const threadId = String(form.get("threadId") ?? "").trim();
  const file = form.get("file");
  if (!threadId || !(file instanceof File)) {
    return NextResponse.json({ error: "threadId and file are required" }, { status: 400 });
  }
  if (file.type.startsWith("image/")) {
    return NextResponse.json({ error: "Image uploads are not supported yet." }, { status: 415 });
  }
  if (file.size > MAX_UPLOAD_SIZE) {
    return NextResponse.json(
      { error: `"${file.name}" is over ${Math.round(MAX_UPLOAD_SIZE / (1024 * 1024))} MB.` },
      { status: 413 },
    );
  }
  const ext = extractExtension(file.name);
  if (!ALLOWED_UPLOAD_EXTENSIONS.has(ext)) {
    return NextResponse.json(
      { error: `Unsupported file type "${ext || "(none)"}".` },
      { status: 415 },
    );
  }

  const orgId = await getOrgId();
  const thread = await getAuthorizedWorkThread(orgId, threadId);
  if (!thread) {
    return NextResponse.json({ error: "Thread not found" }, { status: 404 });
  }

  const saved = await saveWorkUpload(orgId, threadId, file);

  // Auto-add to the uploader's personal library: a copy in their folder of
  // the neko_library file source, which the agent can search and read in
  // later threads. Best-effort — a library hiccup must never fail the
  // upload itself. Callers can opt an upload out entirely with
  // catalog=false (no library record at all — the file stays thread-only).
  const catalog = String(form.get("catalog") ?? "true") !== "false";
  if (!catalog) {
    return NextResponse.json({
      file: {
        name: saved.name,
        size: saved.size,
        relativePath: saved.relativePath.replace(/\\/g, "/"),
        absolutePath: saved.absolutePath,
      },
    });
  }
  try {
    const actor = await getCurrentActor();
    const stored = await saveLibrarySourceFile({
      userId: actor.userId,
      filename: saved.name,
      data: Buffer.from(await file.arrayBuffer()),
    });
    await createLibraryDocument({
      orgId,
      userId: actor.userId,
      sourceThreadId: threadId,
      filename: saved.name,
      relativePath: stored.relativePath,
      contentHash: stored.contentHash,
      sizeBytes: stored.size,
      status: "ready",
    });
  } catch (error) {
    console.warn(
      `[work-upload] library auto-add failed (upload succeeded): ${error instanceof Error ? error.message : error}`,
    );
  }

  return NextResponse.json({
    file: {
      name: saved.name,
      size: saved.size,
      relativePath: saved.relativePath.replace(/\\/g, "/"),
      absolutePath: saved.absolutePath,
    },
  });
}

function extractExtension(name: string): string {
  const idx = name.lastIndexOf(".");
  if (idx <= 0) return "";
  return name.slice(idx).toLowerCase();
}
