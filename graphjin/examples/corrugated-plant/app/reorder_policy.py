def needs_reorder(roll: dict) -> bool:
    return float(roll.get("remaining_kg", 0)) <= float(roll.get("reorder_point_kg", 0))


def reorder_quantity(roll: dict, eight_week_demand_kg: float) -> float:
    buffer = eight_week_demand_kg * 0.18
    target = max(float(roll.get("reorder_point_kg", 0)) * 2.5, eight_week_demand_kg + buffer)
    return round(max(target - float(roll.get("remaining_kg", 0)), 0), 2)


def supplier_message(roll: dict, quantity_kg: float) -> str:
    return f"Order {quantity_kg}kg of {roll['paper_type']} from {roll['supplier']} for roll {roll['roll_code']}."
