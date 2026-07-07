function main(input) {
  const waitlist = input.waitlist || [];
  const cancellations = input.cancellations || [];

  const highPriority = waitlist.filter(
    (entry) => entry.status === "waiting" && entry.priority === "high"
  );
  const shouldPromote = highPriority.length > 0 && cancellations.length > 0;

  return {
    waiting_high_priority: highPriority.length,
    open_slots: cancellations.length,
    recommendation: shouldPromote
      ? "promote " + highPriority.length + " high-priority waitlist entries into open slots"
      : "no promotion needed",
  };
}
