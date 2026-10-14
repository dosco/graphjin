import { NextResponse } from "next/server";
import { getOrgId } from "@/lib/db";
import { readJsonBody } from "@/lib/groups-admin";
import { RecordAdminPermissionError } from "@/lib/records";
import { recordsApiError } from "@/lib/records-api";
import { RecordFormExecutionError, RecordFormPolicyError } from "@/lib/records-actions";
import {
  RecordSchemaChangeRejectedError,
  SCHEMA_EDIT_KINDS,
  submitRecordSchemaChange,
  type SchemaEditKind,
} from "@/lib/records-schema-edit";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type Context = { params: Promise<{ app: string }> };

export async function POST(request: Request, context: Context) {
  try {
    const [{ app }, body, orgId] = await Promise.all([context.params, readJsonBody(request), getOrgId()]);
    const kind = body?.kind;
    const payload = body?.payload;
    if (
      typeof kind !== "string" ||
      !SCHEMA_EDIT_KINDS.includes(kind as SchemaEditKind) ||
      !payload ||
      typeof payload !== "object" ||
      Array.isArray(payload)
    ) {
      return NextResponse.json({ error: "kind and payload are required" }, { status: 400 });
    }
    const result = await submitRecordSchemaChange({
      orgId,
      appId: app,
      kind: kind as SchemaEditKind,
      payload: payload as Record<string, unknown>,
    });
    return NextResponse.json(result, { status: result.status === "queued" ? 202 : 200 });
  } catch (error) {
    if (error instanceof RecordAdminPermissionError) {
      return NextResponse.json({ error: error.message }, { status: 403 });
    }
    if (error instanceof RecordFormPolicyError) {
      return NextResponse.json({ error: error.message }, { status: 403 });
    }
    if (error instanceof RecordFormExecutionError) {
      return NextResponse.json({ error: error.message, actionRequestId: error.actionRequestId }, { status: 422 });
    }
    if (error instanceof RecordSchemaChangeRejectedError) {
      return NextResponse.json({ error: error.message }, { status: 400 });
    }
    return recordsApiError(error);
  }
}
