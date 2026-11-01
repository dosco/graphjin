import { beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";

const {
  mockGetOrgId,
  mockGetCurrentActor,
  mockGetAuthorizedWorkRun,
  mockGetWorkRunEvents,
  mockReadRunArtifact,
} = vi.hoisted(() => ({
  mockGetOrgId: vi.fn(),
  mockGetCurrentActor: vi.fn(),
  mockGetAuthorizedWorkRun: vi.fn(),
  mockGetWorkRunEvents: vi.fn(),
  mockReadRunArtifact: vi.fn(),
}));

vi.mock("@/lib/db", () => ({ getOrgId: mockGetOrgId }));
vi.mock("@/lib/actor", () => ({ getCurrentActor: mockGetCurrentActor }));
vi.mock("@/lib/work-thread-auth", () => ({
  getAuthorizedWorkRun: mockGetAuthorizedWorkRun,
}));
vi.mock("@/lib/work-store", () => ({
  getWorkRunEvents: mockGetWorkRunEvents,
}));
vi.mock("@/lib/work-files", () => ({ readRunArtifact: mockReadRunArtifact }));

const runId = "11111111-1111-4111-8111-111111111111";

async function download(path: string[]) {
  const { GET } = await import("@/app/api/work/files/[...path]/route");
  return GET(new NextRequest("http://localhost:3000/test"), {
    params: Promise.resolve({ path }),
  });
}

describe("GET /api/work/files/[...path]", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetOrgId.mockResolvedValue("org-1");
    mockGetCurrentActor.mockResolvedValue({ userId: "user-1", role: "member" });
    mockGetAuthorizedWorkRun.mockResolvedValue({ id: runId });
    mockGetWorkRunEvents.mockResolvedValue([
      {
        type: "artifact",
        artifact: { path: `runs/${runId}/artifacts/quote.xlsx` },
      },
    ]);
    mockReadRunArtifact.mockResolvedValue({
      data: Buffer.from("workbook"),
      filename: "quote.xlsx",
      mimeType:
        "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    });
  });

  it("downloads an explicitly emitted artifact as an attachment", async () => {
    const response = await download([
      "runs",
      runId,
      "artifacts",
      "quote.xlsx",
    ]);
    expect(response.status).toBe(200);
    expect(response.headers.get("content-disposition")).toBe(
      'attachment; filename="quote.xlsx"',
    );
    expect(response.headers.get("x-content-type-options")).toBe("nosniff");
    expect(mockReadRunArtifact).toHaveBeenCalledWith(
      "org-1",
      runId,
      "quote.xlsx",
    );
  });

  it("blocks every non-artifact workspace path before reading a file", async () => {
    for (const path of [
      ["uploads", "thread-1", "input.csv"],
      ["memory", "MEMORY.md"],
      ["skills", "pdf", "SKILL.md"],
      ["runs", runId, "not-artifacts", "quote.xlsx"],
    ]) {
      const response = await download(path);
      expect(response.status, path.join("/")).toBe(404);
    }
    expect(mockReadRunArtifact).not.toHaveBeenCalled();
  });

  it("blocks a real run file that has no artifact event", async () => {
    mockGetWorkRunEvents.mockResolvedValue([]);
    const response = await download([
      "runs",
      runId,
      "artifacts",
      "unpublished.csv",
    ]);
    expect(response.status).toBe(404);
    expect(mockReadRunArtifact).not.toHaveBeenCalled();
  });
});
