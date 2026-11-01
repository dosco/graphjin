/**
 * Per-test temporary HOME.
 *
 * provisionHostConfig writes to ~/.config/graphjin/client.json and
 * ~/Library/Application Support/graphjin/client.json (on macOS). Tests must
 * NOT touch the developer's real home dir, so we override HOME for the
 * duration of the test.
 */

import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";

export type TempHome = {
  path: string;
  cleanup: () => Promise<void>;
};

export async function makeTempHome(): Promise<TempHome> {
  const path = await mkdtemp(join(tmpdir(), "neko-test-home-"));
  const prevHome = process.env.HOME;
  process.env.HOME = path;

  return {
    path,
    cleanup: async () => {
      process.env.HOME = prevHome;
      await rm(path, { recursive: true, force: true });
    },
  };
}
