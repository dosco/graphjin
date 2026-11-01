import { afterEach, expect, it, vi } from "vitest";
const mocks = vi.hoisted(() => ({ list: vi.fn(async () => []), fixture: vi.fn(async () => []) }));
vi.mock("next/server", () => ({ connection: async () => {} }));
vi.mock("@/lib/auth", () => ({ getCurrentUser: async () => ({ id: "alice" }), getAuthProvider: async () => ({}) }));
vi.mock("@/lib/actor", () => ({ getCurrentActor: async () => ({ userId: "alice", role: "member" }) }));
vi.mock("@neko/db", () => ({ getOrgId: async () => "org" }));
vi.mock("@/lib/entitlements", () => ({ heldItemIds: async () => "*" }));
vi.mock("@neko/llm/graphjin/pack-user-connections", () => ({ listPackUserConnections: mocks.list }));
vi.mock("@/lib/integrations", () => ({ getOperatorConnectStatus: async () => [], getDeploymentConnectStatus: async () => [], listConnectProviders: async () => [] }));
vi.mock("../src/app/integrations/visual-fixture", () => ({ personalConnectionsFixture: mocks.fixture }));
vi.mock("../src/app/integrations/IntegrationsList", () => ({ default: () => null }));
import IntegrationsPage from "../src/app/integrations/page";
afterEach(() => { vi.unstubAllEnvs(); vi.clearAllMocks(); });
it("uses installed account state on the normal route even when visual testing is enabled", async () => {
  vi.stubEnv("OPENNEKO_VISUAL_TEST", "true");
  const page = await IntegrationsPage({ searchParams: Promise.resolve({}) });
  expect(mocks.list).toHaveBeenCalledWith({ orgId: "org", userId: "alice" });
  expect(mocks.fixture).not.toHaveBeenCalled();
  expect(page.props.initial.personal).toEqual([]);
});
it("requires an explicit connection state to render visual fixtures", async () => {
  vi.stubEnv("OPENNEKO_VISUAL_TEST", "true");
  await IntegrationsPage({ searchParams: Promise.resolve({ state: "connected" }) });
  expect(mocks.fixture).toHaveBeenCalledWith("connected");
  expect(mocks.list).not.toHaveBeenCalled();
});
