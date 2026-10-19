import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { describe, expect, it } from "vitest";
import { app_user, db, eq, organization, pool } from "@neko/db";
import type { DirectoryChange, ListDirectoryResult } from "../src/directory/types";
import { createAdminHandler } from "../src/admin-server";
import {
  DirectorySyncError,
  collectDirectorySnapshot,
  createDirectoryUser,
  directoryStatus,
  runDirectorySync,
  type DirectorySource,
} from "../src/directory/directory-service";

const reachable = await pool().query("select 1").then(() => true, () => false);
const describeIfDb = reachable ? describe : describe.skip;

function source(pages: ListDirectoryResult[] | (() => never), changes: DirectoryChange[] = [], createUser = false): DirectorySource {
  return {
    getDirectoryProvider: () => ({
      id: "scalekit",
      capabilities: {
        providerLabel: "Scalekit",
        read: { users: true, groups: true, memberships: true },
        write: { createUser, deactivateUser: false },
      },
    }),
    listDirectory: async (cursor) => {
      if (typeof pages === "function") return pages();
      return pages[cursor ? Number(cursor) : 0]!;
    },
    applyDirectoryChange: async (change) => {
      changes.push(change);
      return { user: { externalId: "usr_1", email: change.op === "create_user" ? change.email : "", active: true } };
    },
  };
}

async function withOrg<T>(fn: (orgId: string) => Promise<T>): Promise<T> {
  const orgId = `dir-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
  await db().insert(organization).values({ id: orgId, name: "Directory" });
  try {
    return await fn(orgId);
  } finally {
    await db().delete(organization).where(eq(organization.id, orgId));
  }
}

describe("collectDirectorySnapshot", () => {
  it("merges pages and rejects a tenant change", async () => {
    const snapshot = await collectDirectorySnapshot(
      source([
        { tenantId: "t1", users: [{ externalId: "u1", email: "a@x.test", active: true }], groups: [], memberships: [], nextCursor: "1" },
        { tenantId: "t1", users: [], groups: [{ externalId: "g1", name: "Sales" }], memberships: [{ userExternalId: "u1", groupExternalId: "g1" }] },
      ]),
      "org",
    );
    expect(snapshot).toMatchObject({
      tenantId: "t1",
      provider: "scalekit",
      createUsers: true,
      groups: [{ externalId: "g1", displayName: "Sales" }],
      memberships: [{ userExternalId: "u1", groupExternalId: "g1" }],
    });
    await expect(
      collectDirectorySnapshot(
        source([
          { tenantId: "t1", users: [], groups: [], memberships: [], nextCursor: "1" },
          { tenantId: "t2", users: [], groups: [], memberships: [] },
        ]),
        "org",
      ),
    ).rejects.toThrow("changed tenant");
  });

  it("fails without a directory provider", async () => {
    await expect(
      collectDirectorySnapshot({ ...source([]), getDirectoryProvider: () => null }, "org"),
    ).rejects.toBeInstanceOf(DirectorySyncError);
  });
});

describe("createDirectoryUser", () => {
  it("creates the user only when the provider accepts the write", async () => {
    const changes: DirectoryChange[] = [];
    await expect(createDirectoryUser(source([], changes), { email: "a@x.test", name: null })).rejects.toMatchObject({ code: "unsupported" });
    expect(await createDirectoryUser(source([], changes, true), { email: "a@x.test", name: "Ana" })).toMatchObject({ user: { externalId: "usr_1" } });
    expect(changes).toEqual([{ op: "create_user", email: "a@x.test", name: "Ana" }]);
  });

  it("serves POST /admin/directory/users", async () => {
    const created: Array<{ email: string; name: string | null }> = [];
    const handler = createAdminHandler({
      directory: {
        status: async () => ({}),
        sync: async () => ({}),
        createUser: async (input) => {
          created.push(input);
          if (input.email.startsWith("no")) throw new DirectorySyncError("unsupported", "Scalekit does not create users");
          return { user: { externalId: "usr_1" } };
        },
      },
    });
    const server = createServer(handler);
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
    const post = (body: unknown) => fetch(`${base}/admin/directory/users`, { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify(body) });
    try {
      expect((await post({ email: " Ana@X.test ", name: "Ana" })).status).toBe(200);
      expect((await post({ email: "nope" })).status).toBe(400);
      expect((await post({ email: "no@x.test" })).status).toBe(400);
      expect((await fetch(`${base}/admin/directory/users`)).status).toBe(405);
      expect(created).toEqual([{ email: "ana@x.test", name: "Ana" }, { email: "no@x.test", name: null }]);
    } finally {
      await new Promise((resolve) => server.close(resolve));
    }
  });
});

describeIfDb("runDirectorySync", () => {
  it("records success and failure in directory_sync_state", async () => {
    await withOrg(async (orgId) => {
      await db().insert(app_user).values({ id: `${orgId}-owner`, org_id: orgId, email: "owner@x.test" });
      const stats = await runDirectorySync(
        source([{ tenantId: "t1", users: [{ externalId: "u1", email: "new@x.test", active: true }], groups: [], memberships: [] }]),
        orgId,
      );
      expect(stats.usersCreated).toBe(1);
      expect((await directoryStatus(source([]), orgId)).state).toMatchObject({ status: "ok", stats: { usersCreated: 1 } });

      await expect(runDirectorySync(source(() => { throw new Error("IdP down"); }), orgId)).rejects.toThrow("IdP down");
      const failed = await directoryStatus(null, orgId);
      expect(failed).toMatchObject({ provider: null, state: { status: "failed", lastError: "IdP down" } });
    });
  });

  it("refuses a second sync while one is running", async () => {
    await withOrg(async (orgId) => {
      await db().execute(`insert into directory_sync_state (org_id, status, started_at) values ('${orgId}', 'running', now())`);
      await expect(runDirectorySync(source([]), orgId)).rejects.toMatchObject({ code: "running" });
    });
  });
});

describe("directory admin routes", () => {
  it("serves status and maps sync errors to HTTP codes", async () => {
    const handler = createAdminHandler({
      directory: {
        status: async () => ({ provider: null, state: { status: "never" } }),
        sync: async () => {
          throw new DirectorySyncError("running", "a directory sync is already running");
        },
        createUser: async () => ({}),
      },
    });
    const server = createServer(handler);
    await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
    const base = `http://127.0.0.1:${(server.address() as AddressInfo).port}`;
    try {
      expect(await (await fetch(`${base}/admin/directory/status`)).json()).toEqual({ provider: null, state: { status: "never" } });
      const sync = await fetch(`${base}/admin/directory/sync`, { method: "POST" });
      expect(sync.status).toBe(409);
      expect((await fetch(`${base}/admin/directory/sync`)).status).toBe(405);
    } finally {
      await new Promise((resolve) => server.close(resolve));
    }
  });
});
