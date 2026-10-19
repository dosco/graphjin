function main(input) {
  const orders = input.orders || [];
  const runs = input.runs || [];

  const plannedByOrder = {};
  runs.forEach((run) => {
    plannedByOrder[String(run.work_order_id)] = (plannedByOrder[String(run.work_order_id)] || 0) + Number(run.planned_minutes || 0);
  });

  const urgent = orders
    .filter((order) => Number(order.priority || 9) <= 2)
    .map((order) => ({
      order_code: order.order_code,
      customer_id: order.customer_id,
      due_date: order.due_date,
      planned_minutes: plannedByOrder[String(order.id)] || 0,
    }));

  return {
    orders_checked: orders.length,
    urgent_orders: urgent,
    recommendation:
      urgent.length > 0
        ? "sequence priority orders before standard converting jobs"
        : "standard sequence is acceptable",
  };
}
