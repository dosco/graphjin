import { describe, expect, it } from "vitest";
import { outsideFenceText } from "../src/agent-backends/output";

describe("outsideFenceText", () => {
  it("returns prose untouched when no fences are present", () => {
    expect(outsideFenceText("hello world")).toBe("hello world");
  });

  it("hides a neko_a2ui fence body", () => {
    const raw = "before ```neko_a2ui\n[]\n``` after";
    expect(outsideFenceText(raw)).toBe("before  after");
  });

  it("hides a neko_workflow_save fence body", () => {
    const raw = [
      "Saved 'X'. You can run it from the workflows list.",
      "",
      "```neko_workflow_save",
      '{"name":"X","steps":[{"id":"s","description":"do"}]}',
      "```",
    ].join("\n");
    const out = outsideFenceText(raw);
    expect(out).toContain("Saved 'X'");
    expect(out).not.toContain("neko_workflow_save");
    expect(out).not.toContain('"name":"X"');
  });

  it("hides a neko_workflow_output fence body", () => {
    const raw = [
      "Done with the check.",
      "```neko_workflow_output",
      '{"kind":"observation","title":"APAC dipped"}',
      "```",
    ].join("\n");
    const out = outsideFenceText(raw);
    expect(out).toContain("Done with the check.");
    expect(out).not.toContain("neko_workflow_output");
    expect(out).not.toContain("observation");
  });

  it("hides a neko_action_request fence body", () => {
    const raw = [
      "Proposing one action.",
      "```neko_action_request",
      '{"scope":"external","kind":"send_message","summary":"x"}',
      "```",
    ].join("\n");
    const out = outsideFenceText(raw);
    expect(out).toContain("Proposing one action.");
    expect(out).not.toContain("neko_action_request");
    expect(out).not.toContain("send_message");
  });

  it("holds back a partial workflow_save opener mid-stream", () => {
    // Simulates streaming where the closing ``` hasn't arrived yet.
    const raw = "prose ```neko_workflow_sa";
    expect(outsideFenceText(raw)).toBe("prose ");
  });

  it("holds back a partial action_request opener mid-stream", () => {
    const raw = "prose ```neko_action_req";
    expect(outsideFenceText(raw)).toBe("prose ");
  });

  it("hides multiple different fences in one stream", () => {
    const raw = [
      "Output one:",
      "```neko_workflow_output",
      '{"kind":"finding"}',
      "```",
      "And an action:",
      "```neko_action_request",
      '{"scope":"external","kind":"send_message","summary":"x"}',
      "```",
      "Done.",
    ].join("\n");
    const out = outsideFenceText(raw);
    expect(out).toContain("Output one:");
    expect(out).toContain("And an action:");
    expect(out).toContain("Done.");
    expect(out).not.toContain("neko_workflow_output");
    expect(out).not.toContain("neko_action_request");
    expect(out).not.toContain("send_message");
  });
});
