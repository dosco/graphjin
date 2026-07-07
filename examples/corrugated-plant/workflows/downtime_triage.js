function main(input) {
  const events = input.downtime_events || [];
  const holds = input.quality_holds || [];

  const severeEvents = events.filter((event) => event.status !== "closed" && event.severity !== "normal");
  const openHolds = holds.filter((hold) => hold.status !== "released");

  return {
    downtime_events_checked: events.length,
    quality_holds_checked: holds.length,
    severe_open_downtime: severeEvents.length,
    open_quality_holds: openHolds.length,
    recommendation:
      severeEvents.length > 0 || openHolds.length > 0
        ? "dispatch maintenance and QA before releasing the next run"
        : "no elevated downtime or quality holds",
  };
}
