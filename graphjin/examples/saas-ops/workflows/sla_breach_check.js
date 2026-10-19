function main(input) {
  const tickets = input.tickets || [];
  const referenceTime = input.reference_time || "";

  const open = tickets.filter((ticket) => ticket.status === "open");
  const breached = open.filter(
    (ticket) => referenceTime && ticket.sla_due_at < referenceTime
  );
  const breachedIds = breached.map((ticket) => ticket.id);

  return {
    tickets_checked: tickets.length,
    open_tickets: open.length,
    breached_ids: breachedIds,
    recommendation:
      breachedIds.length > 0
        ? "sla breach: tickets " + breachedIds.join(", ") + " are past due — escalate to the on-call lead"
        : "sla ok across open tickets",
  };
}
