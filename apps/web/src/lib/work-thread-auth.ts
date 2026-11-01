import "server-only";

import type { RunActor } from "@neko/llm/work";
import { getCurrentActor } from "@/lib/actor";
import { getWorkRun, getWorkThread } from "@/lib/work-store";

type WorkThreadRow = NonNullable<Awaited<ReturnType<typeof getWorkThread>>>;
type WorkRunRow = NonNullable<Awaited<ReturnType<typeof getWorkRun>>>;

/**
 * Web Ask history is personal for every human role. Being an admin grants
 * configuration authority, not access to another person's conversation. The
 * solo profile has a persisted owner ID; its legacy null-owned web threads
 * are assigned to that owner during the automatic upgrade. Channel threads never use these web routes.
 */
export function canAccessWorkThread(
  actor: RunActor,
  thread: Pick<WorkThreadRow, "created_by_user_id">,
): boolean {
  if (actor.role === "service") return false;
  return thread.created_by_user_id === actor.userId;
}

export async function getAuthorizedWorkThread(
  orgId: string,
  threadId: string,
  actor?: RunActor,
): Promise<WorkThreadRow | null> {
  const effectiveActor = actor ?? (await getCurrentActor());
  const thread = await getWorkThread(orgId, threadId);
  return thread && canAccessWorkThread(effectiveActor, thread) ? thread : null;
}

export async function getAuthorizedWorkRun(
  orgId: string,
  runId: string,
  actor?: RunActor,
): Promise<WorkRunRow | null> {
  const effectiveActor = actor ?? (await getCurrentActor());
  const run = await getWorkRun(orgId, runId);
  if (!run) return null;
  const thread = await getAuthorizedWorkThread(
    orgId,
    run.thread_id,
    effectiveActor,
  );
  return thread ? run : null;
}
