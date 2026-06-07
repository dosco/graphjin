export type ProductionOrder = {
  id: number;
  customer_id: number;
  requested_ship_date: string;
  status: string;
  priority: number;
};

export function promiseRisk(order: ProductionOrder, todayISO: string) {
  const today = new Date(todayISO + "T00:00:00Z");
  const due = new Date(order.requested_ship_date + "T00:00:00Z");
  const daysLeft = Math.ceil((due.getTime() - today.getTime()) / 86400000);

  if (order.status === "complete") {
    return "none";
  }
  if (order.priority <= 1 || daysLeft <= 2) {
    return "high";
  }
  if (daysLeft <= 4) {
    return "medium";
  }
  return "low";
}
