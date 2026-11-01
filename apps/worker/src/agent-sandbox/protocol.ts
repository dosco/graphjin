/**
 * Wire protocol between the agent sandbox entry and the host launcher. The
 * sandbox streams these tagged JSON lines over the exec's stdout; the launcher
 * greps for them (EVENT lines → host emit, the RESULT line → AgentRunResult).
 * Kept separate from entry.ts so importing the markers never triggers entry's
 * top-level main().
 */
export const EVENT_MARKER = "__openneko_event__";
export const RESULT_MARKER = "__openneko_agent_result__";
export const ARTIFACTS_MARKER = "__openneko_artifacts__";
