import { execFile } from "node:child_process";
import { access, mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { promisify } from "node:util";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const execFileAsync = promisify(execFile);
const REPO_ROOT = fileURLToPath(new URL("../../../", import.meta.url));
const PRUNE_SCRIPT = `${REPO_ROOT}scripts/prune-node-runtime.sh`;

async function exists(path: string): Promise<boolean> {
  try {
    await access(path);
    return true;
  } catch {
    return false;
  }
}

describe("production Node runtime pruning", () => {
  it("keeps ONNX CPU runtime files but removes GPU providers and install scripts", async () => {
    const root = await mkdtemp(join(tmpdir(), "openneko-node-runtime-"));
    const onnxRoot = join(
      root,
      "node_modules/.pnpm/onnxruntime-node@1.24.3/node_modules/onnxruntime-node",
    );
    const nativeRoot = join(onnxRoot, "bin/napi-v6/linux/x64");
    const scriptRoot = join(onnxRoot, "script");
    const files = {
      cpu: join(nativeRoot, "libonnxruntime.so.1"),
      shared: join(nativeRoot, "libonnxruntime_providers_shared.so"),
      cuda: join(nativeRoot, "libonnxruntime_providers_cuda.so"),
      tensorrt: join(nativeRoot, "libonnxruntime_providers_tensorrt.so"),
      metadata: join(scriptRoot, "install-metadata.js"),
      installer: join(scriptRoot, "install.js"),
    };

    try {
      await Promise.all([
        mkdir(nativeRoot, { recursive: true }),
        mkdir(scriptRoot, { recursive: true }),
      ]);
      await Promise.all(
        Object.values(files).map((path) => writeFile(path, "fixture")),
      );

      await execFileAsync("sh", [PRUNE_SCRIPT, root, "amd64"]);

      await expect(exists(files.cpu)).resolves.toBe(true);
      await expect(exists(files.shared)).resolves.toBe(true);
      await expect(exists(files.cuda)).resolves.toBe(false);
      await expect(exists(files.tensorrt)).resolves.toBe(false);
      await expect(exists(files.metadata)).resolves.toBe(false);
      await expect(exists(files.installer)).resolves.toBe(false);
    } finally {
      await rm(root, { recursive: true, force: true });
    }
  });
});
