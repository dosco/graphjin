import { sweepStaleWorkflowOutputs } from "@neko/llm/workflows";

export async function runWorkflowOutputTtlSweep(): Promise<void> {
  const result = await sweepStaleWorkflowOutputs();
  if (result.deleted > 0) {
    console.log(
      `[workflow-output-ttl] deleted ${result.deleted} stale output(s) (grace ${result.graceSeconds}s)`,
    );
  }
}
