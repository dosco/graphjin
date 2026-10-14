import { NextResponse } from "next/server";
import {
  WorkflowApiError,
  disableWorkflowApiAccess,
  enableWorkflowApiAccess,
  getLastWorkflowApiRequest,
  getWorkflowApiAccess,
  rotateWorkflowApiToken,
  updateWorkflowApiLimits,
} from "@neko/llm/workflows";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

type RouteContext = {
  params: Promise<{ workflowId: string }>;
};

function response(body: unknown, status = 200): NextResponse {
  return NextResponse.json(body, {
    status,
    headers: { "Cache-Control": "no-store, private" },
  });
}
function failure(error: unknown): NextResponse {
  if (error instanceof WorkflowApiError) {
    return response(
      { error: { code: error.code, message: error.message } },
      error.status,
    );
  }
  console.error(
    `[workflow-api-admin] request failed: ${error instanceof Error ? error.message : String(error)}`,
  );
  return response(
    { error: { code: "internal_error", message: "Request failed." } },
    500,
  );
}

async function adminContext(context: RouteContext) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return { denied: actor } as const;
  const [{ workflowId }, orgId] = await Promise.all([
    context.params,
    getOrgId(),
  ]);
  return { actor, workflowId, orgId } as const;
}

export async function GET(_request: Request, context: RouteContext) {
  const scope = await adminContext(context);
  if ("denied" in scope) return scope.denied;
  const [access, lastRequest] = await Promise.all([
    getWorkflowApiAccess(scope.orgId, scope.workflowId),
    getLastWorkflowApiRequest(scope.orgId, scope.workflowId),
  ]);
  if (!access) {
    return response(
      { error: { code: "workflow_not_found", message: "Workflow not found." } },
      404,
    );
  }
  return response({ access, lastRequest });
}

export async function POST(request: Request, context: RouteContext) {
  const scope = await adminContext(context);
  if ("denied" in scope) return scope.denied;
  try {
    const body = (await request.json().catch(() => ({}))) as {
      action?: unknown;
    };
    const action = body.action;
    if (action !== "enable" && action !== "rotate") {
      throw new WorkflowApiError(
        "invalid_action",
        "Action must be 'enable' or 'rotate'.",
        400,
      );
    }
    const result =
      action === "enable"
        ? await enableWorkflowApiAccess({
            orgId: scope.orgId,
            workflowId: scope.workflowId,
            actor: scope.actor,
          })
        : await rotateWorkflowApiToken({
            orgId: scope.orgId,
            workflowId: scope.workflowId,
            actor: scope.actor,
          });
    // `token` is deliberately present on this response only. GET can never
    // recover it because storage contains only its verifier.
    return response(result, action === "enable" ? 201 : 200);
  } catch (error) {
    return failure(error);
  }
}

export async function PATCH(request: Request, context: RouteContext) {
  const scope = await adminContext(context);
  if ("denied" in scope) return scope.denied;
  try {
    const body = (await request.json().catch(() => ({}))) as {
      limits?: unknown;
    };
    const access = await updateWorkflowApiLimits({
      orgId: scope.orgId,
      workflowId: scope.workflowId,
      actor: scope.actor,
      limits: body.limits,
    });
    return response({ access });
  } catch (error) {
    return failure(error);
  }
}

export async function DELETE(_request: Request, context: RouteContext) {
  const scope = await adminContext(context);
  if ("denied" in scope) return scope.denied;
  try {
    const access = await disableWorkflowApiAccess({
      orgId: scope.orgId,
      workflowId: scope.workflowId,
      actor: scope.actor,
    });
    return response({ access });
  } catch (error) {
    return failure(error);
  }
}
