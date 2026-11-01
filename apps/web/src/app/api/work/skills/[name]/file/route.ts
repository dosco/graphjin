import { NextResponse } from "next/server";
import { getOrgId } from "@/lib/db";
import { requireItem } from "@/lib/entitlements";
import { readWorkSkillFile, writeWorkSkillFile } from "@/lib/work-files";

type RouteContext = {
  params: Promise<{ name: string }>;
};

export const runtime = "nodejs";

export async function GET(request: Request, context: RouteContext) {
  const { name } = await context.params;
  const denied = await requireItem("skill", decodeURIComponent(name), { notFound: "File not found" });
  if (denied) return denied;
  const path = new URL(request.url).searchParams.get("path");
  if (!path) {
    return NextResponse.json({ error: "Missing file path" }, { status: 400 });
  }
  const file = await readWorkSkillFile(await getOrgId(), decodeURIComponent(name), path);
  if (!file) {
    return NextResponse.json({ error: "File not found" }, { status: 404 });
  }
  return NextResponse.json({ file });
}

export async function PUT(request: Request, context: RouteContext) {
  const { name } = await context.params;
  const denied = await requireItem("skill", decodeURIComponent(name), { notFound: "File not found" });
  if (denied) return denied;
  const path = new URL(request.url).searchParams.get("path");
  if (!path) {
    return NextResponse.json({ error: "Missing file path" }, { status: 400 });
  }
  let body: { content?: unknown };
  try {
    body = (await request.json()) as { content?: unknown };
  } catch {
    return NextResponse.json({ error: "Invalid request body" }, { status: 400 });
  }
  if (typeof body.content !== "string") {
    return NextResponse.json({ error: "content must be a string" }, { status: 400 });
  }
  const result = await writeWorkSkillFile(
    await getOrgId(),
    decodeURIComponent(name),
    path,
    body.content,
  );
  if (!result.ok) {
    const status =
      result.reason === "not-found"
        ? 404
        : result.reason === "too-large"
          ? 413
          : result.reason === "binary"
            ? 415
            : 400;
    const message =
      result.reason === "not-found"
        ? "File not found"
        : result.reason === "too-large"
          ? "File is too large to save from here."
          : result.reason === "binary"
            ? "This file is not a text file and cannot be edited here."
            : "Invalid file path";
    return NextResponse.json({ error: message }, { status });
  }
  return NextResponse.json({ ok: true, bytes: result.bytes });
}
