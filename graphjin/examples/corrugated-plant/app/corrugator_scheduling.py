def priority_score(work_order: dict, customer: dict, downtime_minutes: int = 0) -> int:
    """Lower scores run earlier on the corrugator."""
    score = int(work_order.get("priority", 3)) * 10
    if customer.get("priority_tier") == "strategic":
        score -= 8
    if work_order.get("status") == "at_risk":
        score -= 5
    if downtime_minutes > 30:
        score += 4
    return score


def choose_machine(board_grade: dict, machines: list[dict]) -> str:
    flute = board_grade.get("flute")
    ready = [m for m in machines if m.get("status") == "ready"]
    if flute == "E":
        compact = [m for m in ready if "mini" in m.get("name", "").lower()]
        if compact:
            return compact[0]["machine_code"]
    return ready[0]["machine_code"] if ready else "manual_review"
