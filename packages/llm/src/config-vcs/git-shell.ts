import { execFile } from "node:child_process";

/**
 * Thin shell wrapper over the `git` binary (already in the worker image).
 * Plumbing via porcelain commands; the per-org lock in lock.ts serializes
 * callers because the git index is not concurrency-safe.
 */
export async function git(
  cwd: string,
  args: string[],
  opts: { env?: Record<string, string>; input?: string } = {},
): Promise<{ stdout: string; stderr: string }> {
  return new Promise((resolve, reject) => {
    const child = execFile(
      "git",
      args,
      {
        cwd,
        env: {
          ...process.env,
          GIT_AUTHOR_NAME: "OpenNeko",
          GIT_AUTHOR_EMAIL: "auto@openneko.local",
          GIT_COMMITTER_NAME: "OpenNeko",
          GIT_COMMITTER_EMAIL: "auto@openneko.local",
          // Never pick up the host user's git config.
          GIT_CONFIG_GLOBAL: "/dev/null",
          GIT_CONFIG_SYSTEM: "/dev/null",
          ...(opts.env ?? {}),
        },
        maxBuffer: 8 * 1024 * 1024,
      },
      (err, stdout, stderr) => {
        if (err) {
          reject(
            new Error(
              `git ${args.join(" ")} failed: ${stderr || err.message}`,
            ),
          );
          return;
        }
        resolve({ stdout: String(stdout), stderr: String(stderr) });
      },
    );
    if (opts.input !== undefined) {
      child.stdin?.write(opts.input);
      child.stdin?.end();
    }
  });
}
