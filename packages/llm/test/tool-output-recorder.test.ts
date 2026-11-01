import { describe, expect, it, vi } from "vitest";
import { createToolOutputRecorder } from "../src/work/tool-output/metrics";

describe("createToolOutputRecorder — tool_start→tool_end correlation", () => {
  it("records a tool_end under the name from its matching tool_start", () => {
    const record = vi.fn();
    const r = createToolOutputRecorder(record);
    r.observe({ type: "tool_start", id: "t1", name: "Bash" });
    r.observe({ type: "tool_end", id: "t1", result: '{"rows":3}' });
    expect(record).toHaveBeenCalledTimes(1);
    expect(record).toHaveBeenCalledWith({ tool: "Bash", result: '{"rows":3}' });
  });

  it("keeps names distinct across interleaved tool calls", () => {
    const record = vi.fn();
    const r = createToolOutputRecorder(record);
    r.observe({ type: "tool_start", id: "a", name: "graphjin" });
    r.observe({ type: "tool_start", id: "b", name: "Read" });
    r.observe({ type: "tool_end", id: "b", result: "file contents" });
    r.observe({ type: "tool_end", id: "a", result: "[1,2,3]" });
    expect(record.mock.calls.map((c) => c[0].tool)).toEqual(["Read", "graphjin"]);
  });

  it("falls back to 'unknown' when no tool_start preceded the tool_end", () => {
    const record = vi.fn();
    const r = createToolOutputRecorder(record);
    r.observe({ type: "tool_end", id: "orphan", result: "x" });
    expect(record).toHaveBeenCalledWith({ tool: "unknown", result: "x" });
  });

  it("uses 'unknown' when tool_start omits a name", () => {
    const record = vi.fn();
    const r = createToolOutputRecorder(record);
    r.observe({ type: "tool_start", id: "t1" });
    r.observe({ type: "tool_end", id: "t1", result: "x" });
    expect(record).toHaveBeenCalledWith({ tool: "unknown", result: "x" });
  });

  it("ignores non-tool events", () => {
    const record = vi.fn();
    const r = createToolOutputRecorder(record);
    r.observe({ type: "message", role: "assistant", content: "hi" });
    r.observe({ type: "status", message: "thinking" });
    r.observe({ type: "done", result: { status: "completed" } });
    expect(record).not.toHaveBeenCalled();
  });

  it("does not record a tool_end with no result (e.g. an error-only end)", () => {
    const record = vi.fn();
    const r = createToolOutputRecorder(record);
    r.observe({ type: "tool_start", id: "t1", name: "Bash" });
    r.observe({ type: "tool_end", id: "t1", error: "boom" });
    expect(record).not.toHaveBeenCalled();
  });

  it("forgets the id after recording so a stale reuse falls back to unknown", () => {
    const record = vi.fn();
    const r = createToolOutputRecorder(record);
    r.observe({ type: "tool_start", id: "t1", name: "Bash" });
    r.observe({ type: "tool_end", id: "t1", result: "first" });
    r.observe({ type: "tool_end", id: "t1", result: "second" });
    expect(record.mock.calls.map((c) => c[0].tool)).toEqual(["Bash", "unknown"]);
  });

  it("tolerates non-object events without throwing", () => {
    const record = vi.fn();
    const r = createToolOutputRecorder(record);
    expect(() => {
      r.observe(null);
      r.observe(undefined);
      r.observe("nope");
      r.observe(42);
    }).not.toThrow();
    expect(record).not.toHaveBeenCalled();
  });
});
