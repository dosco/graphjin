import { NextRequest, NextResponse } from "next/server";
import { getGraphjinConfigSettingsForOrg } from "@neko/db";
import {
  MAX_MANAGED_FILE_SIZE,
  MAX_MANAGED_FILE_UPLOAD_COUNT,
  MAX_MANAGED_FILE_UPLOAD_SIZE,
  acquireManagedFileSourceLock,
  assertManagedFileSourceMutable,
  listManagedFileSourceFiles,
  stageManagedFileSourceFiles,
} from "@neko/llm/graphjin/file-source-assets";
import { graphjinConfigHasSource } from "@neko/llm/graphjin/persist-source-config";
import { getCurrentActor } from "@/lib/actor";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

function unavailable() {
  return NextResponse.json({ error: "not found" }, { status: 404 });
}

export async function POST(request: NextRequest) {
  const actor = await getCurrentActor();
  if (actor.role !== "admin") return unavailable();
  const orgId = await getOrgId();
  if (!(await getGraphjinConfigSettingsForOrg(orgId)).sourceConfigEnabled) {
    return unavailable();
  }
  const configFile = process.env.OPENNEKO_GRAPHJIN_CONFIG?.trim();
  if (!configFile) {
    return NextResponse.json(
      { error: "managed GraphJin file storage is unavailable" },
      { status: 503 },
    );
  }
  const contentLength = Number(request.headers.get("content-length") ?? 0);
  if (contentLength > MAX_MANAGED_FILE_UPLOAD_SIZE + 1024 * 1024) {
    return NextResponse.json({ error: "file upload is too large" }, { status: 413 });
  }

  let releaseLock: (() => Promise<void>) | null = null;
  try {
    const form = await request.formData();
    const sourceName = String(form.get("sourceName") ?? "").trim();
    const files = form
      .getAll("files")
      .filter((value): value is File => value instanceof File);
    if (!sourceName || files.length === 0) {
      return NextResponse.json(
        { error: "sourceName and at least one file are required" },
        { status: 400 },
      );
    }
    if (files.length > MAX_MANAGED_FILE_UPLOAD_COUNT) {
      return NextResponse.json(
        { error: `upload no more than ${MAX_MANAGED_FILE_UPLOAD_COUNT} files at a time` },
        { status: 400 },
      );
    }
    for (const file of files) {
      if (file.size === 0 || file.size > MAX_MANAGED_FILE_SIZE) {
        return NextResponse.json(
          {
            error: `"${file.name}" must be between 1 byte and ${MAX_MANAGED_FILE_SIZE / (1024 * 1024)} MB`,
          },
          { status: 400 },
        );
      }
    }
    const localBase =
      process.env.OPENNEKO_GRAPHJIN_FILES_LOCAL_DIR?.trim() || undefined;
    releaseLock = await acquireManagedFileSourceLock({
      configFile,
      orgId,
      sourceName,
      localBase,
    });
    await assertManagedFileSourceMutable({
      configFile,
      orgId,
      sourceName,
      localBase,
    });
    if (await graphjinConfigHasSource({ configFile, sourceName })) {
      throw new Error(
        "managed file source is active; use a new source name for new content",
      );
    }
    await stageManagedFileSourceFiles({
      configFile,
      orgId,
      sourceName,
      localBase,
      files: await Promise.all(
        files.map(async (file) => ({
          name: file.name,
          content: new Uint8Array(await file.arrayBuffer()),
          contentType: file.type,
        })),
      ),
    });
    const managedFiles = await listManagedFileSourceFiles({
      configFile,
      orgId,
      sourceName,
      localBase,
    });
    return NextResponse.json(
      {
        managedLocalFiles: {
          sourceName,
          ready: true,
          files: managedFiles,
          totalSize: managedFiles.reduce((sum, file) => sum + file.size, 0),
        },
      },
      { status: 201 },
    );
  } catch (error) {
    return NextResponse.json(
      {
        error:
          error instanceof Error ? error.message : "managed file upload failed",
      },
      { status: 400 },
    );
  } finally {
    await releaseLock?.();
  }
}
