import { afterAll, describe, expect, it, vi } from "vitest";
import { and, db, eq, pool } from "@neko/db";
import { action_changeset, action_changeset_row, action_request } from "@neko/db";
import { dbReachable, withTestOrg } from "@neko/db/test-helpers";
import {
  approveActionRequest,
  canRetryActionRequest,
  createActionPolicy,
  createActionRequest,
  executeApprovedActionRequest,
  getActionRequest,
  InvalidActionStatusTransitionError,
  listActionExecutions,
  markActionRequestFailed,
  rejectActionRequest,
  retryActionRequest,
  registerActionAdapter,
  registerActionRequestCreatedHook,
  RetryableActionAdapterError,
  UnsafeActionRetryError,
  saveWorkflow,
  createWorkflowRun,
  updateActionRequestPayload,
} from "../../src/workflows";
import { createWorkRun, createWorkThread } from "../../src/work/store";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[action-flow] skipping: Postgres unreachable.");
}

async function setupWorkflowRun(orgId: string) {
  const { workflow } = await saveWorkflow({
    orgId,
    name: `wf-${Math.random().toString(36).slice(2, 7)}`,
    steps: [{ id: "s1", description: "act" }],
  });
  const thread = await createWorkThread(orgId, workflow.name);
  const workRun = await createWorkRun(orgId, thread.id, "ax");
  const wfRun = await createWorkflowRun({
    orgId,
    workflowId: workflow.id,
    threadId: thread.id,
    workRunId: workRun.id,
    triggerKind: "manual",
  });
  return { workflowId: workflow.id, workflowRunId: wfRun.id, threadId: thread.id, workRunId: workRun.id };
}

describeIfDb("action stack — approve → execute → executed", () => {
  afterAll(async () => {
    await pool().end();
  });

  it("approve transitions pending_approval → approved → executed via an explicitly registered test adapter", async () => {
    await withTestOrg(async (orgId) => {
      const { workflowRunId } = await setupWorkflowRun(orgId);
      const policy = await createActionPolicy({
        orgId,
        name: "external_test",
        description: "",
        appliesToKinds: [],
        appliesToScopes: ["external"],
        mode: "approval_required",
        riskThresholdAutoApprove: null,
        allowedTargets: null,
        deniedTargets: null,
        limits: {},
        approverRole: null,
        priority: 100,
        enabled: true,
      });
      const request = await createActionRequest({
        orgId,
        workflowRunId,
        policyId: policy.id,
        scope: "external",
        kind: "send_message",
        target: "ops-channel",
        payload: { text: "hello world" },
        riskLevel: "medium",
        status: "pending_approval",
        summary: "Notify ops",
      });
      expect(request.status).toBe("pending_approval");

      const approved = await approveActionRequest({
        id: request.id,
        orgId,
        approverUserId: null,
      });
      expect(approved.status).toBe("approved");

      registerActionAdapter("send_message", async () => ({ result: { delivered: true } }));
      const result = await executeApprovedActionRequest(orgId, request.id);
      expect(result.ok).toBe(true);

      const rows = await db()
        .select()
        .from(action_request)
        .where(and(eq(action_request.org_id, orgId), eq(action_request.id, request.id)));
      expect(rows[0]?.status).toBe("executed");
    });
  });

  it("finishes request preflight before returning an approvable request", async () => {
    await withTestOrg(async (orgId) => {
      const unregister = registerActionRequestCreatedHook(async (request) => {
        if (request.kind !== "test_preflight") return;
        return updateActionRequestPayload({
          id: request.id,
          orgId: request.orgId,
          payload: { ...request.payload, preview_hash: "sha256:test" },
        });
      });
      try {
        const request = await createActionRequest({
          orgId,
          scope: "internal",
          kind: "test_preflight",
          payload: { app: "support" },
          status: "pending_approval",
        });
        expect(request).toMatchObject({
          status: "pending_approval",
          payload: { app: "support", preview_hash: "sha256:test" },
        });
      } finally {
        unregister();
      }
    });
  });

  it("fails a request when its preflight cannot produce an approval artifact", async () => {
    await withTestOrg(async (orgId) => {
      const unregister = registerActionRequestCreatedHook(async (request) => {
        if (request.kind === "test_preflight_failure") {
          throw new Error("catalog unavailable");
        }
      });
      try {
        await expect(
          createActionRequest({
            orgId,
            scope: "internal",
            kind: "test_preflight_failure",
            status: "pending_approval",
          }),
        ).rejects.toThrow("catalog unavailable");
        const [failed] = await db()
          .select()
          .from(action_request)
          .where(
            and(
              eq(action_request.org_id, orgId),
              eq(action_request.kind, "test_preflight_failure"),
            ),
          );
        expect(failed).toMatchObject({
          status: "failed",
          rejection_reason: null,
          failure_reason: "action preflight failed: catalog unavailable",
        });
      } finally {
        unregister();
      }
    });
  });

  it("reject leaves status=rejected and execute throws", async () => {
    await withTestOrg(async (orgId) => {
      const { workflowRunId } = await setupWorkflowRun(orgId);
      const request = await createActionRequest({
        orgId,
        workflowRunId,
        scope: "external",
        kind: "send_message",
        payload: {},
        riskLevel: "low",
        status: "pending_approval",
        summary: "test",
      });
      const rejected = await rejectActionRequest({
        id: request.id,
        orgId,
        approverUserId: null,
        reason: "not now",
      });
      expect(rejected.status).toBe("rejected");
      expect(rejected.rejectionReason).toBe("not now");

      await expect(
        executeApprovedActionRequest(orgId, request.id),
      ).rejects.toThrow(/expected approved/);
    });
  });

  it("rejects approval of an already-approved request", async () => {
    await withTestOrg(async (orgId) => {
      const { workflowRunId } = await setupWorkflowRun(orgId);
      const r = await createActionRequest({
        orgId,
        workflowRunId,
        scope: "external",
        kind: "send_message",
        payload: {},
        status: "pending_approval",
        summary: "x",
      });
      await approveActionRequest({ id: r.id, orgId, approverUserId: null });
      await expect(
        approveActionRequest({ id: r.id, orgId, approverUserId: null }),
      ).rejects.toBeInstanceOf(InvalidActionStatusTransitionError);
    });
  });

  it("kind-specific adapter returns its provider outcome", async () => {
    await withTestOrg(async (orgId) => {
      const adapter = vi.fn().mockResolvedValue({
        commandOrOperation: "real:slack.postMessage",
        externalRef: "slack-1234",
        result: { ts: "1234.5678" },
      });
      registerActionAdapter("test_send_message", adapter);
      try {
        const { workflowRunId } = await setupWorkflowRun(orgId);
        const r = await createActionRequest({
          orgId,
          workflowRunId,
          scope: "external",
          kind: "test_send_message",
          payload: { text: "hi" },
          status: "approved",
          summary: "x",
        });
        const result = await executeApprovedActionRequest(orgId, r.id);
        expect(result.ok).toBe(true);
        expect(adapter).toHaveBeenCalledTimes(1);
        const rows = await db()
          .select()
          .from(action_request)
          .where(eq(action_request.id, r.id));
        expect(rows[0]?.status).toBe("executed");
      } finally {
        // Clean up the test adapter so other tests aren't affected.
        registerActionAdapter("test_send_message", async () => ({
          result: { mocked: true },
        }));
      }
    });
  });

  it("keeps approved requests retryable after an uncertain adapter attempt", async () => {
    await withTestOrg(async (orgId) => {
      let attempts = 0;
      registerActionAdapter("test_retryable_action", async () => {
        attempts += 1;
        if (attempts === 1) {
          throw new RetryableActionAdapterError("temporary upstream outage");
        }
        return { result: { recovered: true } };
      });
      try {
        const { workflowRunId } = await setupWorkflowRun(orgId);
        const request = await createActionRequest({
          orgId,
          workflowRunId,
          scope: "internal",
          kind: "test_retryable_action",
          payload: {},
          status: "approved",
          summary: "retry me",
        });

        await expect(
          executeApprovedActionRequest(orgId, request.id),
        ).rejects.toBeInstanceOf(RetryableActionAdapterError);
        await expect(getActionRequest(orgId, request.id)).resolves.toMatchObject({
          status: "approved",
        });
        await expect(executeApprovedActionRequest(orgId, request.id)).resolves.toMatchObject({
          ok: true,
          outcome: { result: { recovered: true } },
        });
        await expect(listActionExecutions(request.id)).resolves.toEqual([
          expect.objectContaining({ status: "succeeded" }),
          expect.objectContaining({
            status: "failed",
            error: "temporary upstream outage",
          }),
        ]);
      } finally {
        registerActionAdapter("test_retryable_action", async () => ({
          result: { mocked: true },
        }));
      }
    });
  });

  it("retries only change sets whose every row failed before any write", async () => {
    await withTestOrg(async (orgId) => {
      const request = await createActionRequest({
        orgId, scope: "external", kind: "magento.manage_catalog",
        payload: {}, status: "approved",
      });
      const [changeset] = await db().insert(action_changeset).values({
        org_id: orgId, action_request_id: request.id, domain: "catalog",
        operation_id: "magentoUpdateProduct", risk_class: 2,
        idempotency_key: `retry-${request.id}`, status: "failed",
      }).returning();
      await db().insert(action_changeset_row).values({
        changeset_id: changeset.id, row_index: 0, entity_ref: "SKU-1",
        operation_id: "magentoUpdateProduct", status: "failed",
        error: "Magento could not be reached before the write",
      });
      await markActionRequestFailed(request.id, "Magento could not be reached before the write");
      expect(await canRetryActionRequest(orgId, request.id)).toBe(true);

      const retried = await retryActionRequest({
        orgId, id: request.id, actor: { role: "admin", userId: null },
      });
      expect(retried).toMatchObject({ status: "approved", failureReason: null, rejectionReason: null });
      const [reset] = await db().select().from(action_changeset_row).where(eq(action_changeset_row.changeset_id, changeset.id));
      expect(reset).toMatchObject({ status: "draft", error: null });
      expect(await canRetryActionRequest(orgId, request.id)).toBe(false);
      await expect(retryActionRequest({
        orgId, id: request.id, actor: { role: "admin", userId: null },
      })).rejects.toBeInstanceOf(InvalidActionStatusTransitionError);
    });
  });

  it.each(["reconcile_required", "applied", "submitted"])(
    "blocks a retry when a change-set row is %s", async (rowStatus) => {
      await withTestOrg(async (orgId) => {
        const request = await createActionRequest({
          orgId, scope: "external", kind: "magento.manage_catalog",
          payload: {}, status: "approved",
        });
        const [changeset] = await db().insert(action_changeset).values({
          org_id: orgId, action_request_id: request.id, domain: "catalog",
          operation_id: "magentoUpdateProduct", risk_class: 2,
          idempotency_key: `retry-${request.id}`, status: "failed",
        }).returning();
        await db().insert(action_changeset_row).values({
          changeset_id: changeset.id, row_index: 0, entity_ref: "SKU-1",
          operation_id: "magentoUpdateProduct", status: rowStatus,
          started_at: new Date(),
        });
        await markActionRequestFailed(request.id, "write outcome is uncertain");
        expect(await canRetryActionRequest(orgId, request.id)).toBe(false);
        await expect(retryActionRequest({
          orgId, id: request.id, actor: { role: "admin", userId: null },
        })).rejects.toBeInstanceOf(UnsafeActionRetryError);
        expect((await getActionRequest(orgId, request.id))?.status).toBe("failed");
      });
    },
  );
});
