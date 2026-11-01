import { describe, expect, it } from "vitest";
import { presentWorkFailure } from "../../src/lib/work-failure";

describe("Work failure presentation", () => {
  it("replaces provider credential bodies with an actionable summary", () => {
    const failure = presentWorkFailure(
      'HTTP 400: [{ "error": { "message": "Please pass a valid API key" } }]',
    );

    expect(failure.summary).toContain("provider rejected its credentials");
    expect(failure.technical).toBe(true);
    expect(failure.detail).toContain("HTTP 400");
  });

  it("explains a missing managed provider dependency without asking the operator to pip install", () => {
    const failure = presentWorkFailure(
      "Internal error: The 'anthropic' package is required for the Anthropic provider.",
    );

    expect(failure.summary).toContain("unavailable in this OpenNeko installation");
    expect(failure.summary).not.toContain("pip install");
    expect(failure.technical).toBe(true);
  });

  it("never includes an HTML response body in the rendered technical detail", () => {
    const failure = presentWorkFailure(
      "HTTP 500: <!doctype html><html><body>framework trace</body></html>",
    );

    expect(failure.summary).toContain("could not load this result");
    expect(failure.detail).not.toContain("<html");
    expect(failure.detail).toContain("contents are omitted");
  });

  it("does not mistake ordinary assistant prose for a transport failure", () => {
    const failure = presentWorkFailure(
      "Revenue increased in August after the enterprise renewal closed.",
    );

    expect(failure.technical).toBe(false);
  });
});
