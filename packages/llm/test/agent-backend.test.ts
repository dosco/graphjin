import { describe, expect, it } from "vitest";
import {
  AGENT_BACKEND_IDS,
  AGENT_DEFAULT_GLOBAL_CAP,
  isAgentBackendId,
  metricRefreshConcurrency,
} from "../src/agent-backend";

describe("isAgentBackendId", () => {
  it("accepts ax, the only backend", () => {
    expect(AGENT_BACKEND_IDS).toEqual(["ax"]);
    expect(isAgentBackendId("ax")).toBe(true);
  });
  it("rejects removed backend ids", () => {
    expect(isAgentBackendId("removed-runtime")).toBe(false);
  });
  it("rejects unknown values", () => {
    expect(isAgentBackendId("openai")).toBe(false);
    expect(isAgentBackendId("")).toBe(false);
    expect(isAgentBackendId("AX")).toBe(false); // case-sensitive
  });
});

describe("default concurrency cap", () => {
  it("starts three jobs concurrently", () => {
    expect(AGENT_DEFAULT_GLOBAL_CAP).toBe(3);
  });
});

describe("metricRefreshConcurrency", () => {
  it("defaults to two refreshes and never exceeds the global cap", () => {
    expect(metricRefreshConcurrency(3)).toBe(2);
    expect(metricRefreshConcurrency(1)).toBe(1);
  });
  it("follows a valid override up to the global cap", () => {
    expect(metricRefreshConcurrency(8, "4")).toBe(4);
    expect(metricRefreshConcurrency(3, "6")).toBe(3);
  });
  it("ignores an invalid override", () => {
    expect(metricRefreshConcurrency(8, "0")).toBe(2);
    expect(metricRefreshConcurrency(8, "many")).toBe(2);
    expect(metricRefreshConcurrency(8, "")).toBe(2);
  });
});
