import { describe, expect, it } from "vitest";
import {
  createScrubber,
  escapeRegex,
  isNoopScrubber,
  REDACTED_PLACEHOLDER,
  scrubAgentEvent,
  scrubJson,
} from "../src/work/secret-scrubber";

describe("createScrubber", () => {
  it("empty input → no-op scrubber", () => {
    const s = createScrubber([]);
    expect(isNoopScrubber(s)).toBe(true);
    expect(s("hello world")).toBe("hello world");
  });

  it("values under 8 chars are excluded (would false-positive)", () => {
    const s = createScrubber(["x", "abc1234"]);
    expect(isNoopScrubber(s)).toBe(true);
    expect(s("contains abc1234 still")).toBe("contains abc1234 still");
  });

  it("redacts a value of length 8 or more", () => {
    const s = createScrubber(["xoxb-abc"]);
    expect(s("token is xoxb-abc and again xoxb-abc")).toBe(
      "token is [REDACTED] and again [REDACTED]",
    );
  });

  it("longest-first ordering: overlapping secrets get the longer one masked first", () => {
    const long = "xoxb-secret-very-long";
    const short = "xoxb-secret";
    const s = createScrubber([short, long]);
    // The combined string contains the long value once; we want it
    // replaced as a whole, not as short+leftover.
    expect(s(`token=${long}`)).toBe("token=[REDACTED]");
  });

  it("deduplicates input values", () => {
    const s = createScrubber(["xoxb-abc", "xoxb-abc"]);
    expect(s("xoxb-abc")).toBe("[REDACTED]");
  });

  it("escapes regex metachars in values", () => {
    const s = createScrubber(["sk-?.+(weird)$"]);
    expect(s("contains sk-?.+(weird)$ literal")).toBe("contains [REDACTED] literal");
  });

  it("non-string input passes through (defensive)", () => {
    const s = createScrubber(["xoxb-abc"]);
    expect(s(null as unknown as string)).toBe(null);
    expect(s(undefined as unknown as string)).toBe(undefined);
  });

  it("REDACTED placeholder is stable", () => {
    expect(REDACTED_PLACEHOLDER).toBe("[REDACTED]");
  });
});

describe("scrubJson", () => {
  const scrub = createScrubber(["xoxb-secret-1", "xoxp-secret-2"]);

  it("scrubs string leaves recursively", () => {
    const input = {
      message: "use xoxb-secret-1",
      nested: {
        token: "xoxp-secret-2",
        unrelated: 42,
        list: ["xoxb-secret-1", { deep: "also xoxp-secret-2" }],
      },
    };
    const out = scrubJson(scrub, input);
    expect(out.message).toBe("use [REDACTED]");
    expect(out.nested.token).toBe("[REDACTED]");
    expect(out.nested.unrelated).toBe(42);
    expect(out.nested.list[0]).toBe("[REDACTED]");
    expect((out.nested.list[1] as { deep: string }).deep).toBe("also [REDACTED]");
  });

  it("passes through unchanged when the scrubber is a no-op", () => {
    const noop = createScrubber([]);
    const input = { a: "xoxb-secret-1" };
    expect(scrubJson(noop, input)).toBe(input);
  });

  it("does not mutate the input object", () => {
    const input = { msg: "xoxb-secret-1" };
    scrubJson(scrub, input);
    expect(input.msg).toBe("xoxb-secret-1");
  });
});

describe("scrubAgentEvent", () => {
  it("scrubs provider progress summaries before persistence", () => {
    const scrub = createScrubber(["secret-value-123"]);
    expect(
      scrubAgentEvent(scrub, {
        type: "progress",
        id: "gemini-summary-1",
        content: "Checking secret-value-123 now",
        source: "provider_summary",
        provider: "google-gemini",
      }),
    ).toEqual({
      type: "progress",
      id: "gemini-summary-1",
      content: "Checking [REDACTED] now",
      source: "provider_summary",
      provider: "google-gemini",
    });
  });
});

describe("escapeRegex", () => {
  it("escapes every POSIX metachar", () => {
    expect(escapeRegex(".*+?^${}()|[]\\")).toBe(
      "\\.\\*\\+\\?\\^\\$\\{\\}\\(\\)\\|\\[\\]\\\\",
    );
  });
});
