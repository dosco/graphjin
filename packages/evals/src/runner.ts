import { randomUUID } from "node:crypto";
import { execFile } from "node:child_process";
import { readFile } from "node:fs/promises";
import { promisify } from "node:util";
import {
  MemoryObservationSink,
  createHarnessObserver,
  redactText,
  type HarnessObserver,
} from "@neko/telemetry";
import { contentDigest } from "./canonical";
import type { LoadedCase, LoadedEval } from "./load";
import type { EvalPlan, EvalSlot } from "./plan";
import { evaluateSuiteGates, promoteResult } from "./report";
import { assertionsForPhase, summarizeEpisodes } from "./scoring";
import {
  EvalSemanticEvidenceSchema,
  type EvalSemanticEvidence,
  type EvalEpisode,
  type EvalManifest,
  type EvalScore,
  type EvalVariant,
} from "./schemas";
import { EvalStateStore } from "./state-store";

const execFileAsync = promisify(execFile);
export const EVAL_RUNNER_VERSION = "0.1.0";

export type EvalExecution = {
  output?: unknown;
  measurements?: Record<string, unknown>;
  /**
   * Private semantic evidence used for method and safety scoring. It is
   * checkpointed with the episode and never included in promoted results.
   */
  semanticEvidence?: EvalSemanticEvidence;
};

export type EvalDriverContext = {
  loaded: LoadedEval;
  plan: EvalPlan;
};

export type EvalSlotContext = EvalDriverContext & {
  runId: string;
  slot: EvalSlot;
  oracle: unknown;
  signal: AbortSignal;
  observer: HarnessObserver;
};

export interface EvalDriver {
  readonly id: string;
  readonly scorer: { id: string; version: string; definition: unknown };
  preflight(context: EvalDriverContext): Promise<{
    datasetFingerprint: unknown;
    resolvedVariants?: Record<string, unknown>;
  }>;
  resolveOracle(
    evalCase: LoadedCase,
    context: EvalDriverContext,
  ): Promise<unknown>;
  reset?(context: Omit<EvalSlotContext, "observer" | "signal">): Promise<void>;
  execute(context: EvalSlotContext): Promise<EvalExecution>;
  score(input: {
    case: LoadedCase;
    variant: EvalVariant;
    oracle: unknown;
    execution: EvalExecution;
    /** Phase of the scored slot; binds phase-keyed oracle bundles and phase-bound assertions. */
    phase: string;
    repetition: number;
  }): Promise<EvalScore> | EvalScore;
  close?(): Promise<void>;
}

export class EvalEnvironmentError extends Error {
  constructor(
    message: string,
    readonly errorType = "environment_error",
  ) {
    super(message);
    this.name = "EvalEnvironmentError";
  }
}

export class EvalTaskError extends Error {
  constructor(
    message: string,
    readonly errorType = "task_error",
    /** Private partial execution data retained for terminal task failures. */
    readonly execution?: EvalExecution,
  ) {
    super(message);
    this.name = "EvalTaskError";
  }
}

function durationMs(value: string): number {
  const match = /^(\d+)(ms|s|m|h)$/u.exec(value);
  if (!match) throw new Error(`invalid duration ${value}`);
  const amount = Number(match[1]);
  return (
    amount * ({ ms: 1, s: 1_000, m: 60_000, h: 3_600_000 }[match[2]!] ?? 1)
  );
}

function errorInfo(cause: unknown): {
  type: string;
  message: string;
  environment: boolean;
} {
  const message = redactText(
    cause instanceof Error ? cause.message : String(cause),
  ).slice(0, 2048);
  const typed = cause instanceof EvalEnvironmentError || cause instanceof EvalTaskError;
  const type = typed ? cause.errorType : cause instanceof Error ? cause.name : "unknown";
  return {
    type: type.replace(/[^A-Za-z0-9_.:-]+/g, "_").slice(0, 128) || "unknown",
    message,
    environment: !(cause instanceof EvalTaskError),
  };
}

function createRunId(now = new Date()): string {
  return `run-${now.toISOString().replace(/[-:.]/g, "").toLowerCase()}-${randomUUID().slice(0, 8)}`;
}

export async function readSourceIdentity(cwd: string): Promise<{
  commit: string;
  dirty: boolean;
  digest: string;
}> {
  try {
    const [{ stdout: commitOut }, { stdout: diff }, { stdout: untrackedOut }] =
      await Promise.all([
        execFileAsync("git", ["rev-parse", "HEAD"], { cwd }),
        execFileAsync("git", ["diff", "--binary", "HEAD"], {
          cwd,
          maxBuffer: 32 * 1024 * 1024,
        }),
        execFileAsync("git", ["ls-files", "--others", "--exclude-standard"], {
          cwd,
        }),
      ]);
    const untracked = untrackedOut.split("\n").filter(Boolean).sort();
    const untrackedContents = await Promise.all(
      untracked.map(async (path) => {
        try {
          return [path, contentDigest(await readFile(`${cwd}/${path}`))];
        } catch {
          return [path, "unreadable"];
        }
      }),
    );
    const commit = commitOut.trim();
    return {
      commit,
      dirty: Boolean(diff || untracked.length),
      digest: contentDigest({ commit, diff, untrackedContents }),
    };
  } catch {
    return {
      commit: "unknown",
      dirty: true,
      digest: contentDigest({ cwd, source: "unknown" }),
    };
  }
}

async function resolveOracles(
  driver: EvalDriver,
  loaded: LoadedEval,
  plan: EvalPlan,
): Promise<Record<string, unknown>> {
  const oracles: Record<string, unknown> = {};
  for (const evalCase of loaded.cases) {
    oracles[evalCase.id] = await driver.resolveOracle(evalCase, {
      loaded,
      plan,
    });
  }
  return oracles;
}

function updateProgress(
  manifest: EvalManifest,
  input: { completed: Set<string>; failed: Set<string>; inFlight?: string },
): EvalManifest {
  const now = new Date().toISOString();
  return {
    ...manifest,
    updatedAt: now,
    completedSlotKeys: [...input.completed].sort(),
    failedSlotKeys: [...input.failed].sort(),
    inFlightSlotKeys: input.inFlight ? [input.inFlight] : [],
  };
}

async function partialReport(
  store: EvalStateStore,
  manifest: EvalManifest,
  episodes: readonly EvalEpisode[],
): Promise<void> {
  await store.writePartialReport(manifest.runId, {
    schemaVersion: "openneko.eval.partial-report/v1",
    runId: manifest.runId,
    status: manifest.status,
    completed: episodes.length,
    expected: manifest.expectedSlotKeys.length,
    summary: summarizeEpisodes(episodes),
  });
}

function measuredTotal(
  episodes: readonly EvalEpisode[],
  field: string,
): number {
  return episodes.reduce((sum, episode) => {
    const value = Number(episode.measurements[field]);
    return sum + (Number.isFinite(value) && value > 0 ? value : 0);
  }, 0);
}

function semanticsForSlot(slot: EvalSlot): string[] {
  const dataPathSemantic: Partial<Record<EvalVariant["data_path"], string>> = {
    "graphjin-direct": "DATA-DIRECT",
    "graphjin-agent": "DATA-DELEGATED",
    "planner-host-execute": "DATA-PLANNER-EXECUTE",
  };
  const semantic = dataPathSemantic[slot.variant.data_path];
  return [...new Set([...slot.case.semantics, ...(semantic ? [semantic] : [])])];
}

function assertionTargetsForSlot(slot: EvalSlot) {
  return assertionsForPhase(slot.case.assertions, slot.phase).flatMap(
    (assertion) =>
      assertion.capabilities
        ? [
            {
              assertionId: assertion.id,
              dimension: assertion.dimension,
              gate: assertion.gate,
              capabilities: assertion.capabilities,
              semantics: assertion.semantics ?? [],
            },
          ]
        : [],
  );
}

function positiveInteger(value: unknown): number | null {
  return Number.isSafeInteger(value) && Number(value) > 0
    ? Number(value)
    : null;
}

function runtimeBudgets(loaded: LoadedEval): EvalManifest["runtimeBudgets"] {
  const outputLimits = loaded.config.variants.map((variant) =>
    positiveInteger(variant.outer_model.config?.max_output_tokens),
  );
  const contextLimits = loaded.config.variants.map((variant) =>
    positiveInteger(variant.outer_model.config?.context_window_tokens),
  );
  const commonLimit = (values: readonly (number | null)[]) =>
    values.length > 0 && values.every((value) => value === values[0])
      ? (values[0] ?? null)
      : null;
  const providerOutputTokenLimit = commonLimit(outputLimits);
  const modelContextTokenLimit = commonLimit(contextLimits);
  const resolutionNotes: string[] = [];
  if (providerOutputTokenLimit === null) {
    resolutionNotes.push(
      "provider output-token limit was not explicitly resolved by every variant",
    );
  }
  if (modelContextTokenLimit === null) {
    resolutionNotes.push(
      "model context limit was not explicitly resolved by every variant",
    );
  }
  return {
    perEpisodeTimeoutMs: durationMs(loaded.config.defaults.timeout),
    providerOutputTokenLimit,
    modelContextTokenLimit,
    toolCallCeilings: Object.fromEntries(
      loaded.config.variants.map((variant) => [
        variant.id,
        positiveInteger(variant.settings?.max_tool_calls),
      ]),
    ),
    harnessMaxAttempts: loaded.config.defaults.max_attempts,
    backendRetryAttempts: Object.fromEntries(
      loaded.config.variants.map((variant) => [
        variant.id,
        Number.isSafeInteger(variant.settings?.backend_retry_attempts) &&
        Number(variant.settings?.backend_retry_attempts) >= 0
          ? Number(variant.settings?.backend_retry_attempts)
          : 0,
      ]),
    ),
    concurrency: loaded.config.defaults.concurrency,
    cacheState: loaded.config.defaults.cache_state,
    configuredModels: Object.fromEntries(
      loaded.config.variants.map((variant) => [
        variant.id,
        {
          provider: variant.outer_model.provider,
          model: variant.outer_model.model,
        },
      ]),
    ),
    resolutionNotes,
  };
}

export type RunEvaluationOptions = {
  loaded: LoadedEval;
  plan: EvalPlan;
  driver: EvalDriver;
  cwd: string;
  stateRoot: string;
  resultsRoot: string;
  resumeRunId?: string;
  restart?: boolean;
  promote?: boolean;
};

export async function runEvaluation(options: RunEvaluationOptions): Promise<{
  runId: string;
  resumed: boolean;
  reusedEpisodes: number;
  gatesPassed: boolean;
  resultDir?: string;
  manifest: EvalManifest;
}> {
  if (options.plan.adapter !== options.driver.id) {
    throw new Error(
      `config requests adapter ${options.plan.adapter}, received ${options.driver.id}`,
    );
  }
  if (options.loaded.config.defaults.concurrency !== 1) {
    throw new Error(
      "eval runner v0.1 requires concurrency: 1; use deterministic shards for parallel quality runs",
    );
  }
  let runLoopOwnsDriver = false;
  try {
    const driverContext = { loaded: options.loaded, plan: options.plan };
    const preflight = await options.driver.preflight(driverContext);
    const oracles = await resolveOracles(
      options.driver,
      options.loaded,
      options.plan,
    );
    const oracleDigest = contentDigest(oracles);
    const source = await readSourceIdentity(options.cwd);
    const scorerDigest = contentDigest(options.driver.scorer);
    const compatibility: EvalManifest["compatibility"] = {
      configDigest: options.loaded.digests.config,
      suiteDigest: contentDigest({
        suite: options.loaded.digests.suite,
        cases: options.loaded.digests.cases,
      }),
      datasetDigest: contentDigest({
        manifests: options.loaded.digests.datasets,
        runtime: preflight.datasetFingerprint,
      }),
      oracleDigest,
      scorerDigest,
      variantDigest: contentDigest(
        preflight.resolvedVariants ??
          Object.fromEntries(
            options.loaded.config.variants.map((variant) => [
              variant.id,
              variant,
            ]),
          ),
      ),
      sourceDigest: source.digest,
      ...(options.loaded.digests.thresholdPolicy
        ? { thresholdPolicyDigest: options.loaded.digests.thresholdPolicy }
        : {}),
    };
    const store = new EvalStateStore(options.stateRoot);
    let manifest: EvalManifest | undefined;
    if (options.resumeRunId) {
      manifest = await store.readManifest(options.resumeRunId);
      if (
        contentDigest(manifest.compatibility) !== contentDigest(compatibility)
      ) {
        throw new Error(
          `run ${options.resumeRunId} is incompatible with the current eval inputs`,
        );
      }
    } else if (!options.restart) {
      manifest = await store.findCompatibleIncomplete(compatibility);
    }
    const resumed = Boolean(manifest);
    if (!manifest) {
      const now = new Date().toISOString();
      manifest = {
        schemaVersion: "openneko.eval.manifest/v1",
        runnerVersion: EVAL_RUNNER_VERSION,
        runId: createRunId(),
        configId: options.loaded.config.id,
        suiteId: options.loaded.suite.id,
        attestation: "self-reported",
        suiteGates: options.loaded.suite.gates,
        ...(options.loaded.thresholdPolicy
          ? {
              thresholdPolicy: options.loaded.thresholdPolicy,
              thresholdPolicyDigest: options.loaded.digests.thresholdPolicy,
            }
          : {}),
        runtimeBudgets: runtimeBudgets(options.loaded),
        status: "in_progress",
        createdAt: now,
        updatedAt: now,
        source: { commit: source.commit, dirty: source.dirty },
        compatibility,
        datasetFingerprint: preflight.datasetFingerprint,
        resolvedVariants: preflight.resolvedVariants ?? {},
        effectiveConfig: options.loaded.config,
        plannedSlotKeys: options.plan.slots.map((slot) => slot.key),
        expectedSlotKeys: options.plan.slots.map((slot) => slot.key).sort(),
        completedSlotKeys: [],
        failedSlotKeys: [],
        inFlightSlotKeys: [],
      };
      await store.initialize(manifest);
      await store.writeOracles(manifest.runId, {
        schemaVersion: "openneko.eval.oracles/v1",
        digest: oracleDigest,
        values: oracles,
      });
    } else {
      const stored = await store.readOracles(manifest.runId);
      if (stored.digest !== oracleDigest) {
        throw new Error(
          `run ${manifest.runId} oracle values changed; refusing unsafe resume`,
        );
      }
    }

    const lock = await store.acquireLock(manifest.runId);
    runLoopOwnsDriver = true;
    const episodes: EvalEpisode[] = [];
    const completed = new Set<string>();
    const failed = new Set<string>();
    let reusedEpisodes = 0;
    try {
      for (const slot of options.plan.slots) {
        const episode = await store.readEpisode(manifest.runId, slot.key);
        if (!episode) continue;
        episodes.push(episode);
        completed.add(slot.key);
        if (
          episode.status !== "completed" ||
          episode.score?.verdict === "fail"
        ) {
          failed.add(slot.key);
        }
      }
      reusedEpisodes = episodes.length;
      manifest = updateProgress(manifest, { completed, failed });
      await store.replaceManifest(manifest);
      await partialReport(store, manifest, episodes);

      for (const slot of options.plan.slots) {
        if (completed.has(slot.key)) continue;
        const wallBudget = options.loaded.config.budgets?.max_wall_time;
        if (
          wallBudget &&
          measuredTotal(episodes, "wallDurationMs") >= durationMs(wallBudget)
        ) {
          throw new EvalEnvironmentError(
            `eval wall-time budget ${wallBudget} is exhausted`,
            "wall_budget_exhausted",
          );
        }
        const costBudget =
          options.loaded.config.budgets?.max_estimated_cost_usd;
        if (
          costBudget !== undefined &&
          measuredTotal(episodes, "estimatedCostUsd") >= costBudget
        ) {
          throw new EvalEnvironmentError(
            `eval estimated cost budget $${costBudget} is exhausted`,
            "cost_budget_exhausted",
          );
        }
        const oracle = oracles[slot.caseId];
        let attempt = await store.nextAttemptNumber(manifest.runId, slot.key);
        const lastAttempt =
          attempt + options.loaded.config.defaults.max_attempts - 1;
        let terminal: EvalEpisode | undefined;
        while (attempt <= lastAttempt && !terminal) {
          const startedAt = new Date().toISOString();
          await store.writeAttempt({
            schemaVersion: "openneko.eval.attempt/v1",
            runId: manifest.runId,
            slotKey: slot.key,
            attempt,
            event: "started",
            timestamp: startedAt,
            status: "running",
          });
          manifest = updateProgress(manifest, {
            completed,
            failed,
            inFlight: slot.key,
          });
          await store.replaceManifest(manifest);

          const memorySink = new MemoryObservationSink();
          const observer = createHarnessObserver({
            runId: manifest.runId,
            sinks: [memorySink],
          });
          const resetContext = {
            ...driverContext,
            runId: manifest.runId,
            slot,
            oracle,
          };
          const timeout = durationMs(
            slot.case.timeout ?? options.loaded.config.defaults.timeout,
          );
          try {
            if (
              slot.case.family === "mutation" ||
              slot.case.family === "watcher"
            ) {
              if (!options.driver.reset) {
                throw new EvalEnvironmentError(
                  `adapter ${options.driver.id} does not implement reset for ${slot.case.family} case ${slot.case.id}`,
                  "reset_unsupported",
                );
              }
              await options.driver.reset(resetContext);
            }
            const rawExecution = await options.driver.execute({
              ...resetContext,
              observer,
              signal: AbortSignal.timeout(timeout),
            });
            const execution: EvalExecution =
              rawExecution.semanticEvidence === undefined
                ? rawExecution
                : {
                    ...rawExecution,
                    semanticEvidence: EvalSemanticEvidenceSchema.parse(
                      rawExecution.semanticEvidence,
                    ),
                  };
            const score = await options.driver.score({
              case: slot.case,
              variant: slot.variant,
              oracle,
              execution,
              phase: slot.phase,
              repetition: slot.repetition,
            });
            const finishedAt = new Date().toISOString();
            await store.writeAttempt({
              schemaVersion: "openneko.eval.attempt/v1",
              runId: manifest.runId,
              slotKey: slot.key,
              attempt,
              event: "finished",
              timestamp: finishedAt,
              status: "completed",
            });
            terminal = await store.writeEpisode({
              schemaVersion: "openneko.eval.episode/v1",
              runId: manifest.runId,
              slotKey: slot.key,
              pairKey: slot.pairKey,
              treatment: slot.treatment,
              caseId: slot.caseId,
              caseContentId: slot.case.contentId,
              family: slot.case.family,
              productPath: slot.case.product_path,
              difficulty: slot.case.difficulty,
              capabilityTags: slot.case.capability_tags,
              semantics: semanticsForSlot(slot),
              ...(assertionTargetsForSlot(slot).length
                ? { assertionTargets: assertionTargetsForSlot(slot) }
                : {}),
              variantId: slot.variantId,
              datasetId: slot.datasetId,
              repetition: slot.repetition,
              phase: slot.phase,
              attempt,
              startedAt,
              finishedAt,
              status: "completed",
              ...(execution.output !== undefined
                ? { output: execution.output }
                : {}),
              measurements: execution.measurements ?? {},
              observations: memorySink.observations,
              ...(execution.semanticEvidence !== undefined
                ? { semanticEvidence: execution.semanticEvidence }
                : {}),
              score,
            });
          } catch (cause) {
            const info = errorInfo(cause);
            const failedExecution =
              cause instanceof EvalTaskError ? cause.execution : undefined;
            const failedEvidence = failedExecution?.semanticEvidence
              ? EvalSemanticEvidenceSchema.parse(
                  failedExecution.semanticEvidence,
                )
              : undefined;
            const finishedAt = new Date().toISOString();
            await store.writeAttempt({
              schemaVersion: "openneko.eval.attempt/v1",
              runId: manifest.runId,
              slotKey: slot.key,
              attempt,
              event: "finished",
              timestamp: finishedAt,
              status: info.environment ? "environment_failure" : "failed",
              errorType: info.type,
              error: info.message,
            });
            if (info.environment && attempt >= lastAttempt) {
              manifest = updateProgress(manifest, { completed, failed });
              await store.replaceManifest(manifest);
              await partialReport(store, manifest, episodes);
              throw new EvalEnvironmentError(
                `run ${manifest.runId} paused at ${slot.key}: ${info.message}; resume with --resume ${manifest.runId}`,
                info.type,
              );
            }
            if (!info.environment) {
              terminal = await store.writeEpisode({
                schemaVersion: "openneko.eval.episode/v1",
                runId: manifest.runId,
                slotKey: slot.key,
                pairKey: slot.pairKey,
                treatment: slot.treatment,
                caseId: slot.caseId,
                caseContentId: slot.case.contentId,
                family: slot.case.family,
                productPath: slot.case.product_path,
                difficulty: slot.case.difficulty,
                capabilityTags: slot.case.capability_tags,
                semantics: semanticsForSlot(slot),
                ...(assertionTargetsForSlot(slot).length
                  ? { assertionTargets: assertionTargetsForSlot(slot) }
                  : {}),
                variantId: slot.variantId,
                datasetId: slot.datasetId,
                repetition: slot.repetition,
                phase: slot.phase,
                attempt,
                startedAt,
                finishedAt,
                status: "failed",
                measurements: failedExecution?.measurements ?? {},
                observations: memorySink.observations,
                ...(failedEvidence !== undefined
                  ? { semanticEvidence: failedEvidence }
                  : {}),
                errorType: info.type,
                error: info.message,
              });
            }
          } finally {
            await observer.shutdown();
            if (
              options.driver.reset &&
              (slot.case.family === "mutation" ||
                slot.case.family === "watcher")
            ) {
              await options.driver.reset(resetContext);
            }
          }
          attempt += 1;
        }
        if (!terminal)
          throw new Error(`slot ${slot.key} ended without an episode`);
        episodes.push(terminal);
        completed.add(slot.key);
        if (
          terminal.status !== "completed" ||
          terminal.score?.verdict === "fail"
        ) {
          failed.add(slot.key);
        }
        manifest = updateProgress(manifest, { completed, failed });
        await store.replaceManifest(manifest);
        await partialReport(store, manifest, episodes);
      }
      manifest = {
        ...updateProgress(manifest, { completed, failed }),
        status: "complete",
      };
      await store.replaceManifest(manifest);
      await partialReport(store, manifest, episodes);
      let resultDir: string | undefined;
      if (
        options.promote ??
        options.loaded.config.artifacts.check_in === "scored"
      ) {
        resultDir = await promoteResult({
          manifest,
          episodes,
          resultsRoot: options.resultsRoot,
        });
        manifest = { ...manifest, resultDir };
        await store.replaceManifest(manifest);
      }
      return {
        runId: manifest.runId,
        resumed,
        reusedEpisodes,
        gatesPassed: evaluateSuiteGates(
          summarizeEpisodes(episodes),
          manifest.suiteGates,
          manifest.thresholdPolicy,
        ),
        ...(resultDir ? { resultDir } : {}),
        manifest,
      };
    } finally {
      try {
        await options.driver.close?.();
      } finally {
        await lock.release();
      }
    }
  } catch (cause) {
    if (!runLoopOwnsDriver) await options.driver.close?.();
    throw cause;
  }
}

export async function rescoreEvaluation(input: {
  loaded: LoadedEval;
  plan: EvalPlan;
  driver: EvalDriver;
  stateRoot: string;
  resultsRoot?: string;
  runId: string;
  promote?: boolean;
}): Promise<{
  path: string;
  episodes: EvalEpisode[];
  gatesPassed: boolean;
  resultDir?: string;
}> {
  try {
    const store = new EvalStateStore(input.stateRoot);
    const manifest = await store.readManifest(input.runId);
    const oracleDocument = await store.readOracles(input.runId);
    const oracles = oracleDocument.values as Record<string, unknown>;
    const rescored: EvalEpisode[] = [];
    for (const slot of input.plan.slots) {
      const episode = await store.readEpisode(input.runId, slot.key);
      if (!episode)
        throw new Error(`cannot rescore incomplete run: missing ${slot.key}`);
      if (episode.status !== "completed") {
        rescored.push(episode);
        continue;
      }
      const execution: EvalExecution = {
        ...(episode.output !== undefined ? { output: episode.output } : {}),
        measurements: episode.measurements,
        ...(episode.semanticEvidence !== undefined
          ? { semanticEvidence: episode.semanticEvidence }
          : {}),
      };
      const score = await input.driver.score({
        case: slot.case,
        variant: slot.variant,
        oracle: oracles[slot.caseId],
        execution,
        phase: slot.phase,
        repetition: slot.repetition,
      });
      rescored.push({ ...episode, score });
    }
    const scorerDigest = contentDigest(input.driver.scorer);
    const path = `${store.runDir(manifest.runId)}/rescore-${scorerDigest.slice(7, 19)}.json`;
    const document = {
      schemaVersion: "openneko.eval.rescore/v1",
      sourceRunId: manifest.runId,
      sourceManifestDigest: contentDigest(manifest),
      scorer: input.driver.scorer,
      scorerDigest,
      summary: summarizeEpisodes(rescored),
      episodes: rescored.map(
        ({
          output: _output,
          observations: _observations,
          semanticEvidence: _semanticEvidence,
          ...episode
        }) => episode,
      ),
    };
    const { mkdir, writeFile } = await import("node:fs/promises");
    const { dirname } = await import("node:path");
    await mkdir(dirname(path), { recursive: true });
    const documentText = `${JSON.stringify(document, null, 2)}\n`;
    await writeFile(path, documentText, "utf8");
    let resultDir: string | undefined;
    if (input.promote) {
      if (!input.resultsRoot) {
        throw new Error("resultsRoot is required to promote a re-score");
      }
      resultDir = await promoteResult({
        manifest,
        episodes: rescored,
        resultsRoot: input.resultsRoot,
        rescore: {
          sourceRunManifestDigest: contentDigest(manifest),
          sourceRescoreDigest: contentDigest(document),
          scorer: input.driver.scorer,
          scorerDigest,
        },
      });
    }
    return {
      path,
      episodes: rescored,
      gatesPassed: evaluateSuiteGates(
        summarizeEpisodes(rescored),
        manifest.suiteGates,
        manifest.thresholdPolicy,
      ),
      ...(resultDir ? { resultDir } : {}),
    };
  } finally {
    await input.driver.close?.();
  }
}
