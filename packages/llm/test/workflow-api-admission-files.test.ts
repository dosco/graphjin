import {
  mkdir,
  mkdtemp,
  rm,
  symlink,
  writeFile,
} from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { getOrgAgentRoot } from "../src/work/workspace";
import {
  readWorkflowApiBatchInput,
  resolveWorkflowApiWorkspacePath,
} from "../src/workflows/api-admission";

function restoreEnv(name: string, value: string | undefined): void {
  if (value === undefined) delete process.env[name];
  else process.env[name] = value;
}

describe("workflow API retained files", () => {
  it("rejects lexical and symlink escapes from the organization workspace", async () => {
    expect(() =>
      resolveWorkflowApiWorkspacePath("org-file-test", "../../outside.ndjson"),
    ).toThrow(/escaped the organization workspace/i);

    const previous = {
      hostWebDev: process.env.OPENNEKO_HOST_WEB_DEV,
      nodeEnv: process.env.NODE_ENV,
      agentHome: process.env.OPENNEKO_AGENT_HOME,
      stackMode: process.env.OPENNEKO_STACK_MODE,
      publicDemo: process.env.NEXT_PUBLIC_DEMO,
      demo: process.env.DEMO,
    };
    const temporaryHome = await mkdtemp(
      join(tmpdir(), "openneko-workflow-api-files-"),
    );
    try {
      process.env.OPENNEKO_HOST_WEB_DEV = "1";
      process.env.NODE_ENV = "development";
      process.env.OPENNEKO_AGENT_HOME = temporaryHome;
      delete process.env.OPENNEKO_STACK_MODE;
      delete process.env.NEXT_PUBLIC_DEMO;
      delete process.env.DEMO;

      const orgRoot = getOrgAgentRoot("org-file-test");
      const runRoot = join(orgRoot, "runs", "run-file-test");
      const outside = join(temporaryHome, "outside.ndjson");
      await mkdir(runRoot, { recursive: true });
      await writeFile(outside, '{"secret":"outside"}\n', "utf8");
      await symlink(outside, join(runRoot, "api-batch-input.ndjson"));

      await expect(
        readWorkflowApiBatchInput({
          orgId: "org-file-test",
          relativePath: "runs/run-file-test/api-batch-input.ndjson",
          maxBytes: 1_024,
        }),
      ).rejects.toMatchObject({
        code: "batch_input_unavailable",
        status: 410,
      });
    } finally {
      restoreEnv("OPENNEKO_HOST_WEB_DEV", previous.hostWebDev);
      restoreEnv("NODE_ENV", previous.nodeEnv);
      restoreEnv("OPENNEKO_AGENT_HOME", previous.agentHome);
      restoreEnv("OPENNEKO_STACK_MODE", previous.stackMode);
      restoreEnv("NEXT_PUBLIC_DEMO", previous.publicDemo);
      restoreEnv("DEMO", previous.demo);
      await rm(temporaryHome, { recursive: true, force: true });
    }
  });
});
