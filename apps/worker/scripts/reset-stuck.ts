import "dotenv/config";
import { db, eq, processing_job, pool } from "@neko/db";
import { boss } from "@neko/db/jobs";

(async () => {
  const b = await boss();
  const c = pool();
  const r = await c.query(
    `SELECT id, data->>'processingJobId' AS processing_job_id
     FROM pgboss.job WHERE state = $1 AND name = $2`,
    ["active", "metric_refresh"],
  );
  console.log("active jobs to reset:", r.rowCount);
  for (const row of r.rows) {
    await b.fail("metric_refresh", row.id, { reason: "worker restart, requeue" });
    if (row.processing_job_id) {
      await db()
        .update(processing_job)
        .set({ status: "queued", started_at: null, error: null })
        .where(eq(processing_job.id, row.processing_job_id));
    }
    console.log("reset", row.id, "→ pj", row.processing_job_id);
  }
  process.exit(0);
})();
