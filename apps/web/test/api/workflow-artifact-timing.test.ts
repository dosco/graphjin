import { expect, it, vi } from "vitest";
import { mkdtemp, writeFile, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { NextRequest } from "next/server";
const state = vi.hoisted(() => ({ path: "" }));
vi.mock("@neko/llm/workflows", () => ({
  enforceWorkflowApiEdgeThrottle: async () => {},
  getWorkflowApiArtifact: async () => ({ absolutePath: state.path, bytes: 3, fileName: "result.csv", contentType: "text/csv" }),
  parseWorkflowApiBearer: () => "credential",
  workflowApiClientFingerprint: () => "fingerprint",
}));
import { GET } from "@/app/api/v1/workflows/[workflowId]/runs/[runId]/artifact/route";
it("preserves streamed bytes and records completion without the artifact path", async () => {
  const dir = await mkdtemp(join(tmpdir(), "workflow-timing-"));
  const log = vi.spyOn(console, "log").mockImplementation(() => {});
  try {
    state.path = join(dir, "result.csv");
    await writeFile(state.path, "x\n1");
    const response = await GET(new NextRequest("http://localhost/artifact"), { params: Promise.resolve({ workflowId: "workflow", runId: "run" }) });
    expect(await response.text()).toBe("x\n1");
    await vi.waitFor(() => expect(log.mock.calls.map(call => JSON.parse(String(call[0])))).toEqual(expect.arrayContaining([expect.objectContaining({ phase: "workflow.artifact_stream", workflowRunId: "run", outcome: "completed", bytesRead: 3 })])));
    expect(JSON.stringify(log.mock.calls)).not.toContain(state.path);
  } finally { log.mockRestore(); await rm(dir, { recursive: true, force: true }); }
});
