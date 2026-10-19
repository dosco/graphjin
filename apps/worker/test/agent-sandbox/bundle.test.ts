import { execFileSync } from "node:child_process";
import { cp, mkdtemp, mkdir, realpath, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { fileURLToPath } from "node:url";
import { build } from "esbuild";
import { expect, it } from "vitest";

it("boots both standalone bundles with assets and no host or embedding dependencies", async () => {
  const root = fileURLToPath(new URL("../../", import.meta.url));
  const out = await realpath(await mkdtemp(join(tmpdir(), "agent-bundle-")));
  try {
    const result = await build({
      absWorkingDir: root,
      entryPoints: ["src/agent-sandbox/entry.ts", "src/agent-sandbox/mcp-bridge.ts"],
      outdir: out,
      bundle: true,
      platform: "node",
      format: "esm",
      metafile: true,
      banner: { js: "import { createRequire } from 'node:module'; const require = createRequire(import.meta.url);" },
    });
    expect(Object.keys(result.metafile!.inputs).filter((name) =>
      /packages\/(db|records|telemetry)\/|embedding\.ts|onnxruntime|huggingface/.test(name),
    )).toEqual([]);
    await writeFile(join(out, "package.json"), '{"type":"module"}');
    await mkdir(join(out, "assets"));
    await cp(join(root, "../../packages/llm/assets/builtin-skills"), join(out, "assets/builtin-skills"), { recursive: true });
    // Deliberately omit inherited package/asset overrides and credentials.
    const options = { cwd: out, env: { PATH: process.env.PATH }, encoding: "utf8" as const };
    const preflight = JSON.parse(execFileSync(process.execPath, ["entry.js", "--preflight"], options));
    expect(preflight).toMatchObject({ status: "ok",
      skillsRoot: join(out, "assets/builtin-skills"), mcpBridgePath: join(out, "mcp-bridge.js") });
    execFileSync(process.execPath, ["--input-type=module", "-e", "await import('./mcp-bridge.js')"], options);
    await rm(join(out, "assets"), { recursive: true });
    expect(() => execFileSync(process.execPath, ["entry.js", "--preflight"], { ...options, stdio: "pipe" })).toThrow();
  } finally {
    await rm(out, { recursive: true, force: true });
  }
});
