function main(input) {
  const orders = input.orders || [];
  const schedule = input.schedule || [];
  const subscriptions = input.subscriptions || [];

  const committedKg = orders.reduce((sum, order) => {
    return sum + (order.quantity_bags * order.bag_size_g) / 1000;
  }, 0);

  const scheduledKg = schedule.reduce((sum, item) => sum + Number(item.target_output_kg || 0), 0);
  const subscriptionBags = subscriptions.reduce((sum, sub) => sum + Number(sub.bags_per_shipment || 0), 0);

  return {
    committed_kg: Number(committedKg.toFixed(2)),
    scheduled_kg: Number(scheduledKg.toFixed(2)),
    subscription_bags_due: subscriptionBags,
    plan_status: scheduledKg >= committedKg ? "covered" : "short",
    next_action: scheduledKg >= committedKg ? "confirm packaging labor" : "add roast capacity",
  };
}
