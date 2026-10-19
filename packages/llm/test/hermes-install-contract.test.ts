import { readFile, readdir } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const REPO_ROOT = fileURLToPath(new URL("../../../", import.meta.url));
const HERMES_REF = "29112bef099274229cadff79cdff7bf7b99c4b77";

describe("Hermes install contract", () => {
  it("keeps container and local development on the same exact upstream ref", async () => {
    const [dockerfile, installer] = await Promise.all([
      readFile(`${REPO_ROOT}Dockerfile`, "utf8"),
      readFile(`${REPO_ROOT}scripts/install-clis.sh`, "utf8"),
    ]);

    expect(dockerfile).toContain(`ARG HERMES_AGENT_REF=${HERMES_REF}`);
    expect(installer).toContain(`HERMES_AGENT_REF="\${HERMES_AGENT_REF:-${HERMES_REF}}"`);
    expect(dockerfile).toContain(
      'git -C /usr/local/lib/hermes-agent fetch --depth 1 origin "$HERMES_AGENT_REF"',
    );
    expect(dockerfile).toContain(
      "UV_PROJECT_ENVIRONMENT=/usr/local/uv/tools/hermes-agent",
    );
    expect(dockerfile).toContain(
      "uv sync --locked --no-dev --extra acp --extra mcp --extra anthropic",
    );
    expect(installer).toContain('uv venv --clear "$hermes_tool_root" --python 3.11');
    expect(installer).toContain(
      'uv sync --project "$hermes_source_root" --locked --no-dev --extra acp --extra mcp --extra anthropic',
    );
    expect(dockerfile).not.toContain("uv tool install");
    expect(installer).not.toContain("uv tool install");
    expect(dockerfile).toContain("hermes_cli.__version__ == '0.21.0'");
    expect(installer).toContain("HERMES_AGENT_VERSION=\"0.21.0\"");
    expect(dockerfile).toContain("Hermes OpenAI provider SDK missing");
    expect(installer).toContain("Hermes OpenAI provider SDK missing");
    expect(dockerfile).toContain("Hermes Anthropic provider SDK missing");
    expect(installer).toContain("Hermes Anthropic provider SDK missing");
  });

  it("keeps the editable Hermes runtime without shipping its development repository", async () => {
    const dockerfile = await readFile(`${REPO_ROOT}Dockerfile`, "utf8");
    const installStart = dockerfile.indexOf(
      "RUN --mount=type=cache,id=hermes-uv,target=/tmp/uv-cache",
    );
    const installEnd = dockerfile.indexOf("# ─── 2c. document toolchain");
    const installBlock = dockerfile.slice(installStart, installEnd);

    expect(installStart).toBeGreaterThan(-1);
    expect(installEnd).toBeGreaterThan(installStart);
    expect(installBlock).toContain(
      'git -C /usr/local/lib/hermes-agent fetch --depth 1 origin "$HERMES_AGENT_REF"',
    );
    expect(installBlock).toContain(
      "uv sync --locked --no-dev --extra acp --extra mcp --extra anthropic",
    );
    expect(installBlock).toContain("/usr/local/lib/hermes-agent/.git");
    expect(installBlock).toContain("/usr/local/lib/hermes-agent/tests");
    expect(installBlock).toContain("/usr/local/lib/hermes-agent/apps");
    expect(installBlock).toContain("/usr/local/lib/hermes-agent/website");
    expect(installBlock).toContain("/usr/local/lib/hermes-agent/optional-skills");
    expect(installBlock).toContain("PYTHONDONTWRITEBYTECODE=1 hermes --version");
    expect(installBlock.match(/\nRUN /g) ?? []).toHaveLength(0);
  });

  it("patches ACP to pass the configured reasoning effort into AIAgent", async () => {
    const [
      dockerfile,
      installer,
      patch,
      interimPatch,
      anthropicPatch,
      delegationPatch,
    ] = await Promise.all([
      readFile(`${REPO_ROOT}Dockerfile`, "utf8"),
      readFile(`${REPO_ROOT}scripts/install-clis.sh`, "utf8"),
      readFile(
        `${REPO_ROOT}scripts/patches/hermes-acp-reasoning-config.patch`,
        "utf8",
      ),
      readFile(`${REPO_ROOT}scripts/patches/hermes-acp-interim-messages.patch`, "utf8"),
      readFile(`${REPO_ROOT}scripts/patches/hermes-acp-anthropic-reasoning.patch`, "utf8"),
      readFile(
        `${REPO_ROOT}scripts/patches/hermes-acp-native-delegation-policy.patch`,
        "utf8",
      ),
    ]);

    expect(patch).toContain("resolve_reasoning_config");
    expect(patch).toContain('"reasoning_config": resolve_reasoning_config');
    expect(interimPatch).toContain("make_interim_message_cb");
    expect(interimPatch).toContain("interim_assistant_callback");
    expect(interimPatch).toContain("pending_streamed_message.append(text)");
    expect(interimPatch).toContain("raw_interim_cb(text, already_streamed=False)");
    expect(interimPatch).toContain(
      '-            and (not streamed_message or result.get("response_transformed"))',
    );
    expect(anthropicPatch).toContain("_emit_unstreamed_anthropic_reasoning");
    expect(anthropicPatch).toContain("reasoning_was_streamed");
    expect(delegationPatch).toContain("OPENNEKO_HERMES_NATIVE_DELEGATION");
    expect(delegationPatch).toContain('["delegation"]');
    expect(dockerfile).toContain("hermes-acp-reasoning-config.patch");
    expect(dockerfile).toContain("hermes-acp-interim-messages.patch");
    expect(dockerfile).toContain("hermes-acp-anthropic-reasoning.patch");
    expect(dockerfile).toContain("hermes-acp-native-delegation-policy.patch");
    expect(installer).toContain("hermes-acp-reasoning-config.patch");
    expect(installer).toContain("hermes-acp-interim-messages.patch");
    expect(installer).toContain("hermes-acp-anthropic-reasoning.patch");
    expect(installer).toContain("hermes-acp-native-delegation-policy.patch");
    expect(dockerfile).toContain("Hermes ACP must pass configured reasoning into AIAgent");
    expect(installer).toContain("Hermes ACP must pass configured reasoning into AIAgent");
    expect(dockerfile).toContain("assert 'usage=usage' in source");
    expect(installer).toContain("assert 'usage=usage' in source");
    expect(dockerfile).toContain("Hermes ACP Anthropic reasoning fallback missing");
    expect(installer).toContain("Hermes ACP Anthropic reasoning fallback missing");
    expect(dockerfile).toContain("Hermes ACP native delegation policy missing");
    expect(installer).toContain("Hermes ACP native delegation policy missing");
    for (const source of [dockerfile, installer]) {
      expect(source).toContain("hermes-acp-run-budget.patch");
      expect(source).toContain("Hermes ACP run budget missing");
    }
    // The reasoning-config assertion inspects _make_agent, and the installer
    // greps this line; the run-budget patch must add overrides after it.
    const runBudgetPatch = await readFile(
      `${REPO_ROOT}scripts/patches/hermes-acp-run-budget.patch`,
      "utf8",
    );
    expect(runBudgetPatch).not.toMatch(/^-\s+"reasoning_config": resolve_reasoning_config/m);
    expect(runBudgetPatch).toMatch(/^\+\s+\*\*_openneko_run_budget\(\),$/m);
  });

  it("patches ACP to pass per-turn cost and prices Gemini 3.7 and 3.8 Flash and Claude Sonnet 5.5", async () => {
    const [dockerfile, installer, costPatch] = await Promise.all([
      readFile(`${REPO_ROOT}Dockerfile`, "utf8"),
      readFile(`${REPO_ROOT}scripts/install-clis.sh`, "utf8"),
      readFile(`${REPO_ROOT}scripts/patches/hermes-acp-cost.patch`, "utf8"),
    ]);

    expect(costPatch).toContain('field_meta = {"openneko": {"usage": cost_usage()}}');
    expect(costPatch).toContain("session_cost_unknown_calls");
    expect(costPatch).toContain('"gemini-3.7-flash", "gemini-3.8-flash"');
    expect(costPatch).toContain("datetime(2027, 1, 1, tzinfo=timezone.utc)");
    expect(costPatch).toContain('("anthropic", "claude-sonnet-5-5")');
    for (const source of [dockerfile, installer]) {
      expect(source.indexOf("hermes-acp-tool-usage.patch")).toBeGreaterThan(-1);
      expect(source.indexOf("hermes-acp-cost.patch")).toBeGreaterThan(
        source.indexOf("hermes-acp-tool-usage.patch"),
      );
      expect(source).toContain("test-hermes-acp-cost.py");
    }
  });

  it("disables network-backed lazy installs in the runtime", async () => {
    const runtimeContract = await readFile(
      `${REPO_ROOT}apps/worker/src/agent-sandbox/runtime-contract.ts`,
      "utf8",
    );

    expect(runtimeContract).toContain('env.HERMES_DISABLE_LAZY_INSTALLS = "1"');
  });

  it("keeps Hermes out of the worker while preserving its required runtime contracts", async () => {
    const dockerfile = await readFile(`${REPO_ROOT}Dockerfile`, "utf8");
    const sharedRuntime = dockerfile.slice(
      dockerfile.indexOf("FROM npm-runtime AS graphjin-node-runtime"),
      dockerfile.indexOf("FROM graphjin-node-runtime AS agent-base"),
    );
    const workerDeployStage = dockerfile.slice(
      dockerfile.indexOf("FROM source AS worker-deploy"),
      dockerfile.indexOf("FROM graphjin-node-runtime AS worker"),
    );
    const workerStage = dockerfile.slice(
      dockerfile.indexOf("FROM graphjin-node-runtime AS worker"),
      dockerfile.indexOf("FROM source AS agent-deploy"),
    );

    expect(sharedRuntime).toContain("COPY --from=go-build /out/openneko /usr/local/bin/openneko");
    expect(sharedRuntime).toContain("ln -s openneko /usr/local/bin/graphjin");
    expect(workerDeployStage).toContain("ONNXRUNTIME_NODE_INSTALL=skip");
    expect(workerStage).toContain(
      "ln -s /usr/bin/python3 /usr/local/uv/tools/hermes-agent/bin/python",
    );
    expect(workerStage).toContain("COPY --from=openshell-bin");
    expect(workerStage).not.toContain("uv tool install");
    expect(workerStage).not.toContain("COPY --from=agent-base");
    expect(workerStage).not.toContain("COPY --from=npm-payload");
  });

  it("ships standalone sandbox bundles with a build-time filesystem check", async () => {
    const dockerfile = await readFile(`${REPO_ROOT}Dockerfile`, "utf8");
    const agentDeploy = dockerfile.slice(
      dockerfile.indexOf("FROM source AS agent-deploy"),
      dockerfile.indexOf("FROM cli AS agent"),
    );
    expect(agentDeploy).toContain("src/agent-sandbox/entry.ts src/agent-sandbox/mcp-bridge.ts");
    expect(agentDeploy).toContain("assets/builtin-skills");
    expect(agentDeploy).not.toContain("deploy --prod");
    expect(dockerfile).toContain("node entry.js --preflight");
    expect(dockerfile).toContain("await import('./mcp-bridge.js')");
    expect(dockerfile).toContain("FROM npm-runtime AS agent-base");
  });

  it("has no production GraphJin CLI execution fallback", async () => {
    const roots = [
      `${REPO_ROOT}packages/llm/src`,
      `${REPO_ROOT}apps/worker/src/agent-sandbox`,
    ];
    const files = (
      await Promise.all(
        roots.map(async (root) =>
          (await readdir(root, { recursive: true }))
            .filter((file) => file.endsWith(".ts"))
            .map((file) => `${root}/${file}`),
        ),
      )
    ).flat();
    const source = (await Promise.all(files.map((file) => readFile(file, "utf8")))).join(
      "\n",
    );

    expect(source).not.toContain("graphjin cli execute_graphql");
    expect(source).not.toContain("OPENNEKO_GRAPHJIN_BIN");
    expect(source).not.toContain("OPENNEKO_GRAPHJIN_WRITE_GRANTS");
  });
});
