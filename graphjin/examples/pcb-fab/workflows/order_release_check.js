function main(input) {
  const orders = input.orders || [];
  const wip = input.wip || [];
  const ncrs = input.ncrs || [];

  const blockersByOrder = {};
  wip.forEach((row) => {
    if (row.blocker) {
      blockersByOrder[row.order_code] = row.blocker;
    }
  });
  ncrs.forEach((ncr) => {
    if (ncr.status === "open" && ncr.severity === "high") {
      blockersByOrder[ncr.order_code || String(ncr.order_id)] = "open high-severity NCR";
    }
  });

  const releaseCandidates = orders
    .filter((order) => order.status !== "engineering_hold" && !blockersByOrder[order.order_code])
    .map((order) => ({ order_code: order.order_code, priority: order.priority }));

  return {
    orders_checked: orders.length,
    blockers_by_order: blockersByOrder,
    release_candidates: releaseCandidates,
    recommendation:
      releaseCandidates.length > 0
        ? `release ${releaseCandidates[0].order_code} next`
        : "do not release new orders until blockers are cleared",
  };
}
