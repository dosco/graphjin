export type Subscription = {
  id: number;
  customer_id: number;
  cadence_days: number;
  next_ship_date: string;
  bags_per_shipment: number;
  active: boolean;
};

export function shipmentPressure(subscriptions: Subscription[], isoDate: string) {
  const today = new Date(isoDate + "T00:00:00Z");
  return subscriptions
    .filter((sub) => sub.active)
    .map((sub) => {
      const shipDate = new Date(sub.next_ship_date + "T00:00:00Z");
      const daysUntilShip = Math.ceil((shipDate.getTime() - today.getTime()) / 86400000);
      return {
        subscription_id: sub.id,
        customer_id: sub.customer_id,
        days_until_ship: daysUntilShip,
        bags_per_shipment: sub.bags_per_shipment,
        attention: daysUntilShip <= 3 ? "plan_now" : "watch",
      };
    });
}
