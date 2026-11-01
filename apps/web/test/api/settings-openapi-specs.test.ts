import { beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import { NextRequest } from "next/server";
import { callRoute } from "../_helpers/route";

const mocks = vi.hoisted(() => ({
  actor: vi.fn(),
  orgId: vi.fn(),
  settings: vi.fn(),
  fromUrl: vi.fn(),
  fromUpload: vi.fn(),
  list: vi.fn(),
}));

vi.mock("@/lib/actor", () => ({ getCurrentActor: mocks.actor }));
vi.mock("@/lib/db", () => ({ getOrgId: mocks.orgId }));
vi.mock("@neko/db", async () => {
  const actual = await vi.importActual<typeof import("@neko/db")>("@neko/db");
  return { ...actual, getGraphjinConfigSettingsForOrg: mocks.settings };
});
vi.mock("@neko/llm/graphjin/openapi-assets", async () => {
  const actual = await vi.importActual<
    typeof import("@neko/llm/graphjin/openapi-assets")
  >(
    "@neko/llm/graphjin/openapi-assets",
  );
  return {
    ...actual,
    createOpenApiSpecAssetFromUrl: mocks.fromUrl,
    createOpenApiSpecAssetFromUpload: mocks.fromUpload,
    listOpenApiSpecAssets: mocks.list,
  };
});

const ASSET = {
  id: "4bc3b97b-d9bb-4d5f-908e-83ad1a4a7512",
  title: "Pet Service",
  operationCount: 2,
};

describe("/api/settings/openapi-specs", () => {
  let GET: typeof import("@/app/api/settings/openapi-specs/route").GET;
  let POST: typeof import("@/app/api/settings/openapi-specs/route").POST;

  beforeAll(async () => {
    ({ GET, POST } = await import("@/app/api/settings/openapi-specs/route"));
  });

  beforeEach(() => {
    vi.clearAllMocks();
    mocks.actor.mockResolvedValue({ userId: "admin-1", role: "admin" });
    mocks.orgId.mockResolvedValue("org-1");
    mocks.settings.mockResolvedValue({ sourceConfigEnabled: true });
    mocks.list.mockResolvedValue([ASSET]);
    mocks.fromUrl.mockResolvedValue(ASSET);
    mocks.fromUpload.mockResolvedValue(ASSET);
  });

  it("is undiscoverable to members", async () => {
    mocks.actor.mockResolvedValue({ userId: "member-1", role: "member" });
    const response = await callRoute(GET);
    expect(response.status).toBe(404);
    expect(mocks.list).not.toHaveBeenCalled();
  });

  it("is unavailable while source configuration is disabled", async () => {
    mocks.settings.mockResolvedValue({ sourceConfigEnabled: false });
    const response = await callRoute(GET);
    expect(response.status).toBe(404);
    expect(mocks.list).not.toHaveBeenCalled();
  });

  it("imports a hosted URL for the current org and admin", async () => {
    const response = await callRoute(POST, {
      method: "POST",
      body: {
        url: "https://example.com/openapi.yaml",
        baseUrl: "https://api.example.com/v1",
      },
    });
    expect(response.status).toBe(201);
    expect(mocks.fromUrl).toHaveBeenCalledWith({
      orgId: "org-1",
      createdByUserId: "admin-1",
      url: "https://example.com/openapi.yaml",
      baseUrl: "https://api.example.com/v1",
    });
  });

  it("accepts an uploaded YAML document", async () => {
    const form = new FormData();
    form.set(
      "file",
      new File(["openapi: 3.0.3\npaths: {}"], "openapi.yaml", {
        type: "application/yaml",
      }),
    );
    const request = new NextRequest("http://localhost/api/settings/openapi-specs", {
      method: "POST",
      body: form,
    });
    const response = await POST(request);
    expect(response.status, JSON.stringify(await response.clone().json())).toBe(201);
    expect(mocks.fromUpload).toHaveBeenCalledWith(
      expect.objectContaining({
        orgId: "org-1",
        createdByUserId: "admin-1",
        originalName: "openapi.yaml",
      }),
    );
  });
});
