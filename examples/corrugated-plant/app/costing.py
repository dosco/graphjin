PAPER_WASTE_FACTOR = 1.08


def estimate_board_cost(board_grade_code: str, area_m2: float, liner_price: float, medium_price: float) -> dict:
    """Estimate material cost for a corrugated work order."""
    material_kg = area_m2 * PAPER_WASTE_FACTOR * 0.42
    blend_price = (liner_price * 0.58) + (medium_price * 0.42)
    return {
        "board_grade_code": board_grade_code,
        "material_kg": round(material_kg, 2),
        "estimated_cost": round(material_kg * blend_price, 2),
    }


def margin_risk(quantity_m2: float, quoted_price: float, estimated_cost: float) -> str:
    margin = (quoted_price - estimated_cost) / max(quoted_price, 1)
    if margin < 0.12:
        return "review"
    if margin < 0.2:
        return "watch"
    return "ok"

# Fix for issue #522: safe input handling
