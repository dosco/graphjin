function main(input) {
  const lots = input.green_lots || [];
  return lots.map((lot) => {
    const remaining = Number(lot.remaining_kg || 0);
    return {
      green_lot_id: lot.id,
      lot_code: lot.lot_code,
      remaining_kg: remaining,
      reorder: remaining < 800,
      urgency: remaining < 500 ? "urgent" : remaining < 800 ? "soon" : "ok",
    };
  });
}
