import { execFile } from "node:child_process";
import { chmod, mkdtemp, readFile, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

function run(command: string, args: string[], timeoutMs = 20_000): Promise<string> {
  return new Promise((resolve, reject) => {
    execFile(command, args, { timeout: timeoutMs, encoding: "utf8" }, (error, stdout, stderr) =>
      error ? reject(new Error(`${command} failed: ${stderr || error.message}`)) : resolve(stdout),
    );
  });
}

async function withScratch<T>(fn: (directory: string) => Promise<T>): Promise<T> {
  const directory = await mkdtemp(join(tmpdir(), "neko-ssh-"));
  await chmod(directory, 0o700);
  try {
    return await fn(directory);
  } finally {
    await rm(directory, { recursive: true, force: true });
  }
}

/** A new ed25519 key pair in OpenSSH format, for use as a repository deploy key. */
export async function generateDeployKey(comment: string): Promise<{ privateKey: string; publicKey: string }> {
  return withScratch(async (directory) => {
    const path = join(directory, "key");
    await run("ssh-keygen", ["-q", "-t", "ed25519", "-N", "", "-C", comment, "-f", path]);
    return { privateKey: await readFile(path, "utf8"), publicKey: (await readFile(`${path}.pub`, "utf8")).trim() };
  });
}

/** The server's host keys in known_hosts format. */
export async function scanHostKeys(host: string, port: number): Promise<string> {
  const knownHosts = await run("ssh-keyscan", ["-T", "10", "-p", String(port), host]).catch(() => "");
  const lines = knownHosts.split("\n").filter((line) => line && !line.startsWith("#"));
  if (lines.length === 0) throw new Error(`OpenNeko could not read the SSH host key of ${host} on port ${port}.`);
  return `${lines.join("\n")}\n`;
}

/** SHA256 fingerprints of known_hosts lines, as `ssh-keygen -l` prints them. */
export async function hostKeyFingerprints(knownHosts: string): Promise<string[]> {
  if (!knownHosts.trim()) return [];
  return withScratch(async (directory) => {
    const path = join(directory, "known_hosts");
    await writeFile(path, knownHosts, { mode: 0o600 });
    const listed = await run("ssh-keygen", ["-l", "-E", "sha256", "-f", path]).catch(() => "");
    return listed.split("\n").filter(Boolean).map((line) => {
      const [, fingerprint, , type] = line.split(" ");
      return `${(type ?? "").replace(/[()]/g, "")} ${fingerprint}`.trim();
    });
  });
}

/**
 * Environment that makes git use one private key and only the pinned host
 * keys. The key is written to a private temporary file for the duration of fn.
 */
export async function withSshEnv<T>(
  ssh: { privateKey: string; knownHosts: string },
  fn: (env: Record<string, string>) => Promise<T>,
): Promise<T> {
  return withScratch(async (directory) => {
    const key = join(directory, "key");
    const knownHosts = join(directory, "known_hosts");
    await writeFile(key, ssh.privateKey.endsWith("\n") ? ssh.privateKey : `${ssh.privateKey}\n`, { mode: 0o600 });
    await writeFile(knownHosts, ssh.knownHosts, { mode: 0o600 });
    return fn({
      GIT_SSH_COMMAND: [
        "ssh", "-F", "/dev/null", "-i", key,
        "-o", "IdentitiesOnly=yes",
        "-o", "BatchMode=yes",
        "-o", "StrictHostKeyChecking=yes",
        "-o", `UserKnownHostsFile=${knownHosts}`,
        "-o", "GlobalKnownHostsFile=/dev/null",
      ].join(" "),
    });
  });
}
