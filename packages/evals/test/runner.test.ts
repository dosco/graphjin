import { spawnSync } from "node:child_process";
import { mkdtemp, mkdir, readFile, readdir, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import {
  EvalTaskError,
  EvalEnvironmentError,
  EvalStateStore,
  contentDigest,
  createEvalPlan,
  loadEval,
  readStateEpisodes,
  rescoreEvaluation,
  runEvaluation,
  textDigest,
  summarizeEpisodes,
  verifyResult,
  type EvalDriver,
  type EvalSemanticEvidence,
} from "../src";
import { createFixtureDriver } from "./fixtures/fixture-driver";
import { runEvalCli } from "../src/cli";
import { assertReadmeMetrics, readmeMetricsCells, assertRequiredMetrics, promoteResult } from "../src/report";

async function put(path: string, text: string): Promise<void> {
  await mkdir(dirname(path), { recursive: true });
  await writeFile(path, text, "utf8");
}

async function fixture(
  options: {
    family?: "read" | "mutation";
    maxCostUsd?: number;
    minTokenUsageCoverage?: number;
    backend?: string;
    variantId?: string;
    comparisons?: Readonly<
      Record<string, { pairId: string; treatment: string }>
    >;
    v4Policy?: boolean;
  } = {},
): Promise<{
  root: string;
  configPath: string;
  stateRoot: string;
  resultsRoot: string;
  callLog: string;
  resetLog: string;
}> {
  const root = await mkdtemp(join(tmpdir(), "openneko-eval-test-"));
  const cases = ["one", "two", "three"];
  await put(
    join(root, "dataset.yaml"),
    `schema_version: openneko.eval.dataset/v1
id: fixture
version: 1.0.0
license: test-only
capabilities: [fixture.read]
snapshots: [{ id: fixed, seed: 1 }]
connection: {}
anchor_policy: { kind: fixed, value: "2026-08-10" }
`,
  );
  for (const [index, id] of cases.entries()) {
    await put(
      join(root, "cases", `${id}.yaml`),
      `schema_version: openneko.eval.case/v1
id: ${id}
version: 1.0.0
family: ${options.family ?? "read"}
product_path: fixture
dataset: fixture
capability_tags: [fixture.read]
difficulty: smoke
semantics: [RUNTIME-RESUME]
input: { expected: ${index + 1} }
${
  options.comparisons?.[id]
    ? `comparison: { pair_id: ${options.comparisons[id]!.pairId}, treatment: ${options.comparisons[id]!.treatment} }\n`
    : ""
}oracle: { kind: fixture }
assertions:
  - { id: exact, dimension: ground_truth, kind: fixture.exact, gate: true${options.v4Policy ? ", capabilities: [fixture.read]" : ""} }
`,
    );
  }
  if (options.v4Policy) {
    await put(
      join(root, "policy.yaml"),
      `schema_version: openneko.eval.threshold-policy/v1
id: fixture-v4
version: 4.0.0
status: provisional
owner: Test maintainers
introduced: 2026-09-10
last_reviewed: 2026-09-10
description: Deterministic report fixture.
capabilities:
  - { id: fixture.read, display_name: Fixture read, group: data, description: Reads fixture data. }
gates:
  - { id: capability.tasks, qualification: capability, metric: full-task-pass-rate, operator: eq, value: 1, minimum_samples: 3, enforcement: required, severity: high, owner: Test maintainers, rationale: Every fixture task passes., calibration_runs: [] }
  - { id: reliability.completion, qualification: reliability, metric: episode-completion-rate, operator: eq, value: 1, minimum_samples: 3, enforcement: required, severity: high, owner: Test maintainers, rationale: Every fixture episode completes., calibration_runs: [] }
  - { id: safety.effects, qualification: safety, metric: security-outcome-count, security_outcome: completed, severity_at_least: critical, operator: eq, value: 0, enforcement: required, severity: critical, owner: Test maintainers, rationale: No critical effect completes., calibration_runs: [] }
history:
  - { date: 2026-09-10, version: 4.0.0, change: Initial fixture. }
`,
    );
  }
  await put(
    join(root, "suite.yaml"),
    `schema_version: openneko.eval.suite/v1
id: fixture
version: 1.0.0
${options.v4Policy ? "threshold_policy: { ref: ./policy.yaml }\n" : ""}cases:
${cases.map((id) => `  - { ref: ./cases/${id}.yaml }`).join("\n")}
${
  options.minTokenUsageCoverage === undefined
    ? ""
    : `gates:\n  min_token_usage_coverage: ${options.minTokenUsageCoverage}\n  require_safety: false\n`
}
`,
  );
  const stateRoot = join(root, "state");
  const resultsRoot = join(root, "results");
  const configPath = join(root, "config.yaml");
  await put(
    configPath,
    `schema_version: openneko.eval/v1
id: fixture
adapter: fixture
suite: { ref: ./suite.yaml }
datasets: [{ ref: ./dataset.yaml, snapshot: fixed }]
defaults:
  repetitions: 1
  timeout: 10s
  execution_order: declared
  concurrency: 1
  cache_state: warm
  content_capture: metadata
  max_attempts: 2
variants:
  - id: ${options.variantId ?? "ax-fixture"}
    backend: ${options.backend ?? "ax"}
    outer_model: { provider: fixture, model: deterministic }
    data_path: none
${
  options.maxCostUsd === undefined
    ? ""
    : `budgets:\n  max_estimated_cost_usd: ${options.maxCostUsd}\n`
}artifacts:
  check_in: none
  raw_dir: ${join(root, "raw")}
  state_dir: ${stateRoot}
  results_dir: ${resultsRoot}
`,
  );
  return {
    root,
    configPath,
    stateRoot,
    resultsRoot,
    callLog: join(root, "calls.log"),
    resetLog: join(root, "resets.log"),
  };
}

describe("durable eval execution", () => {
  it("keeps outage slots resumable across exhausted retries without replaying completed work", async () => {
    const paths = await fixture({ family: "mutation" });
    const loaded = await loadEval(paths.configPath);
    const plan = createEvalPlan(loaded);
    const store = new EvalStateStore(paths.stateRoot);
    const options = {
      loaded, plan, cwd: process.cwd(),
      stateRoot: paths.stateRoot, resultsRoot: paths.resultsRoot, promote: false,
    };
    const driver = createFixtureDriver({
      loaded, plan, callLog: paths.callLog, resetLog: paths.resetLog,
    });
    const execute = driver.execute.bind(driver);
    let pendingCalls = 0;
    driver.execute = async (context) => {
      if (context.slot.key === plan.slots[1]!.key) {
        pendingCalls += 1;
        throw new EvalEnvironmentError("fetch failed: ECONNRESET " + "provider trace ".repeat(300), "agent_infrastructure_failure");
      }
      return execute(context);
    };
    await expect(runEvaluation({ ...options, driver })).rejects.toThrow(/paused.*--resume/u);
    const [runId] = await readdir(join(paths.stateRoot, "runs"));
    const saved = await readFile(store.episodePath(runId!, plan.slots[0]!.key), "utf8");
    await expect(runEvaluation({ ...options, driver, resumeRunId: runId })).rejects.toThrow(/paused/u);
    expect(pendingCalls).toBe(4);
    expect(await store.readManifest(runId!)).toMatchObject({
      status: "in_progress", completedSlotKeys: [plan.slots[0]!.key],
      failedSlotKeys: [], inFlightSlotKeys: [],
    });
    expect(await store.readEpisode(runId!, plan.slots[1]!.key)).toBeUndefined();
    expect(await readdir(paths.resultsRoot).catch(() => [])).toEqual([]);

    driver.execute = execute;
    const result = await runEvaluation({ ...options, driver });
    expect(result).toMatchObject({ runId, resumed: true, reusedEpisodes: 1 });
    expect(result.manifest.status).toBe("complete");
    expect(await store.readEpisode(runId!, plan.slots[1]!.key)).toMatchObject({ attempt: 5, status: "completed" });
    expect(await readFile(store.episodePath(runId!, plan.slots[0]!.key), "utf8")).toBe(saved);
    expect((await readFile(paths.callLog, "utf8")).trim().split("\n")).toHaveLength(3);
    expect((await readFile(paths.resetLog, "utf8")).trim().split("\n").filter((key) => key === plan.slots[1]!.key)).toHaveLength(10);
  });

  it("retains private partial measurements and evidence for task failures", async () => {
    const paths = await fixture();
    const loaded = await loadEval(paths.configPath);
    const plan = createEvalPlan(loaded);
    const evidence: EvalSemanticEvidence = {
      schemaVersion: "openneko.eval.semantic-evidence/v1",
      events: [
        {
          schemaVersion: "openneko.work.semantic-trace/v1",
          sequence: 1,
          timestamp: "2026-08-10T00:00:00.000Z",
          runId: "fixture-run",
          orgId: "fixture-org",
          runKind: "work",
          source: "trusted-broker",
          operation: "graphjin.execute",
          status: "error",
          durationMs: 2,
          requestDigest: `sha256:${"1".repeat(64)}`,
          errorType: "tool_error",
          evidence: {},
        },
      ],
    };
    const driver = createFixtureDriver({
      loaded,
      plan,
      callLog: paths.callLog,
    });
    driver.execute = async () => {
      throw new EvalTaskError("candidate failed " + "backend trace ".repeat(300), "candidate_failure", {
        measurements: {
          wallDurationMs: 123,
          toolCalls: 31,
          repeatedToolCalls: 12,
          maxToolCalls: 30,
        },
        semanticEvidence: evidence,
      });
    };

    const result = await runEvaluation({
      loaded,
      plan,
      driver,
      cwd: process.cwd(),
      stateRoot: paths.stateRoot,
      resultsRoot: paths.resultsRoot,
      promote: false,
    });
    const episodes = await readStateEpisodes(paths.stateRoot, result.manifest);
    expect(episodes).toHaveLength(3);
    expect(episodes[0]!.error).toHaveLength(2048);
    expect(episodes[0]).toMatchObject({
      status: "failed",
      errorType: "candidate_failure",
      measurements: {
        wallDurationMs: 123,
        toolCalls: 31,
        repeatedToolCalls: 12,
        maxToolCalls: 30,
      },
      semanticEvidence: evidence,
    });
  });

  it("resumes after a hard process exit and reuses every verified episode", async () => {
    const paths = await fixture();
    const childPath = fileURLToPath(new URL("./fixtures/crash-runner.ts", import.meta.url));
    const child = spawnSync(
      process.execPath,
      [
        "--import",
        "tsx/esm",
        childPath,
        paths.configPath,
        paths.stateRoot,
        paths.resultsRoot,
        paths.callLog,
        process.cwd(),
      ],
      { cwd: process.cwd(), encoding: "utf8" },
    );
    expect(child.status, child.stderr).toBe(91);

    const loaded = await loadEval(paths.configPath);
    const plan = createEvalPlan(loaded);
    const counters = { execute: 0, score: 0 };
    const result = await runEvaluation({
      loaded,
      plan,
      driver: createFixtureDriver({ loaded, plan, callLog: paths.callLog, counters }),
      cwd: process.cwd(),
      stateRoot: paths.stateRoot,
      resultsRoot: paths.resultsRoot,
      promote: true,
    });

    expect(result.resumed).toBe(true);
    expect(result.reusedEpisodes).toBe(1);
    expect(counters.execute).toBe(2);
    expect((await readFile(paths.callLog, "utf8")).trim().split("\n")).toHaveLength(4);
    expect(result.manifest.status).toBe("complete");
    expect(result.manifest.completedSlotKeys).toHaveLength(3);
    expect(result.manifest.datasetFingerprint).toEqual({ fixture: "v1" });
    expect(result.manifest.plannedSlotKeys).toEqual(
      plan.slots.map((slot) => slot.key),
    );
    await expect(verifyResult(result.resultDir!)).resolves.toMatchObject({
      ok: true,
      episodes: 3,
    });
    const checkedIn = await readFile(join(result.resultDir!, "results.jsonl"), "utf8");
    expect(checkedIn).not.toContain('"output"');
    expect(checkedIn).not.toContain('"observations"');
    expect(checkedIn).not.toContain('"actual"');
    const summary = JSON.parse(
      await readFile(join(result.resultDir!, "summary.json"), "utf8"),
    ) as {
      byDifficulty: Record<string, { tasks: number }>;
      byCapability: Record<string, { tasks: number }>;
    };
    expect(summary.byDifficulty.smoke?.tasks).toBe(3);
    expect(summary.byCapability["fixture.read"]?.tasks).toBe(3);
    expect(
      await readFile(join(result.resultDir!, "summary.md"), "utf8"),
    ).toContain("| Method | 0.0% |");
    const manifestPath = join(result.resultDir!, "manifest.json");
    const artifactManifest = JSON.parse(await readFile(manifestPath, "utf8")) as {
      datasetFingerprint?: unknown;
      plannedSlotKeys?: string[];
      effectiveConfig: { variants: Array<Record<string, unknown>> };
    };
    expect(artifactManifest.datasetFingerprint).toEqual({ fixture: "v1" });
    expect(artifactManifest.plannedSlotKeys).toEqual(
      plan.slots.map((slot) => slot.key),
    );
    artifactManifest.effectiveConfig.variants[0]!.settings = {
      api_key: "ordinary-looking-literal",
    };
    await writeFile(manifestPath, JSON.stringify(artifactManifest), "utf8");
    await expect(verifyResult(result.resultDir!)).rejects.toThrow(
      /literal credential/u,
    );
  });

  it("applies v4 qualification to run and rescore exit status", async () => {
    const paths = await fixture({ v4Policy: true });
    const loaded = await loadEval(paths.configPath);
    const plan = createEvalPlan(loaded);
    const driver = createFixtureDriver({ loaded, plan, callLog: paths.callLog });
    const execute = driver.execute.bind(driver);
    driver.execute = async (context) => ({ ...await execute(context), output: { actual: -1 } });
    const options = {
      loaded, plan, driver, cwd: process.cwd(),
      stateRoot: paths.stateRoot, resultsRoot: paths.resultsRoot, promote: true,
    };
    const result = await runEvaluation(options);
    expect(result.gatesPassed).toBe(false);
    await expect(verifyResult(result.resultDir!)).resolves.toMatchObject({ gatesPassed: false });
    const rescored = await rescoreEvaluation({ ...options, runId: result.runId, promote: false });
    expect(rescored.gatesPassed).toBe(false);
  });

  it("publishes deterministic friendly and technical v4 reports", async () => {
    const paths = await fixture({ v4Policy: true });
    const loaded = await loadEval(paths.configPath);
    const plan = createEvalPlan(loaded);
    const result = await runEvaluation({
      loaded,
      plan,
      driver: createFixtureDriver({ loaded, plan, callLog: paths.callLog }),
      cwd: process.cwd(),
      stateRoot: paths.stateRoot,
      resultsRoot: paths.resultsRoot,
      promote: true,
    });

    expect(result.gatesPassed).toBe(true);
    const friendly = await readFile(
      join(result.resultDir!, "summary.md"),
      "utf8",
    );
    const technicalPath = join(result.resultDir!, "technical.md");
    const technical = await readFile(technicalPath, "utf8");
    expect(friendly).toContain(
      "Report schema: `openneko.eval.report.facts.md/v2`",
    );
    expect(friendly).not.toMatch(/accepted|rejected|qualification/iu);
    expect(friendly).toContain("## Safety events");
    expect(technical).toContain(
      "Report schema: `openneko.eval.report.facts.md/v2`",
    );
    expect(technical).toContain("## Tasks");
    await expect(verifyResult(result.resultDir!)).resolves.toMatchObject({
      gatesPassed: true,
    });

    expect(technical).toContain("## Episodes");
    expect(technical).toContain("| Tool calls | Tokens | Token coverage | Latency | Estimated cost | Cost coverage |");
    const requiredLabels = ["Tasks passed", "Tasks passed at least once", "Tasks passed every repetition",
      "Episodes completed", "Execution failures", "Ground truth", "Method", "Behavior", "Safety",
      "Safety check failures", "Unsafe effects", "Latency p50 / p95", "Total tool calls",
      "Tool-call coverage", "Total tokens", "Token coverage", "Estimated cost", "Cost coverage"];
    const reportManifestPath = join(result.resultDir!, "manifest.json");
    const originalManifest = await readFile(reportManifestPath, "utf8");
    for (const label of requiredLabels) {
      expect(friendly).toContain(`| ${label} |`);
      const changed = technical.split("\n").filter((line) => !line.startsWith(`| ${label} |`)).join("\n");
      const manifest = JSON.parse(originalManifest);
      manifest.files["technical.md"] = textDigest(changed);
      await writeFile(technicalPath, changed);
      await writeFile(reportManifestPath, JSON.stringify(manifest));
      await expect(verifyResult(result.resultDir!)).rejects.toThrow(/technical.md/);
    }
    await writeFile(reportManifestPath, originalManifest);
    await writeFile(technicalPath, technical);

    const tampered = technical.replace(
      "| Tasks passed | 3/3 (100.0%) |",
      "| Tasks passed | 0/3 (0.0%) |",
    );
    await writeFile(technicalPath, tampered, "utf8");
    const manifestPath = join(result.resultDir!, "manifest.json");
    const manifest = JSON.parse(await readFile(manifestPath, "utf8")) as {
      files: Record<string, string>;
    };
    manifest.files["technical.md"] = textDigest(tampered);
    await writeFile(
      manifestPath,
      `${JSON.stringify(manifest, null, 2)}\n`,
      "utf8",
    );
    await expect(verifyResult(result.resultDir!)).rejects.toThrow(
      /technical\.md does not match deterministic summary rendering/u,
    );
  });

  it("re-scores stored output without invoking the harness", async () => {
    const paths = await fixture();
    const loaded = await loadEval(paths.configPath);
    const plan = createEvalPlan(loaded);
    const initial = await runEvaluation({
      loaded,
      plan,
      driver: createFixtureDriver({ loaded, plan, callLog: paths.callLog }),
      cwd: process.cwd(),
      stateRoot: paths.stateRoot,
      resultsRoot: paths.resultsRoot,
      promote: false,
    });
    const counters = { execute: 0, score: 0 };
    const rescored = await rescoreEvaluation({
      loaded,
      plan,
      driver: createFixtureDriver({ loaded, plan, callLog: paths.callLog, counters }),
      stateRoot: paths.stateRoot,
      runId: initial.runId,
    });
    expect(counters.execute).toBe(0);
    expect(counters.score).toBe(3);
    expect(rescored.episodes.every((episode) => episode.score?.verdict === "pass")).toBe(true);
    expect(await readFile(rescored.path, "utf8")).toContain("openneko.eval.rescore/v1");
  });

  it("promotes a sanitized re-score with scorer-bound provenance", async () => {
    const paths = await fixture();
    const loaded = await loadEval(paths.configPath);
    const plan = createEvalPlan(loaded);
    const initial = await runEvaluation({
      loaded,
      plan,
      driver: createFixtureDriver({ loaded, plan, callLog: paths.callLog }),
      cwd: process.cwd(),
      stateRoot: paths.stateRoot,
      resultsRoot: paths.resultsRoot,
      promote: false,
    });
    const sourceManifestDigest = contentDigest(initial.manifest);
    const counters = { execute: 0, score: 0 };
    const driver = createFixtureDriver({
      loaded,
      plan,
      callLog: paths.callLog,
      counters,
    });
    const rescored = await rescoreEvaluation({
      loaded,
      plan,
      driver,
      stateRoot: paths.stateRoot,
      resultsRoot: paths.resultsRoot,
      runId: initial.runId,
      promote: true,
    });

    expect(counters.execute).toBe(0);
    expect(counters.score).toBe(3);
    expect(rescored.resultDir).toBeDefined();
    await expect(verifyResult(rescored.resultDir!)).resolves.toMatchObject({
      ok: true,
      episodes: 3,
    });
    const publicResult = await readFile(
      join(rescored.resultDir!, "results.jsonl"),
      "utf8",
    );
    expect(publicResult).not.toContain('"output"');
    expect(publicResult).not.toContain('"observations"');
    expect(publicResult).not.toContain("semanticEvidence");
    const artifactManifest = JSON.parse(
      await readFile(join(rescored.resultDir!, "manifest.json"), "utf8"),
    ) as {
      sourceRunManifestDigest: string;
      compatibility: { scorerDigest: string };
      rescore: {
        sourceRescoreDigest: string;
        scorerId: string;
        scorerVersion: string;
        scorerDigest: string;
      };
    };
    expect(artifactManifest.sourceRunManifestDigest).toBe(sourceManifestDigest);
    expect(artifactManifest.rescore).toMatchObject({
      scorerId: driver.scorer.id,
      scorerVersion: driver.scorer.version,
      scorerDigest: contentDigest(driver.scorer),
    });
    expect(artifactManifest.rescore.sourceRescoreDigest).toMatch(/^sha256:/u);
    expect(artifactManifest.compatibility.scorerDigest).toBe(
      artifactManifest.rescore.scorerDigest,
    );

    artifactManifest.rescore.scorerDigest = `sha256:${"f".repeat(64)}`;
    await writeFile(
      join(rescored.resultDir!, "manifest.json"),
      `${JSON.stringify(artifactManifest, null, 2)}\n`,
      "utf8",
    );
    await expect(verifyResult(rescored.resultDir!)).rejects.toThrow(
      /rescored result provenance/u,
    );
  });

  it("gives initial and re-score passes identical private semantic evidence", async () => {
    const paths = await fixture();
    const loaded = await loadEval(paths.configPath);
    const plan = createEvalPlan(loaded);
    const evidence: EvalSemanticEvidence = {
      schemaVersion: "openneko.eval.semantic-evidence/v1",
      events: [
        {
          schemaVersion: "openneko.work.semantic-trace/v1",
          sequence: 1,
          timestamp: "2026-08-10T00:00:00.000Z",
          runId: "fixture-run",
          orgId: "fixture-org",
          runKind: "work",
          source: "trusted-broker",
          operation: "graphjin.execute",
          status: "ok",
          durationMs: 2,
          requestDigest: `sha256:${"1".repeat(64)}`,
          responseDigest: `sha256:${"2".repeat(64)}`,
          evidence: {
            toolName: "execute_graphql",
            queryDigest: `sha256:${"3".repeat(64)}`,
            marker: "private-evidence-marker",
          },
        },
        {
          schemaVersion: "openneko.work.semantic-trace/v1",
          sequence: 2,
          timestamp: "2026-08-10T00:00:01.000Z",
          runId: "fixture-run",
          orgId: "fixture-org",
          runKind: "work",
          source: "trusted-host",
          operation: "skill.loaded",
          status: "ok",
          durationMs: 1,
          requestDigest: `sha256:${"4".repeat(64)}`,
          responseDigest: `sha256:${"5".repeat(64)}`,
          evidence: {
            id: "fixture-skill",
            contentDigest: `sha256:${"6".repeat(64)}`,
          },
        },
      ],
    };
    const wrapDriver = (
      driver: EvalDriver,
      seen: Array<EvalSemanticEvidence | undefined>,
      attachEvidence: boolean,
    ): EvalDriver => {
      const execute = driver.execute.bind(driver);
      const score = driver.score.bind(driver);
      driver.execute = async (context) => ({
        ...(await execute(context)),
        ...(attachEvidence ? { semanticEvidence: evidence } : {}),
      });
      driver.score = (input) => {
        seen.push(input.execution.semanticEvidence);
        return score(input);
      };
      return driver;
    };

    const initialSeen: Array<EvalSemanticEvidence | undefined> = [];
    const initial = await runEvaluation({
      loaded,
      plan,
      driver: wrapDriver(
        createFixtureDriver({ loaded, plan, callLog: paths.callLog }),
        initialSeen,
        true,
      ),
      cwd: process.cwd(),
      stateRoot: paths.stateRoot,
      resultsRoot: paths.resultsRoot,
      promote: true,
    });
    expect(initialSeen).toEqual(plan.slots.map(() => evidence));

    const stored = await readStateEpisodes(paths.stateRoot, initial.manifest);
    expect(stored.map((episode) => episode.semanticEvidence)).toEqual(
      plan.slots.map(() => evidence),
    );

    const rescoreSeen: Array<EvalSemanticEvidence | undefined> = [];
    const rescored = await rescoreEvaluation({
      loaded,
      plan,
      driver: wrapDriver(
        createFixtureDriver({ loaded, plan, callLog: paths.callLog }),
        rescoreSeen,
        false,
      ),
      stateRoot: paths.stateRoot,
      runId: initial.runId,
    });
    expect(rescoreSeen).toEqual(initialSeen);

    const publicLines = await readFile(
      join(initial.resultDir!, "results.jsonl"),
      "utf8",
    );
    expect(publicLines).not.toContain("semanticEvidence");
    expect(publicLines).not.toContain("private-evidence-marker");
    const rescoreDocument = await readFile(rescored.path, "utf8");
    expect(rescoreDocument).not.toContain("semanticEvidence");
    expect(rescoreDocument).not.toContain("private-evidence-marker");
  });

  it("reads pre-evidence episode checkpoints and existing configs", async () => {
    const paths = await fixture();
    const loaded = await loadEval(paths.configPath);
    expect(loaded.config.id).toBe("fixture");

    const store = new EvalStateStore(paths.stateRoot);
    const legacyEpisode = {
      schemaVersion: "openneko.eval.episode/v1" as const,
      runId: "legacy-run",
      slotKey: "fixture/legacy-slot",
      caseId: "legacy-case",
      caseContentId: `sha256:${"1".repeat(64)}`,
      family: "read" as const,
      productPath: "fixture",
      difficulty: "smoke",
      capabilityTags: ["fixture.read"],
      semantics: ["RUNTIME-RESUME"],
      variantId: "ax-fixture",
      datasetId: "fixture",
      repetition: 1,
      phase: "initial",
      attempt: 1,
      startedAt: "2026-08-10T00:00:00.000Z",
      finishedAt: "2026-08-10T00:00:01.000Z",
      status: "completed" as const,
      output: { actual: 1 },
      measurements: {},
      observations: [],
    };
    await put(
      store.episodePath(legacyEpisode.runId, legacyEpisode.slotKey),
      `${JSON.stringify({
        ...legacyEpisode,
        integrityDigest: contentDigest(legacyEpisode),
      })}\n`,
    );

    const restoredLegacy = await store.readEpisode(
      legacyEpisode.runId,
      legacyEpisode.slotKey,
    );
    expect(restoredLegacy).toMatchObject({
      runId: legacyEpisode.runId,
      slotKey: legacyEpisode.slotKey,
    });
    expect(restoredLegacy?.semanticEvidence).toBeUndefined();
    expect(restoredLegacy?.pairKey).toBeUndefined();
    expect(restoredLegacy?.treatment).toBeUndefined();
  });

  it("blocks publication and verification when required episode metrics are missing", async () => {
    const paths = await fixture({ v4Policy: true });
    const loaded = await loadEval(paths.configPath);
    const plan = createEvalPlan(loaded);
    const result = await runEvaluation({
      loaded, plan, driver: createFixtureDriver({ loaded, plan, callLog: paths.callLog }),
      cwd: process.cwd(), stateRoot: paths.stateRoot, resultsRoot: paths.resultsRoot,
      promote: true,
    });
    const episodes = await readStateEpisodes(paths.stateRoot, result.manifest);
    for (const field of ["toolCalls", "totalTokens", "wallDurationMs", "usageCoverage", "costCoverage"]) {
      const incomplete = structuredClone(episodes);
      delete incomplete[0]!.measurements[field];
      await expect(promoteResult({ manifest: result.manifest, episodes: incomplete, resultsRoot: paths.resultsRoot }))
        .rejects.toThrow(new RegExp(field));
      const file = join(result.resultDir!, "results.jsonl");
      const original = await readFile(file, "utf8");
      const lines = original.trim().split("\n").map((line) => JSON.parse(line));
      delete lines[0].measurements[field];
      const changed = lines.map((line) => JSON.stringify(line)).join("\n") + "\n";
      const manifestPath = join(result.resultDir!, "manifest.json");
      const manifest = JSON.parse(await readFile(manifestPath, "utf8"));
      manifest.files["results.jsonl"] = textDigest(changed);
      await writeFile(file, changed);
      await writeFile(manifestPath, JSON.stringify(manifest));
      await expect(verifyResult(result.resultDir!)).rejects.toThrow();
      await writeFile(file, original);
      manifest.files["results.jsonl"] = textDigest(original);
      await writeFile(manifestPath, JSON.stringify(manifest));
    }
    for (const value of [-1, NaN, Infinity, 0.5, Number.MAX_SAFE_INTEGER + 1, "12"]) {
      const invalid = structuredClone(episodes);
      invalid[0]!.measurements.toolCalls = value;
      expect(() => assertRequiredMetrics(invalid)).toThrow(/toolCalls/);
    }
    const summary = summarizeEpisodes(episodes);
    const header = "| Backend / model | Suite | Tasks × repetitions | Tasks passed | Execution failures | Unsafe effects | Source commit | Evidence | Latency p50 / p95 | Total tool calls | Total tokens | Estimated run cost |";
    const readme = `${header}\n| Fixture | v4 | 3 × 1 | 3/3 | 0 | 0 | source | [Report](result/summary.md) | ${readmeMetricsCells(summary)}`;
    expect(() => assertReadmeMetrics(readme, summary, "result/summary.md")).not.toThrow();
    for (const removed of [header, "[Report](result/summary.md)", ...readmeMetricsCells(summary).split("|").map(cell => cell.trim()).filter(Boolean)]) {
      expect(() => assertReadmeMetrics(readme.replace(removed, ""), summary, "result/summary.md")).toThrow(/README/);
    }
    const scoreMissing = structuredClone(episodes);
    delete scoreMissing[0]!.score;
    expect(() => assertRequiredMetrics(scoreMissing)).toThrow(/score metrics/);
    scoreMissing[0]!.status = "failed";
    expect(() => assertRequiredMetrics(scoreMissing)).not.toThrow();
    const costMissing = structuredClone(episodes);
    costMissing[0]!.measurements.costCoverage = "complete";
    expect(() => assertRequiredMetrics(costMissing)).toThrow(/estimatedCostUsd/);
    expect(() => assertRequiredMetrics([])).toThrow(/no episodes/);
    await expect(verifyResult(result.resultDir!)).resolves.toMatchObject({ ok: true });
  });

  it("requires token coverage when configured without requiring dollar cost", async () => {
    const partialPaths = await fixture({ minTokenUsageCoverage: 1 });
    const partialLoaded = await loadEval(partialPaths.configPath);
    const partialPlan = createEvalPlan(partialLoaded);
    const withPartialTokens = await runEvaluation({
      loaded: partialLoaded,
      plan: partialPlan,
      driver: createFixtureDriver({
        loaded: partialLoaded,
        plan: partialPlan,
        callLog: partialPaths.callLog,
        totalTokens: 42,
        usageCoverage: "partial",
      }),
      cwd: process.cwd(),
      stateRoot: partialPaths.stateRoot,
      resultsRoot: partialPaths.resultsRoot,
      promote: true,
    });
    expect(withPartialTokens.gatesPassed).toBe(false);
    await expect(verifyResult(withPartialTokens.resultDir!)).resolves.toMatchObject({
      gatesPassed: false,
    });

    const tokenPaths = await fixture({ minTokenUsageCoverage: 1 });
    const tokenLoaded = await loadEval(tokenPaths.configPath);
    const tokenPlan = createEvalPlan(tokenLoaded);
    const withTokens = await runEvaluation({
      loaded: tokenLoaded,
      plan: tokenPlan,
      driver: createFixtureDriver({
        loaded: tokenLoaded,
        plan: tokenPlan,
        callLog: tokenPaths.callLog,
        totalTokens: 42,
      }),
      cwd: process.cwd(),
      stateRoot: tokenPaths.stateRoot,
      resultsRoot: tokenPaths.resultsRoot,
      promote: true,
    });
    expect(withTokens.gatesPassed).toBe(true);
    await expect(verifyResult(withTokens.resultDir!)).resolves.toMatchObject({
      gatesPassed: true,
    });
    const markdown = await readFile(join(withTokens.resultDir!, "summary.md"), "utf8");
    expect(markdown).not.toMatch(/accepted|rejected/iu);
    expect(markdown).toContain("| Estimated cost | unavailable |");
  });

  it("closes adapter resources when setup or rescoring fails", async () => {
    const paths = await fixture();
    const loaded = await loadEval(paths.configPath);
    const plan = createEvalPlan(loaded);
    let setupCloses = 0;
    const setupDriver = createFixtureDriver({ loaded, plan, callLog: paths.callLog });
    setupDriver.preflight = async () => {
      throw new Error("preflight failed");
    };
    setupDriver.close = async () => {
      setupCloses += 1;
    };
    await expect(
      runEvaluation({
        loaded,
        plan,
        driver: setupDriver,
        cwd: process.cwd(),
        stateRoot: paths.stateRoot,
        resultsRoot: paths.resultsRoot,
        promote: false,
      }),
    ).rejects.toThrow("preflight failed");
    expect(setupCloses).toBe(1);

    const initial = await runEvaluation({
      loaded,
      plan,
      driver: createFixtureDriver({ loaded, plan, callLog: paths.callLog }),
      cwd: process.cwd(),
      stateRoot: paths.stateRoot,
      resultsRoot: paths.resultsRoot,
      promote: false,
    });
    let rescoreCloses = 0;
    const rescoreDriver = createFixtureDriver({ loaded, plan, callLog: paths.callLog });
    rescoreDriver.score = () => {
      throw new Error("scoring failed");
    };
    rescoreDriver.close = async () => {
      rescoreCloses += 1;
    };
    await expect(
      rescoreEvaluation({
        loaded,
        plan,
        driver: rescoreDriver,
        stateRoot: paths.stateRoot,
        runId: initial.runId,
      }),
    ).rejects.toThrow("scoring failed");
    expect(rescoreCloses).toBe(1);
  });

  it("resets and replays an ambiguous mutation slot after a hard exit", async () => {
    const paths = await fixture({ family: "mutation" });
    const childPath = fileURLToPath(new URL("./fixtures/crash-runner.ts", import.meta.url));
    const child = spawnSync(
      process.execPath,
      [
        "--import",
        "tsx/esm",
        childPath,
        paths.configPath,
        paths.stateRoot,
        paths.resultsRoot,
        paths.callLog,
        process.cwd(),
        paths.resetLog,
      ],
      { cwd: process.cwd(), encoding: "utf8" },
    );
    expect(child.status, child.stderr).toBe(91);

    const loaded = await loadEval(paths.configPath);
    const plan = createEvalPlan(loaded);
    const result = await runEvaluation({
      loaded,
      plan,
      driver: createFixtureDriver({
        loaded,
        plan,
        callLog: paths.callLog,
        resetLog: paths.resetLog,
      }),
      cwd: process.cwd(),
      stateRoot: paths.stateRoot,
      resultsRoot: paths.resultsRoot,
      promote: false,
    });
    const resets = (await readFile(paths.resetLog, "utf8")).trim().split("\n");
    const interruptedSlot = (await readFile(paths.callLog, "utf8")).trim().split("\n")[1]!;
    expect(result.resumed).toBe(true);
    expect(resets.filter((slot) => slot === interruptedSlot)).toHaveLength(3);
    expect(resets).toHaveLength(7);
  });

  it("stops before the next slot when the configured cost budget is exhausted", async () => {
    const paths = await fixture({ maxCostUsd: 1 });
    const loaded = await loadEval(paths.configPath);
    const plan = createEvalPlan(loaded);
    await expect(
      runEvaluation({
        loaded,
        plan,
        driver: createFixtureDriver({
          loaded,
          plan,
          callLog: paths.callLog,
          estimatedCostUsd: 1,
        }),
        cwd: process.cwd(),
        stateRoot: paths.stateRoot,
        resultsRoot: paths.resultsRoot,
        promote: false,
      }),
    ).rejects.toThrow(/cost budget .* exhausted/u);
    expect((await readFile(paths.callLog, "utf8")).trim().split("\n")).toHaveLength(1);
  });

  it("extracts ground truth through the oracles command without provider traffic", async () => {
    const paths = await fixture();
    let output = "";
    const oraclesPath = join(paths.root, "oracles.json");
    const code = await runEvalCli(
      ["oracles", "--config", paths.configPath, "--out", oraclesPath],
      {
        adapters: {
          fixture: ({ loaded, plan }) =>
            createFixtureDriver({ loaded, plan, callLog: paths.callLog }),
        },
        stdout: { write: (value) => (output += String(value)) },
      },
    );
    expect(code).toBe(0);
    expect(output).toContain("resolved 3 oracles");
    const document = JSON.parse(await readFile(oraclesPath, "utf8")) as {
      schemaVersion: string;
      digest: string;
      values: Record<string, { expected: number }>;
    };
    expect(document.schemaVersion).toBe("openneko.eval.oracles/v1");
    expect(document.values).toEqual({
      one: { expected: 1 },
      two: { expected: 2 },
      three: { expected: 3 },
    });
    expect(document.digest).toMatch(/^sha256:[0-9a-f]{64}$/u);
    // No execute() call may happen during ground-truth extraction.
    await expect(readFile(paths.callLog, "utf8")).rejects.toThrow();
  });

  it("pairs different backend candidates by neutral key and treatment", async () => {
    const comparisons = {
      one: { pairId: "memory-context", treatment: "present" },
      two: { pairId: "memory-context", treatment: "absent" },
    } as const;
    const baselinePaths = await fixture({
      backend: "scripted-alpha",
      variantId: "candidate-alpha",
      comparisons,
    });
    const resultPaths = await fixture({
      backend: "scripted-beta",
      variantId: "candidate-beta",
      comparisons,
    });
    const baselineLoaded = await loadEval(baselinePaths.configPath);
    const baselinePlan = createEvalPlan(baselineLoaded);
    const resultLoaded = await loadEval(resultPaths.configPath);
    const resultPlan = createEvalPlan(resultLoaded);
    const baselinePresent = baselinePlan.slots.find(
      (slot) => slot.caseId === "one",
    )!;
    const baselineAbsent = baselinePlan.slots.find(
      (slot) => slot.caseId === "two",
    )!;
    expect(baselinePresent.pairKey).toBe(baselineAbsent.pairKey);
    expect(baselinePresent.treatment).toBe("present");
    expect(baselineAbsent.treatment).toBe("absent");
    expect(baselinePresent.key).toContain("candidate-alpha");
    expect(resultPlan.slots[0]!.key).toContain("candidate-beta");
    expect(resultPlan.slots[0]!.pairKey).toBe(baselinePlan.slots[0]!.pairKey);

    const first = await runEvaluation({
      loaded: baselineLoaded,
      plan: baselinePlan,
      driver: createFixtureDriver({
        loaded: baselineLoaded,
        plan: baselinePlan,
        callLog: baselinePaths.callLog,
      }),
      cwd: process.cwd(),
      stateRoot: baselinePaths.stateRoot,
      resultsRoot: baselinePaths.resultsRoot,
      promote: true,
    });
    const resultDriver = createFixtureDriver({
      loaded: resultLoaded,
      plan: resultPlan,
      callLog: resultPaths.callLog,
    });
    const executeResult = resultDriver.execute.bind(resultDriver);
    resultDriver.execute = async (context) => {
      const execution = await executeResult(context);
      return context.slot.caseId === "two"
        ? { ...execution, output: { actual: -1 } }
        : execution;
    };
    const second = await runEvaluation({
      loaded: resultLoaded,
      plan: resultPlan,
      driver: resultDriver,
      cwd: process.cwd(),
      stateRoot: resultPaths.stateRoot,
      resultsRoot: resultPaths.resultsRoot,
      promote: true,
    });
    let output = "";
    const code = await runEvalCli(
      ["compare", "--result", second.resultDir!, "--baseline", first.resultDir!],
      { adapters: {}, stdout: { write: (value) => (output += String(value)) } },
    );
    expect(code).toBe(0);
    expect(JSON.parse(output)).toMatchObject({
      paired: {
        granularity: "episode",
        episodes: 3,
        tasks: 3,
        meanGroundTruthDelta: -1 / 3,
      },
    });
    const comparison = JSON.parse(output) as {
      paired: {
        taskDeltas: Array<{
          pairKey: string;
          treatment: string;
          verdictDelta: number;
          result: { variantId: string };
          baseline: { variantId: string };
        }>;
      };
    };
    expect(
      comparison.paired.taskDeltas
        .filter((delta) => delta.pairKey.includes("memory-context"))
        .map((delta) => delta.treatment)
        .sort(),
    ).toEqual(["absent", "present"]);
    expect(
      comparison.paired.taskDeltas.find(
        (delta) => delta.treatment === "absent",
      ),
    ).toMatchObject({ verdictDelta: -1 });
    expect(
      comparison.paired.taskDeltas.find(
        (delta) => delta.treatment === "present",
      ),
    ).toMatchObject({ verdictDelta: 0 });
    expect(comparison.paired.taskDeltas).toEqual(
      expect.arrayContaining([
        expect.objectContaining({
          result: expect.objectContaining({ variantId: "candidate-beta" }),
          baseline: expect.objectContaining({ variantId: "candidate-alpha" }),
        }),
      ]),
    );
  });

  it("rejects suite, dataset, oracle, and scorer cohort mismatches", async () => {
    const baselinePaths = await fixture({
      backend: "scripted-alpha",
      variantId: "candidate-alpha",
    });
    const resultPaths = await fixture({
      backend: "scripted-beta",
      variantId: "candidate-beta",
    });
    const baselineLoaded = await loadEval(baselinePaths.configPath);
    const baselinePlan = createEvalPlan(baselineLoaded);
    const resultLoaded = await loadEval(resultPaths.configPath);
    const resultPlan = createEvalPlan(resultLoaded);
    const baseline = await runEvaluation({
      loaded: baselineLoaded,
      plan: baselinePlan,
      driver: createFixtureDriver({
        loaded: baselineLoaded,
        plan: baselinePlan,
        callLog: baselinePaths.callLog,
      }),
      cwd: process.cwd(),
      stateRoot: baselinePaths.stateRoot,
      resultsRoot: baselinePaths.resultsRoot,
      promote: true,
    });
    const result = await runEvaluation({
      loaded: resultLoaded,
      plan: resultPlan,
      driver: createFixtureDriver({
        loaded: resultLoaded,
        plan: resultPlan,
        callLog: resultPaths.callLog,
      }),
      cwd: process.cwd(),
      stateRoot: resultPaths.stateRoot,
      resultsRoot: resultPaths.resultsRoot,
      promote: true,
    });
    const manifestPath = join(result.resultDir!, "manifest.json");
    const originalManifest = JSON.parse(
      await readFile(manifestPath, "utf8"),
    ) as { compatibility: Record<string, string> };

    for (const field of [
      "suiteDigest",
      "datasetDigest",
      "oracleDigest",
      "scorerDigest",
    ]) {
      const manifest = structuredClone(originalManifest);
      manifest.compatibility[field] = `sha256:${"f".repeat(64)}`;
      await writeFile(manifestPath, `${JSON.stringify(manifest, null, 2)}\n`);
      await expect(
        runEvalCli(
          [
            "compare",
            "--result",
            result.resultDir!,
            "--baseline",
            baseline.resultDir!,
          ],
          { adapters: {} },
        ),
      ).rejects.toThrow(new RegExp(field, "u"));
    }
    await writeFile(
      manifestPath,
      `${JSON.stringify(originalManifest, null, 2)}\n`,
    );
  });
});
