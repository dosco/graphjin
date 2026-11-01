import { NextResponse } from "next/server";
import { getOrgId } from "@/lib/db";
import { requireItem } from "@/lib/entitlements";
import { deleteWorkSkill, getWorkSkillDetail } from "@/lib/work-files";

type RouteContext = {
  params: Promise<{ name: string }>;
};

export async function GET(_: Request, context: RouteContext) {
  const { name } = await context.params;
  const denied = await requireItem("skill", decodeURIComponent(name), { notFound: "Skill not found" });
  if (denied) return denied;
  const skill = await getWorkSkillDetail(await getOrgId(), decodeURIComponent(name));
  if (!skill) {
    return NextResponse.json({ error: "Skill not found" }, { status: 404 });
  }
  return NextResponse.json({ skill });
}

export async function DELETE(_: Request, context: RouteContext) {
  const { name } = await context.params;
  const denied = await requireItem("skill", decodeURIComponent(name), { notFound: "Skill not found" });
  if (denied) return denied;
  const ok = await deleteWorkSkill(await getOrgId(), decodeURIComponent(name));
  if (!ok) {
    return NextResponse.json({ error: "Skill not found" }, { status: 404 });
  }
  return NextResponse.json({ ok: true });
}
