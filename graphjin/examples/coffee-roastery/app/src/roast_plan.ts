export type RoastScheduleRow = {
  id: number;
  profile_id: number;
  machine_id: string;
  target_output_kg: number;
  status: "planned" | "roasting" | "complete" | "blocked";
};

export type GreenLotRow = {
  id: number;
  lot_code: string;
  remaining_kg: number;
  cupping_score: number;
};

export function reserveGreenCoffee(schedule: RoastScheduleRow, lot: GreenLotRow) {
  const expectedGreenKg = Math.ceil(schedule.target_output_kg / 0.84);
  const canReserve = lot.remaining_kg >= expectedGreenKg;

  return {
    schedule_id: schedule.id,
    green_lot_id: lot.id,
    expected_green_kg: expectedGreenKg,
    can_reserve: canReserve,
    shortage_kg: canReserve ? 0 : expectedGreenKg - lot.remaining_kg,
  };
}

export function roastWindowPriority(schedule: RoastScheduleRow) {
  if (schedule.machine_id === "loring-s35" && schedule.target_output_kg >= 150) {
    return "high";
  }
  if (schedule.status === "blocked") {
    return "blocked";
  }
  return "normal";
}
