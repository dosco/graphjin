import { NextResponse } from "next/server";
import { getOrgId } from "@/lib/db";
import {
  RecordFormExecutionError,
  RecordFormPolicyError,
  submitRecordDeleteAction,
} from "@/lib/records-actions";
import { recordsApiError } from "@/lib/records-api";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type RouteContext = {
  params: Promise<{ app: string; object: string; id: string }>;
};

export async function POST(_request: Request, context: RouteContext) {
  try {
    const [{ app, object, id }, orgId] = await Promise.all([
      context.params,
      getOrgId(),
    ]);
    const result = await submitRecordDeleteAction({
      orgId,
      appId: app,
      objectApiName: object,
      recordId: id,
    });
    return NextResponse.json(result, {
      status: result.status === "queued" ? 202 : 200,
    });
  } catch (error) {
    if (error instanceof RecordFormPolicyError) {
      return NextResponse.json({ error: error.message }, { status: 403 });
    }
    if (error instanceof RecordFormExecutionError) {
      return NextResponse.json(
        { error: error.message, actionRequestId: error.actionRequestId },
        { status: 422 },
      );
    }
    return recordsApiError(error);
  }
}
