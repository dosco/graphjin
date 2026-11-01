import { existsSync, statSync } from "node:fs";
import { describe, expect, it } from "vitest";
import { generateDeployKey, hostKeyFingerprints, withSshEnv } from "../src/config-vcs/ssh";

describe("SSH deploy keys", () => {
  it("generates an ed25519 key pair in OpenSSH format", async () => {
    const key = await generateDeployKey("openneko-org-1");
    expect(key.privateKey).toContain("BEGIN OPENSSH PRIVATE KEY");
    expect(key.publicKey).toMatch(/^ssh-ed25519 \S+ openneko-org-1$/);
  });

  it("prints SHA256 fingerprints of known host keys", async () => {
    const key = await generateDeployKey("host");
    const [type, body] = key.publicKey.split(" ");
    const fingerprints = await hostKeyFingerprints(`git.acme.local ${type} ${body}\n`);
    expect(fingerprints).toEqual([expect.stringMatching(/^ED25519 SHA256:\S+$/)]);
    expect(await hostKeyFingerprints("")).toEqual([]);
  });

  it("gives git one private key and the pinned host keys, then removes them", async () => {
    let keyPath = "";
    await withSshEnv({ privateKey: "PRIVATE", knownHosts: "host ssh-ed25519 AAAA\n" }, async (env) => {
      const command = env.GIT_SSH_COMMAND!;
      expect(command).toContain("StrictHostKeyChecking=yes");
      expect(command).toContain("IdentitiesOnly=yes");
      expect(command).toContain("BatchMode=yes");
      expect(command).not.toContain("PRIVATE");
      keyPath = command.split(" -i ")[1]!.split(" ")[0]!;
      expect(statSync(keyPath).mode & 0o777).toBe(0o600);
    });
    expect(existsSync(keyPath)).toBe(false);
  });
});
