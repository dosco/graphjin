/**
 * @neko/llm — shared LLM machinery: providers, agents, classifier.
 *
 * Both apps/web (sync RPC: classify, provider-test) and apps/worker (job
 * handlers: profiler, industry-researcher, bootstrap-metrics-writer,
 * metric-agent) consume this package. Keeping it shared lets the web app
 * call LLM functions in-process instead of round-tripping through the
 * worker over HTTP.
 */

export * from "./config";
export * from "./llm";
export * from "./classifier";
export * from "./summarize-briefing";
export * from "./metric-agent";
export * from "./usage-normalization";
export * from "./discovery-pathways";
export * from "./profiler";
export * from "./bootstrap-metrics-writer";
export * from "./industry-researcher";
export * from "./agent-backend";
export { makeAgentBackend } from "./agent-runtime";
export {
  resolveAgentBackend,
  resolveAgentConcurrency,
  type AgentConcurrency,
} from "./agent-backend-resolver";
export { cancelAllAgents, registerAgentCanceller } from "./agent-shutdown";
export {
  DataSourceAuthorizationError,
  UpstreamProviderError,
  detectAgentExecutionError,
  detectDataSourceAuthorizationError,
  detectUpstreamError,
} from "./agent-error";
export {
  ensureHostConfigProvisioned,
  loadAgentModelKeys,
  provisionHostConfig,
  verifyAgentRuntimeReady,
} from "./host-provision";
export {
  prefetchKnowledgePack,
  prefetchKnowledgeForOrg,
  prefetchAgenticKnowledgePack,
  knowledgePackPaths,
  discoveryUrlFromMcpUrl,
  type PrefetchKnowledgeResult,
  type KnowledgePackPaths,
} from "./knowledge-pack";
export * from "./work";
export * from "./library";
export * from "./workflows";
export * from "./graphjin";

export { dispatchEmbeddingJobs, runEmbeddingIndexJob, type EmbeddingIndexPayload } from "./embedding-jobs";
