import { beforeEach, expect, it, vi } from "vitest";
const mocks = vi.hoisted(() => ({ browse: vi.fn(), parse: vi.fn(), actor: vi.fn() }));
vi.mock("@/lib/actor", () => ({ getCurrentActor: mocks.actor }));
vi.mock("@/lib/db", () => ({ getOrgId: async () => "trusted-org" }));
vi.mock("@neko/llm/work", () => ({ browseLibrary: mocks.browse, parseLibraryBrowseOptions: mocks.parse }));
import { GET as browse } from "@/app/api/library/route";

beforeEach(() => {
  vi.clearAllMocks();
  mocks.actor.mockResolvedValue({ userId: "trusted-user", role: "member" });
  mocks.parse.mockReturnValue({ query: "", status: "", sort: "recent", page: 1, pageSize: 50 });
  mocks.browse.mockResolvedValue({ total: 0, page: 1, pageSize: 50, documents: [] });
});

it("lists only the signed-in user's documents, whatever the query says", async () => {
  const response = await browse(new Request("http://localhost/api/library?userId=another-user&orgId=another-org"));
  expect(response.status).toBe(200);
  expect(mocks.browse).toHaveBeenCalledWith({ orgId: "trusted-org", userId: "trusted-user" }, expect.any(Object));
});

it("rejects invalid filters", async () => {
  mocks.parse.mockImplementation(() => { throw new Error("Invalid library filters."); });
  expect((await browse(new Request("http://localhost/api/library?sort=nope"))).status).toBe(400);
  expect(mocks.browse).not.toHaveBeenCalled();
});

it("reports a failed load without pretending the library is empty", async () => {
  mocks.browse.mockRejectedValue(new Error("db down"));
  expect((await browse(new Request("http://localhost/api/library"))).status).toBe(503);
});
