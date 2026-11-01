import { NextResponse } from "next/server";
import { getOrgId } from "@/lib/db";
import { filterToHeld } from "@/lib/entitlements";
import { listWorkSkills } from "@/lib/work-files";
import { getCurrentActor } from "@/lib/actor";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { importWorkSkillArchive, SkillArchiveError } from "@/lib/skill-archive";

export const runtime = "nodejs";

export async function GET() {
  const skills = await filterToHeld("skill", await listWorkSkills(await getOrgId()), (skill) => skill.name);
  const actor = await getCurrentActor();
  return NextResponse.json({ skills, canImport: actor.role === "admin" });
}

export async function POST(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const contentLength = Number(request.headers.get("content-length") ?? 0);
  if (contentLength > 17 * 1024 * 1024) {
    return NextResponse.json({ error: "Skill archive must be at most 16 MB." }, { status: 413 });
  }
  const form = await request.formData().catch(() => null);
  const file = form?.get("file");
  if (!(file instanceof File) || !/\.(?:skill|zip)$/i.test(file.name)) {
    return NextResponse.json({ error: "Choose a .skill or ZIP archive." }, { status: 400 });
  }
  if (file.size === 0 || file.size > 16 * 1024 * 1024) {
    return NextResponse.json({ error: "Skill archive must be at most 16 MB." }, { status: 413 });
  }
  try {
    const name = await importWorkSkillArchive(await getOrgId(), Buffer.from(await file.arrayBuffer()), actor.userId);
    return NextResponse.json({ name }, { status: 201 });
  } catch (error) {
    if (error instanceof SkillArchiveError) {
      return NextResponse.json({ error: error.message }, { status: error.status });
    }
    throw error;
  }
}
