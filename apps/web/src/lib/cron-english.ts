// Turns common cron expressions into plain English. Falls back to the
// raw expression for anything we don't recognize — the goal is to make
// the common cases readable, not to be a full cron parser.

const DAY_NAMES = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"];

type Parsed = {
  minute: string;
  hour: string;
  dom: string;
  month: string;
  dow: string;
};

function parse(cron: string): Parsed | null {
  const parts = cron.trim().split(/\s+/);
  if (parts.length !== 5) return null;
  return {
    minute: parts[0],
    hour: parts[1],
    dom: parts[2],
    month: parts[3],
    dow: parts[4],
  };
}

function formatHourMinute(h: string, m: string): string | null {
  const hourNum = Number(h);
  const minuteNum = Number(m);
  if (!Number.isInteger(hourNum) || !Number.isInteger(minuteNum)) return null;
  if (hourNum < 0 || hourNum > 23 || minuteNum < 0 || minuteNum > 59) return null;
  const period = hourNum < 12 ? "AM" : "PM";
  const display = hourNum === 0 ? 12 : hourNum > 12 ? hourNum - 12 : hourNum;
  const minuteStr = minuteNum.toString().padStart(2, "0");
  return `${display}:${minuteStr} ${period}`;
}

function describeDow(dow: string): string | null {
  if (dow === "*") return "every day";
  if (dow === "1-5") return "every weekday";
  if (dow === "0,6" || dow === "6,0") return "every weekend";
  // single day
  const single = Number(dow);
  if (Number.isInteger(single) && single >= 0 && single <= 6) {
    return `every ${DAY_NAMES[single]}`;
  }
  // comma-separated list of days
  const list = dow.split(",").map((s) => s.trim());
  if (list.every((d) => /^[0-6]$/.test(d))) {
    const names = list.map((d) => DAY_NAMES[Number(d)]);
    if (names.length === 2) return `every ${names[0]} and ${names[1]}`;
    return `every ${names.slice(0, -1).join(", ")} and ${names[names.length - 1]}`;
  }
  return null;
}

function describeMinuteInterval(minute: string): string | null {
  const m = minute.match(/^\*\/(\d+)$/);
  if (!m) return null;
  const interval = Number(m[1]);
  if (!Number.isInteger(interval) || interval < 1 || interval > 59) return null;
  return `every ${interval} minute${interval === 1 ? "" : "s"}`;
}

export function cronToEnglish(
  cron: string | null,
  timezone?: string | null,
): string | null {
  if (!cron) return null;
  const p = parse(cron);
  if (!p) return null;

  const tz = timezone && timezone !== "UTC" ? ` ${timezone}` : timezone === "UTC" ? " UTC" : "";

  // every N minutes — only valid when nothing else constrains it
  if (p.minute.startsWith("*/") && p.hour === "*" && p.dom === "*" && p.month === "*" && p.dow === "*") {
    return describeMinuteInterval(p.minute);
  }

  // every hour, on the minute X
  if (p.hour === "*" && /^\d+$/.test(p.minute) && p.dom === "*" && p.month === "*" && p.dow === "*") {
    const minuteNum = Number(p.minute);
    if (minuteNum === 0) return "every hour";
    return `every hour at :${minuteNum.toString().padStart(2, "0")}`;
  }

  // Specific hour + minute, daily or per-day-of-week
  if (/^\d+$/.test(p.minute) && /^\d+$/.test(p.hour) && p.dom === "*" && p.month === "*") {
    const time = formatHourMinute(p.hour, p.minute);
    if (!time) return null;
    const cadence = describeDow(p.dow);
    if (!cadence) return null;
    return `${cadence} at ${time}${tz}`;
  }

  return null;
}

// Returns a user-facing line for a workflow's schedule, honoring cron_enabled.
export function describeSchedule(
  cron: string | null,
  timezone: string | null | undefined,
  cronEnabled: boolean,
): string {
  if (!cron) return "Runs when you ask";
  const english = cronToEnglish(cron, timezone);
  const raw = english ?? `${cron}${timezone && timezone !== "UTC" ? ` (${timezone})` : ""}`;
  const label = raw.charAt(0).toUpperCase() + raw.slice(1);
  if (!cronEnabled) return `${label} · Paused`;
  return label;
}
