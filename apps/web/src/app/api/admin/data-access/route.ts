import { NextResponse } from "next/server";
import { getGroupGrantsEnabled, listDataAccessRules, upsertDataAccessRule } from "@neko/db";
import { parseRowFilter, RowFilterError } from "@neko/llm/graphjin";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { groupErrorResponse, readJsonBody, scheduleGroupGrantsApply } from "@/lib/groups-admin";

export async function GET(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const orgId = await getOrgId();
  const groupId = new URL(request.url).searchParams.get("groupId") ?? undefined;
  const [rules, enabled] = await Promise.all([listDataAccessRules(orgId, groupId), getGroupGrantsEnabled(orgId)]);
  return NextResponse.json({ rules, enabled });
}

export async function POST(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const body = await readJsonBody(request);
  if (
    !body ||
    typeof body.groupId !== "string" ||
    typeof body.source !== "string" ||
    typeof body.tableName !== "string" ||
    !Array.isArray(body.columns) ||
    !body.columns.every((c) => typeof c === "string")
  ) {
    return NextResponse.json({ error: "groupId, source, tableName and columns are required" }, { status: 400 });
  }
  let rowFilter: unknown = null;
  if (body.rowFilter != null) {
    try {
      rowFilter = parseRowFilter(body.rowFilter);
    } catch (error) {
      if (error instanceof RowFilterError) return NextResponse.json({ error: error.message }, { status: 400 });
      throw error;
    }
  }
  try {
    const rule = await upsertDataAccessRule(await getOrgId(), {
      groupId: body.groupId,
      source: body.source,
      tableSchema: typeof body.tableSchema === "string" ? body.tableSchema : "",
      tableName: body.tableName,
      columns: body.columns as string[],
      rowFilter,
      actorUserId: actor.userId,
    });
    await scheduleGroupGrantsApply();
    return NextResponse.json({ rule }, { status: 201 });
  } catch (error) {
    return groupErrorResponse(error);
  }
}
