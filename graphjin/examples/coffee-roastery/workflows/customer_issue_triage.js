function main(input) {
  const ticket = input.ticket || {};
  const body = String(ticket.body || "").toLowerCase();
  const severity = ticket.severity || "normal";

  const needsRoastReview =
    body.includes("extract") ||
    body.includes("roast") ||
    body.includes("bitter") ||
    body.includes("sour");

  return {
    ticket_id: ticket.id,
    route_to: needsRoastReview ? "quality_and_roasting" : "customer_success",
    priority: severity === "high" || needsRoastReview ? "same_day" : "normal",
    suggested_reply_tone: "specific, calm, and operational",
  };
}
