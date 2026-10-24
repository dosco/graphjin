import { describe, expect, it } from "vitest";
import { parseJsonFromOutput } from "../src/agent-backends/output";

describe("parseJsonFromOutput", () => {
  it("parses raw JSON object", () => {
    const out = parseJsonFromOutput('{"mood":"good","value":42}');
    expect(out).toEqual({ mood: "good", value: 42 });
  });

  it("strips ```json fenced block", () => {
    const raw = '```json\n{"mood":"watch","value":1}\n```';
    expect(parseJsonFromOutput(raw)).toEqual({ mood: "watch", value: 1 });
  });

  it("strips bare ``` fenced block (no language tag)", () => {
    const raw = "```\n{\"k\":\"v\"}\n```";
    expect(parseJsonFromOutput(raw)).toEqual({ k: "v" });
  });

  it("slices first '{' to last '}' when JSON is wrapped in prose", () => {
    const raw = "Here is your output:\n{\"mood\":\"bad\",\"score\":7}\nThanks!";
    expect(parseJsonFromOutput(raw)).toEqual({ mood: "bad", score: 7 });
  });

  it("tolerates leading + trailing whitespace", () => {
    const raw = "   \n{\"a\":1}\n  ";
    expect(parseJsonFromOutput(raw)).toEqual({ a: 1 });
  });

  it("throws with a head-of-output excerpt when no braces are present", () => {
    expect(() => parseJsonFromOutput("just a sentence with no json")).toThrow(
      /not parseable as JSON/,
    );
  });

  it("throws when braces enclose malformed JSON", () => {
    expect(() => parseJsonFromOutput("{not: valid, json}")).toThrow();
  });

  it("raw arrays parse successfully (JSON.parse path)", () => {
    // The raw-parse branch happily parses any valid JSON — including arrays.
    // Object-biased brace-slicing is only the fallback for prose-wrapped
    // output. Documented here because the metric agent's contract is one
    // object, but the parser itself is permissive.
    expect(parseJsonFromOutput("[1, 2, 3]")).toEqual([1, 2, 3]);
  });

  it("prose-wrapped arrays are NOT recovered (slice strategy looks for braces, not brackets)", () => {
    expect(() => parseJsonFromOutput("Here you go: [1, 2, 3] — done.")).toThrow();
  });
});
