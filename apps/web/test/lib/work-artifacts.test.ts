import { describe, expect, it } from "vitest";
import {
  canonicalRunArtifactPath,
  isEmittedRunArtifact,
} from "../../src/lib/work-artifacts";

describe("work artifact download authorization", () => {
  const runId = "11111111-1111-4111-8111-111111111111";

  it("accepts only the exact artifact path emitted by the authorized run", () => {
    const events = [
      {
        type: "artifact",
        artifact: {
          path: `/workspace/org/runs/${runId}/artifacts/quotes/final.xlsx`,
        },
      },
    ];
    expect(
      isEmittedRunArtifact(
        events,
        runId,
        `runs/${runId}/artifacts/quotes/final.xlsx`,
      ),
    ).toBe(true);
    expect(
      isEmittedRunArtifact(
        events,
        runId,
        `runs/${runId}/artifacts/quotes/draft.xlsx`,
      ),
    ).toBe(false);
  });

  it("rejects uploads, memory, other runs, traversal, and prefix tricks", () => {
    const rejected = [
      `uploads/thread-1/input.csv`,
      `memory/MEMORY.md`,
      `runs/other/artifacts/final.xlsx`,
      `runs/${runId}/artifacts/../secret.txt`,
      `runs/${runId}/artifacts//final.xlsx`,
      `runs/${runId}/artifacts`,
      `/tmp/runs/${runId}/artifacts-evil/final.xlsx`,
    ];
    for (const path of rejected) {
      expect(canonicalRunArtifactPath(path, runId), path).toBeNull();
    }
  });
});
