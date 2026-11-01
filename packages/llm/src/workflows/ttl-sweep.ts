import { db, sql, workflow_output } from "@neko/db";

const DEFAULT_GRACE_SECONDS = 600;

export type SweepStaleOutputsResult = {
  deleted: number;
  graceSeconds: number;
};

/**
 * Delete workflow_output rows whose freshness has expired. Outputs with
 * a null `freshness_ttl_seconds` are kept forever — that's an explicit
 * "keep this forever" signal from the producer. A grace window (default
 * 10 minutes) gives in-flight subscribers a chance to read fresh
 * matches before the row disappears.
 *
 * Cascading FKs:
 *   - observation.source_output_id is ON DELETE SET NULL → observations
 *     remain as audit trail with a null source pointer.
 *   - workflow_output_source_observation.workflow_output_id is ON DELETE
 *     CASCADE → join rows pointing at the deleted output go too.
 */
export async function sweepStaleWorkflowOutputs(
  opts: { graceSeconds?: number; now?: Date } = {},
): Promise<SweepStaleOutputsResult> {
  const grace = opts.graceSeconds ?? DEFAULT_GRACE_SECONDS;
  const result = await db()
    .delete(workflow_output)
    .where(
      sql`${workflow_output.freshness_ttl_seconds} is not null
        and ${workflow_output.created_at}
          + (${workflow_output.freshness_ttl_seconds} * interval '1 second')
          < now() - (${grace} * interval '1 second')`,
    )
    .returning({ id: workflow_output.id });
  return { deleted: result.length, graceSeconds: grace };
}
