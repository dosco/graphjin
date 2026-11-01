import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { pool } from "@neko/db";
import {
  confirmContextRemoteHostKey,
  deleteContextRemote,
  getContextRemote,
  publishOrgContext,
  saveContextRemote,
} from "../src/config-vcs/remote-settings";

async function dbReachable(): Promise<boolean> {
  try {
    await pool().query("select 1 from context_remote limit 1");
    return true;
  } catch {
    return false;
  }
}

const describeIfDb = (await dbReachable()) ? describe : describe.skip;

async function withOrg<T>(fn: (orgId: string) => Promise<T>): Promise<T> {
  const orgId = `remote-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 8)}`;
  await pool().query("insert into organization (id, name) values ($1, 'Remote test')", [orgId]);
  try {
    return await fn(orgId);
  } finally {
    await pool().query("delete from organization where id = $1", [orgId]);
  }
}

const draft = {
  url: "https://github.com/acme/openneko-context.git",
  mode: "pull_request" as const,
  branch: "main",
  kinds: ["skills" as const, "workflows" as const],
};

describeIfDb("context remote settings", () => {
  it("saves the remote, stores the token encrypted and never returns it", async () => {
    await withOrg(async (orgId) => {
      const saved = await saveContextRemote(orgId, null, { ...draft, token: "ghp_plain" });
      expect(saved).toMatchObject({ provider: "github", mode: "pull_request", hasToken: true, lastPublish: null });
      expect(JSON.stringify(saved)).not.toContain("ghp_plain");
      const { rows } = await pool().query<{ token: string }>("select token from context_remote where org_id = $1", [orgId]);
      expect(rows[0].token).not.toBe("ghp_plain");

      expect((await saveContextRemote(orgId, null, { ...draft, branch: "release" })).hasToken).toBe(true);
      expect((await saveContextRemote(orgId, null, { ...draft, token: null })).hasToken).toBe(false);

      await deleteContextRemote(orgId, null);
      expect(await getContextRemote(orgId)).toBeNull();
    });
  });

  it("rejects an address with credentials, a bad branch, or no kinds", async () => {
    await withOrg(async (orgId) => {
      await expect(saveContextRemote(orgId, null, { ...draft, url: "https://u:p@github.com/a/b.git" })).rejects.toThrow("token field");
      await expect(saveContextRemote(orgId, null, { ...draft, branch: "a..b" })).rejects.toThrow("branch");
      await expect(saveContextRemote(orgId, null, { ...draft, kinds: [] })).rejects.toThrow("at least one");
    });
  });

  it("records a failed publish", async () => {
    await withOrg(async (orgId) => {
      const orgRoot = await mkdtemp(join(tmpdir(), "remote-settings-"));
      try {
        await saveContextRemote(orgId, null, draft);
        await expect(publishOrgContext(orgId, orgRoot, null)).rejects.toThrow("nothing to publish");
        expect((await getContextRemote(orgId))?.lastPublish).toMatchObject({ status: "failed", detail: "There is nothing to publish yet." });
      } finally {
        await rm(orgRoot, { recursive: true, force: true });
      }
    });
  });

  it("refuses an SSH server it cannot reach, and waits for host key confirmation before connecting", async () => {
    await withOrg(async (orgId) => {
      await expect(saveContextRemote(orgId, null, { ...draft, url: "ssh://git@127.0.0.1:1/acme/context.git" }))
        .rejects.toThrow("could not read the SSH host key");

      await pool().query(
        `insert into context_remote (org_id, url, mode, branch, kinds, ssh_private_key, ssh_public_key, ssh_known_hosts)
         values ($1, 'ssh://git@git.acme.local/acme/context.git', 'push', 'main', '["skills"]', 'key', 'ssh-ed25519 AAAA openneko', $2)`,
        [orgId, "git.acme.local ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOMqqnkVzrm0SdG6UOoqKLsabgH5C9okWi0dh2l9GKJl\n"],
      );
      const orgRoot = await mkdtemp(join(tmpdir(), "remote-settings-"));
      try {
        expect((await getContextRemote(orgId))?.ssh).toMatchObject({ hostConfirmed: false, hostFingerprints: [expect.stringMatching(/^ED25519 SHA256:/)] });
        await expect(publishOrgContext(orgId, orgRoot, null)).rejects.toThrow("Confirm the server's SSH host key first.");
        expect((await confirmContextRemoteHostKey(orgId, null)).ssh?.hostConfirmed).toBe(true);
      } finally {
        await rm(orgRoot, { recursive: true, force: true });
      }
    });
  });
});
