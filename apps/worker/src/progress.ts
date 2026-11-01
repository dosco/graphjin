/**
 * Shared progress writer for processing_job.
 *
 * Worker job handlers call this to push status messages that the web
 * client polls via /api/briefing/status?jobId=. Uses JSONB concat (`||`)
 * so existing keys survive an update.
 */

import { db, eq, processing_job, sql } from "@neko/db";

export async function updateProgress(
  jobId: string,
  message: string,
): Promise<void> {
  await db()
    .update(processing_job)
    .set({
      progress: sql`${processing_job.progress} || ${JSON.stringify({ message })}::jsonb`,
      updated_at: new Date(),
    })
    .where(eq(processing_job.id, jobId));
}
