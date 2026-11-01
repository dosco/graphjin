import { describe, expect, it } from "vitest";
import {
  POLL_INTERVAL_MS,
  pollBackoffMs,
  shouldLogPollFailure,
} from "../../src/channels/poll-backoff.js";

describe("pollBackoffMs", () => {
  it("polls at the base interval while healthy", () => {
    expect(pollBackoffMs(0)).toBe(POLL_INTERVAL_MS);
    expect(pollBackoffMs(-1)).toBe(POLL_INTERVAL_MS);
  });

  it("doubles per consecutive failure", () => {
    expect(pollBackoffMs(1)).toBe(3_000);
    expect(pollBackoffMs(2)).toBe(6_000);
    expect(pollBackoffMs(3)).toBe(12_000);
    expect(pollBackoffMs(4)).toBe(24_000);
    expect(pollBackoffMs(5)).toBe(48_000);
  });

  it("caps the backoff at 60s", () => {
    expect(pollBackoffMs(6)).toBe(60_000);
    expect(pollBackoffMs(7)).toBe(60_000);
    expect(pollBackoffMs(100)).toBe(60_000);
  });
});

describe("shouldLogPollFailure", () => {
  it("logs whenever the error changes", () => {
    expect(shouldLogPollFailure(1, true)).toBe(true);
    expect(shouldLogPollFailure(7, true)).toBe(true);
  });

  it("suppresses identical repeats except every 10th", () => {
    expect(shouldLogPollFailure(2, false)).toBe(false);
    expect(shouldLogPollFailure(9, false)).toBe(false);
    expect(shouldLogPollFailure(10, false)).toBe(true);
    expect(shouldLogPollFailure(20, false)).toBe(true);
  });
});
