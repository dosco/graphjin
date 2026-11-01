// SEC7 — the rogue-agent standing test: seed activity past a threshold,
// run the sweep, and the alert fires (once per window, not per tick).

import { afterAll, beforeAll, describe, expect, it } from "vitest";
import {
  behavior_alert,
  control_plane_audit,
  db,
  eq,
  pool,
  work_memory_event,
} from "@neko/db";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import { runBehaviorSweep } from "../../src/work/behavior-monitor";
import { createWorkRun, createWorkThread } from "../../src/work/store";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[behavior-monitor] skipping: Postgres unreachable.");
}

const THRESHOLDS = {
  controlPlaneCallsPerRun: 5,
  actionRequestsPerHour: 1000,
  memoryWritesPerHour: 8,
};

describeIfDb("SEC7 behavior monitor", () => {
  const orgId = uniqueOrgId("sec7");
  let runId: string;

  beforeAll(async () => {
    await createTestOrg(orgId);
    const thread = await createWorkThread(orgId, "t", "web");
    const run = await createWorkRun(orgId, thread.id, "ax", {
      userId: null,
      role: "service",
    });
    runId = run.id;
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
  });

  it("quiet org → no alerts", async () => {
    expect(await runBehaviorSweep(orgId, THRESHOLDS)).toEqual([]);
  });

  it("a run hammering the control plane raises one alert per window", async () => {
    await db()
      .insert(control_plane_audit)
      .values(
        Array.from({ length: 9 }, () => ({
          org_id: orgId,
          run_id: runId,
          path: "/v1/memory/search",
          actor_role: "service",
          backend: "ax",
        })),
      );
    const first = await runBehaviorSweep(orgId, THRESHOLDS);
    expect(first).toHaveLength(1);
    expect(first[0]).toMatchObject({
      kind: "control_plane_call_volume",
      subject: runId,
      observed: 9,
      threshold: 5,
    });

    // Same window, same subject → deduped.
    expect(await runBehaviorSweep(orgId, THRESHOLDS)).toEqual([]);
    const rows = await db()
      .select()
      .from(behavior_alert)
      .where(eq(behavior_alert.org_id, orgId));
    expect(rows.filter((r) => r.kind === "control_plane_call_volume")).toHaveLength(1);
  });

  it("a memory-write burst raises the volume alert", async () => {
    await db()
      .insert(work_memory_event)
      .values(
        Array.from({ length: 12 }, () => ({
          org_id: orgId,
          memory_id: null,
          run_id: null,
          thread_id: null,
          action: "remember",
          payload: {},
        })),
      );
    const alerts = await runBehaviorSweep(orgId, THRESHOLDS);
    expect(alerts.map((a) => a.kind)).toContain("memory_write_volume");
  });
});
