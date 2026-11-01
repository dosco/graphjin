import {
  action_request,
  and,
  behavior_alert,
  control_plane_audit,
  db,
  eq,
  gte,
  sql,
  work_memory_event,
} from "@neko/db";

/**
 * SEC7 — behavioral thresholds. Counts recent activity in the SEC5
 * audit stream and the action/memory write rates, and raises a
 * behavior_alert when an agent's behavior departs from its envelope.
 * Per (kind, subject) alerts dedupe within their window, so a runaway
 * run alerts once per window, not once per sweep tick. Each alert also
 * dispatches a `security.behavior_threshold` external event so OL3
 * subscriptions can page someone.
 */

function envInt(name: string, fallback: number): number {
  const v = Number(process.env[name]);
  return Number.isFinite(v) && v > 0 ? Math.floor(v) : fallback;
}

export type BehaviorThresholds = {
  /** Broker calls a single run may make in 10 minutes. */
  controlPlaneCallsPerRun: number;
  /** Action requests an org may accumulate in an hour. */
  actionRequestsPerHour: number;
  /** Memory writes (remember events) an org may accumulate in an hour. */
  memoryWritesPerHour: number;
};

export function behaviorThresholdsFromEnv(): BehaviorThresholds {
  return {
    controlPlaneCallsPerRun: envInt("OPENNEKO_BEHAVIOR_MAX_CP_CALLS_PER_RUN_10M", 200),
    actionRequestsPerHour: envInt("OPENNEKO_BEHAVIOR_MAX_ACTIONS_PER_HOUR", 50),
    memoryWritesPerHour: envInt("OPENNEKO_BEHAVIOR_MAX_MEMORY_WRITES_PER_HOUR", 100),
  };
}

export type BehaviorAlert = {
  kind: string;
  subject: string;
  observed: number;
  threshold: number;
  windowSeconds: number;
};

const CP_WINDOW_SECONDS = 10 * 60;
const HOUR_SECONDS = 60 * 60;

export async function runBehaviorSweep(
  orgId: string,
  thresholds: BehaviorThresholds = behaviorThresholdsFromEnv(),
): Promise<BehaviorAlert[]> {
  const raised: BehaviorAlert[] = [];

  const cpSince = new Date(Date.now() - CP_WINDOW_SECONDS * 1000);
  const byRun = await db()
    .select({
      runId: control_plane_audit.run_id,
      calls: sql<number>`count(*)::int`,
    })
    .from(control_plane_audit)
    .where(
      and(
        eq(control_plane_audit.org_id, orgId),
        gte(control_plane_audit.created_at, cpSince),
      ),
    )
    .groupBy(control_plane_audit.run_id);
  for (const row of byRun) {
    if (row.calls > thresholds.controlPlaneCallsPerRun) {
      raised.push({
        kind: "control_plane_call_volume",
        subject: row.runId ?? "",
        observed: row.calls,
        threshold: thresholds.controlPlaneCallsPerRun,
        windowSeconds: CP_WINDOW_SECONDS,
      });
    }
  }

  const hourSince = new Date(Date.now() - HOUR_SECONDS * 1000);
  const [actions] = await db()
    .select({ count: sql<number>`count(*)::int` })
    .from(action_request)
    .where(
      and(eq(action_request.org_id, orgId), gte(action_request.created_at, hourSince)),
    );
  if ((actions?.count ?? 0) > thresholds.actionRequestsPerHour) {
    raised.push({
      kind: "action_request_volume",
      subject: orgId,
      observed: actions.count,
      threshold: thresholds.actionRequestsPerHour,
      windowSeconds: HOUR_SECONDS,
    });
  }

  const [writes] = await db()
    .select({ count: sql<number>`count(*)::int` })
    .from(work_memory_event)
    .where(
      and(
        eq(work_memory_event.org_id, orgId),
        eq(work_memory_event.action, "remember"),
        gte(work_memory_event.created_at, hourSince),
      ),
    );
  if ((writes?.count ?? 0) > thresholds.memoryWritesPerHour) {
    raised.push({
      kind: "memory_write_volume",
      subject: orgId,
      observed: writes.count,
      threshold: thresholds.memoryWritesPerHour,
      windowSeconds: HOUR_SECONDS,
    });
  }

  const persisted: BehaviorAlert[] = [];
  for (const alert of raised) {
    if (await alreadyAlerted(orgId, alert)) continue;
    await db().insert(behavior_alert).values({
      org_id: orgId,
      kind: alert.kind,
      subject: alert.subject,
      observed: alert.observed,
      threshold: alert.threshold,
      window_seconds: alert.windowSeconds,
      details: {},
    });
    console.warn(
      `[behavior-monitor] ALERT org=${orgId} ${alert.kind} subject=${alert.subject} observed=${alert.observed} > threshold=${alert.threshold} in ${alert.windowSeconds}s`,
    );
    try {
      const { dispatchExternalEvent } = await import(
        "../workflows/external-events"
      );
      await dispatchExternalEvent({
        orgId,
        event: {
          name: "security.behavior_threshold",
          source: "behavior-monitor",
          payload: { ...alert },
          dedupeKey: `${alert.kind}:${alert.subject}:${Math.floor(Date.now() / (alert.windowSeconds * 1000))}`,
        },
      });
    } catch (err) {
      console.warn(
        `[behavior-monitor] external event dispatch failed: ${err instanceof Error ? err.message : err}`,
      );
    }
    persisted.push(alert);
  }
  return persisted;
}

/** One alert per (kind, subject) per window — re-raising every sweep tick is noise. */
async function alreadyAlerted(orgId: string, alert: BehaviorAlert): Promise<boolean> {
  const since = new Date(Date.now() - alert.windowSeconds * 1000);
  const [existing] = await db()
    .select({ id: behavior_alert.id })
    .from(behavior_alert)
    .where(
      and(
        eq(behavior_alert.org_id, orgId),
        eq(behavior_alert.kind, alert.kind),
        eq(behavior_alert.subject, alert.subject),
        gte(behavior_alert.created_at, since),
      ),
    )
    .limit(1);
  return Boolean(existing);
}
