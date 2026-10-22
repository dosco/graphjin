import { beforeEach, describe, expect, it, vi } from "vitest";

const mocks = vi.hoisted(() => {
  const metadata = {
    query: vi.fn(async (sql: string) => {
      void sql;
      return { rows: [], rowCount: 0 };
    }),
    release: vi.fn(),
  };
  return {
    metadata,
    writeLocalConfig: vi.fn(),
    reconnectPool: vi.fn(async () => undefined),
    hasCustomPassword: vi.fn(() => true),
    fetch: vi.fn(async () => new Response("restarting", { status: 202 })),
  };
});

vi.mock("@neko/db", () => ({
  hasCustomPassword: mocks.hasCustomPassword,
  pool: () => ({ connect: async () => mocks.metadata }),
  reconnectPool: mocks.reconnectPool,
  writeLocalConfig: mocks.writeLocalConfig,
}));

vi.mock("@/lib/admin-auth", () => ({
  requireAdminActor: async () => ({ userId: "admin-1", role: "admin" }),
  isDenied: () => false,
}));

import { GET, POST } from "@/app/api/admin/change-password/route";

function request(password: string): Request {
  return new Request("http://localhost/api/admin/change-password", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ password }),
  });
}

describe("admin database password rotation", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mocks.metadata.query.mockResolvedValue({ rows: [], rowCount: 0 });
    vi.stubGlobal("fetch", mocks.fetch);
  });

  it("rotates the database role before persisting and reconnecting", async () => {
    const response = await POST(request("A-safe-passphrase-2026") as never);

    expect(response.status).toBe(200);
    expect(mocks.metadata.query.mock.calls.map(([sql]) => sql)).toEqual([
      // The advisory lock brackets rotation AND config persistence, so two
      // concurrent rotations can't leave the file and the role divergent.
      "select pg_advisory_lock(hashtext('openneko.password-rotation'))",
      "alter role neko with password 'A-safe-passphrase-2026'",
      "select pg_advisory_unlock(hashtext('openneko.password-rotation'))",
    ]);
    expect(mocks.writeLocalConfig).toHaveBeenCalledWith({
      pg: { password: "A-safe-passphrase-2026" },
    });
    expect(mocks.reconnectPool).toHaveBeenCalledOnce();
    expect(mocks.metadata.release).toHaveBeenCalledOnce();
    expect(mocks.fetch).toHaveBeenCalledWith(
      "http://127.0.0.1:4100/admin/reconnect",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("never persists a password the database rejected", async () => {
    mocks.metadata.query.mockImplementation(async (sql: string) => {
      if (sql.startsWith("alter role")) throw new Error("database unavailable");
      return { rows: [], rowCount: 0 };
    });

    const response = await POST(request("A-safe-passphrase-2026") as never);

    expect(response.status).toBe(500);
    expect(mocks.writeLocalConfig).not.toHaveBeenCalled();
    expect(mocks.metadata.release).toHaveBeenCalledOnce();
  });

  it("reports whether the managed password has been changed", async () => {
    const response = await GET();
    await expect(response.json()).resolves.toEqual({ changed: true });
  });
});
