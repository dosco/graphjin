function main(input) {
  const rolls = input.rolls || [];
  const low = rolls.filter((roll) => Number(roll.remaining_kg || 0) <= Number(roll.reorder_point_kg || 0));

  return {
    rolls_checked: rolls.length,
    low_rolls: low.map((roll) => ({
      roll_code: roll.roll_code,
      paper_type: roll.paper_type,
      remaining_kg: Number(roll.remaining_kg || 0),
      supplier: roll.supplier,
    })),
    recommendation:
      low.length > 0
        ? "issue purchase orders for low paper rolls"
        : "paper roll inventory is above reorder points",
  };
}
