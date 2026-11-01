import { afterAll, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";

const mocks = vi.hoisted(() => ({
  actor: vi.fn(),
  orgId: vi.fn(),
  settings: vi.fn(),
  stage: vi.fn(),
  list: vi.fn(),
  acquireLock: vi.fn(),
  assertMutable: vi.fn(),
  hasSource: vi.fn(),
}));

vi.mock("@/lib/actor", () => ({ getCurrentActor: mocks.actor }));
vi.mock("@/lib/db", () => ({ getOrgId: mocks.orgId }));
vi.mock("@neko/db", async () => {
  const actual = await vi.importActual<typeof import("@neko/db")>("@neko/db");
  return { ...actual, getGraphjinConfigSettingsForOrg: mocks.settings };
});
vi.mock("@neko/llm/graphjin/file-source-assets", async () => {
  const actual = await vi.importActual<
    typeof import("@neko/llm/graphjin/file-source-assets")
  >(
    "@neko/llm/graphjin/file-source-assets",
  );
  return {
    ...actual,
    stageManagedFileSourceFiles: mocks.stage,
    listManagedFileSourceFiles: mocks.list,
    acquireManagedFileSourceLock: mocks.acquireLock,
    assertManagedFileSourceMutable: mocks.assertMutable,
  };
});
vi.mock("@neko/llm/graphjin/persist-source-config", () => ({
  graphjinConfigHasSource: mocks.hasSource,
}));

describe("/api/settings/file-sources", () => {
  let POST: typeof import("@/app/api/settings/file-sources/route").POST;
  const previousConfig = process.env.OPENNEKO_GRAPHJIN_CONFIG;

  beforeAll(async () => {
    ({ POST } = await import("@/app/api/settings/file-sources/route"));
  });

  beforeEach(() => {
    vi.clearAllMocks();
    process.env.OPENNEKO_GRAPHJIN_CONFIG = "/graphjin-config/agentic.yml";
    mocks.actor.mockResolvedValue({ userId: "admin-1", role: "admin" });
    mocks.orgId.mockResolvedValue("org-1");
    mocks.settings.mockResolvedValue({ sourceConfigEnabled: true });
    mocks.acquireLock.mockResolvedValue(vi.fn());
    mocks.assertMutable.mockResolvedValue(undefined);
    mocks.hasSource.mockResolvedValue(false);
    mocks.stage.mockResolvedValue([
      {
        name: "report.csv",
        size: 12,
        contentType: "text/csv",
        checksumSha256: "a".repeat(64),
      },
    ]);
    mocks.list.mockResolvedValue([
      {
        name: "report.csv",
        size: 12,
        contentType: "application/octet-stream",
        checksumSha256: "a".repeat(64),
      },
    ]);
  });

  afterAll(() => {
    if (previousConfig === undefined) delete process.env.OPENNEKO_GRAPHJIN_CONFIG;
    else process.env.OPENNEKO_GRAPHJIN_CONFIG = previousConfig;
  });

  function uploadRequest() {
    const form = new FormData();
    form.set("sourceName", "documents");
    form.append("files", new File(["a,b\n1,2\n"], "report.csv", { type: "text/csv" }));
    return new NextRequest("http://localhost/api/settings/file-sources", {
      method: "POST",
      body: form,
    });
  }

  it("is undiscoverable to members", async () => {
    mocks.actor.mockResolvedValue({ userId: "member-1", role: "member" });
    const response = await POST(uploadRequest());
    expect(response.status).toBe(404);
    expect(mocks.stage).not.toHaveBeenCalled();
  });

  it("is unavailable when source configuration is disabled", async () => {
    mocks.settings.mockResolvedValue({ sourceConfigEnabled: false });
    const response = await POST(uploadRequest());
    expect(response.status).toBe(404);
    expect(mocks.stage).not.toHaveBeenCalled();
  });

  it("stages files into an org-scoped managed source", async () => {
    const response = await POST(uploadRequest());
    expect(response.status).toBe(201);
    expect(mocks.stage).toHaveBeenCalledWith(
      expect.objectContaining({
        configFile: "/graphjin-config/agentic.yml",
        orgId: "org-1",
        sourceName: "documents",
      }),
    );
    expect(mocks.list).toHaveBeenCalledWith({
      configFile: "/graphjin-config/agentic.yml",
      orgId: "org-1",
      sourceName: "documents",
    });
    await expect(response.json()).resolves.toMatchObject({
      managedLocalFiles: {
        sourceName: "documents",
        ready: true,
        files: [{ name: "report.csv" }],
      },
    });
  });

  it("keeps active source content behind the approval boundary", async () => {
    mocks.hasSource.mockResolvedValue(true);
    const response = await POST(uploadRequest());
    expect(response.status).toBe(400);
    expect(mocks.stage).not.toHaveBeenCalled();
    await expect(response.json()).resolves.toMatchObject({
      error: expect.stringMatching(/active/i),
    });
  });
});
