import { describe, expect, it } from "vitest";
import { cronToEnglish, describeSchedule } from "@/lib/cron-english";

describe("cronToEnglish", () => {
  it("handles every-day patterns", () => {
    expect(cronToEnglish("0 9 * * *", "UTC")).toBe("every day at 9:00 AM UTC");
    expect(cronToEnglish("30 14 * * *", "Asia/Kolkata")).toBe(
      "every day at 2:30 PM Asia/Kolkata",
    );
  });

  it("handles single-weekday patterns", () => {
    expect(cronToEnglish("30 7 * * 1", "UTC")).toBe(
      "every Monday at 7:30 AM UTC",
    );
    expect(cronToEnglish("0 0 * * 0", "UTC")).toBe(
      "every Sunday at 12:00 AM UTC",
    );
  });

  it("handles weekday and weekend shorthand", () => {
    expect(cronToEnglish("0 9 * * 1-5", "UTC")).toBe(
      "every weekday at 9:00 AM UTC",
    );
    expect(cronToEnglish("0 9 * * 0,6")).toBe("every weekend at 9:00 AM");
    expect(cronToEnglish("0 9 * * 6,0")).toBe("every weekend at 9:00 AM");
  });

  it("handles multi-day lists", () => {
    expect(cronToEnglish("0 9 * * 1,3,5", "UTC")).toBe(
      "every Monday, Wednesday and Friday at 9:00 AM UTC",
    );
    expect(cronToEnglish("0 9 * * 1,2", "UTC")).toBe(
      "every Monday and Tuesday at 9:00 AM UTC",
    );
  });

  it("handles every-N-minutes patterns", () => {
    expect(cronToEnglish("*/15 * * * *")).toBe("every 15 minutes");
    expect(cronToEnglish("*/1 * * * *")).toBe("every 1 minute");
  });

  it("handles every-hour patterns", () => {
    expect(cronToEnglish("0 * * * *")).toBe("every hour");
    expect(cronToEnglish("15 * * * *")).toBe("every hour at :15");
  });

  it("returns null for null cron and for unrecognized patterns", () => {
    expect(cronToEnglish(null)).toBeNull();
    expect(cronToEnglish("")).toBeNull();
    // Day-of-month patterns aren't supported (rare for v1 cadence schedules).
    expect(cronToEnglish("0 9 1 * *", "UTC")).toBeNull();
  });

  it("omits timezone label for UTC when none provided", () => {
    expect(cronToEnglish("0 9 * * *")).toBe("every day at 9:00 AM");
  });
});

describe("describeSchedule", () => {
  it("returns 'Runs when you ask' when cron is null", () => {
    expect(describeSchedule(null, "UTC", true)).toBe("Runs when you ask");
  });

  it("appends 'Paused' when cron is set but disabled", () => {
    expect(describeSchedule("0 9 * * *", "UTC", false)).toBe(
      "Every day at 9:00 AM UTC · Paused",
    );
  });

  it("returns plain English when enabled and recognized", () => {
    expect(describeSchedule("30 7 * * 1", "UTC", true)).toBe(
      "Every Monday at 7:30 AM UTC",
    );
  });

  it("falls back to raw cron with timezone when pattern isn't recognized", () => {
    expect(describeSchedule("0 9 1 * *", "Asia/Kolkata", true)).toBe(
      "0 9 1 * * (Asia/Kolkata)",
    );
  });
});
