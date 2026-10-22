"""Build the public corpus: real documents from their original publishers.

Each file is downloaded from a pinned URL and checked against the SHA-256 of
the copy the questions were written against (retrieved 2026-10-22). Spreadsheet
answers are computed from the files; PDF answers were read from the cited page.
Output: corpus/public/files/* and corpus/public/questions.yaml.
"""

from __future__ import annotations

import hashlib
import urllib.request
from pathlib import Path

import openpyxl
import yaml

ROOT = Path(__file__).parent / "corpus" / "public"
FILES = ROOT / "files"
AGENT = "openneko-document-eval/0.1 (research)"

SOURCES = {
    "census_state_population_estimates_2020_2024.xlsx": (
        "https://www2.census.gov/programs-surveys/popest/tables/2020-2024/state/totals/NST-EST2024-POP.xlsx",
        "4a98cd464d3f9e122e74b47b38f1584e78e9077f4b36be1cabf7a79ab5942852"),
    "census_median_household_income_by_state_h08.xlsx": (
        "https://www2.census.gov/programs-surveys/cps/tables/time-series/historical-income-households/h08.xlsx",
        "5cd4278aafb9852f9527bc03eadc43df03616000adce4799083fc3a48be313b2"),
    "census_income_in_the_us_2022_p60-279.pdf": (
        "https://www.census.gov/content/dam/Census/library/publications/2023/demo/p60-279.pdf",
        "ce921ae1c54b000b3c1defbe52762a5358b9b597a6cd01aee5b84671fab598c5"),
    # The attachment URL now serves version 6.0 (January 2024) of this guidance.
    "uk_traffic_commissioners_travel_subsistence_v6.pdf": (
        "https://assets.publishing.service.gov.uk/government/uploads/system/uploads/attachment_data/file/962750/210216_Advice_on_Travel_Subsistence_Claims__Ver_4.0_.pdf",
        "54d1c8502636db717ec40d973c2e2f29c431a3f6a563dc9f1e60463d220cf789"),
    "uk_hmt_travel_and_subsistence_policy_2013.pdf": (
        "https://assets.publishing.service.gov.uk/government/uploads/system/uploads/attachment_data/file/224100/hmt_group_travel_and_subsistence_policy.pdf",
        "f004d97f9bd23d0f5724f73cd9e583f2520d0f19d0ec60717e0eef4d36673dfd"),
}


def fetch() -> None:
    FILES.mkdir(parents=True, exist_ok=True)
    for name, (url, sha) in SOURCES.items():
        path = FILES / name
        if not path.exists():
            req = urllib.request.Request(url, headers={"User-Agent": AGENT})
            with urllib.request.urlopen(req, timeout=120) as resp:
                path.write_bytes(resp.read())
        got = hashlib.sha256(path.read_bytes()).hexdigest()
        if got != sha:
            raise SystemExit(f"{name}: publisher changed the file (sha256 {got}); the questions were written for {sha}")


def population():
    ws = openpyxl.load_workbook(FILES / "census_state_population_estimates_2020_2024.xlsx", data_only=True).active
    rows = {}
    for r in ws.iter_rows(min_row=5, values_only=True):
        if r[0] and isinstance(r[1], (int, float)):
            rows[str(r[0]).lstrip(".")] = {"base": r[1], 2020: r[2], 2021: r[3], 2022: r[4], 2023: r[5], 2024: r[6]}
    return rows


def income():
    """Both stacked tables of H-8: current dollars, then 2025 dollars."""
    ws = openpyxl.load_workbook(FILES / "census_median_household_income_by_state_h08.xlsx", data_only=True).active
    tables, current, years = {}, None, {}
    for r in ws.iter_rows(values_only=True):
        if r[0] in ("Current Dollars", "2025 Dollars"):
            current = r[0]
            tables[current] = {}
        elif r[0] == "State":
            years = {}
            for j, v in enumerate(r):
                if j and v is not None:
                    years[j] = str(v).split(" ")[0] if str(v).split(" ")[0] not in years.values() else f"{str(v).split(' ')[0]}b"
        elif current and r[0] and isinstance(r[1], (int, float)):
            tables[current][str(r[0]).strip()] = {y: (r[j], r[j + 1]) for j, y in years.items()}
    return tables


def questions():
    pop, inc = population(), income()
    states = {k: v for k, v in pop.items() if k not in ("United States", "Northeast", "Midwest", "South", "West", "District of Columbia", "Puerto Rico")}
    assert len(states) == 50, len(states)
    qs = []

    def add(qid, kind, prompt, answer, files, **extra):
        qs.append({"id": qid, "kind": kind, "prompt": prompt, "answer": answer, "files": files, **extra})

    P, I = ["census_state_population_estimates_2020_2024.xlsx"], ["census_median_household_income_by_state_h08.xlsx"]
    add("pp01", "number", "What was the resident population of Texas on July 1, 2024, according to the Census Bureau estimates?", pop["Texas"][2024], P, tolerance_pct=0)
    grew = max(states, key=lambda s: states[s][2024] - states[s][2023])
    add("pp02", "text", "Which state's population grew the most in absolute numbers between July 1, 2023 and July 1, 2024?", grew, P)
    add("pp03", "number", "What was the population of the South region on July 1, 2024?", pop["South"][2024], P, tolerance_pct=0)
    add("pp04", "number", "How many of the 50 states had more than 10 million residents on July 1, 2024?", sum(1 for v in states.values() if v[2024] > 10_000_000), P, tolerance_pct=0)
    add("pp05", "number", "By how many people did Puerto Rico's population change from the April 1, 2020 estimates base to July 1, 2024? Give a negative number for a decline.",
        pop["Puerto Rico"][2024] - pop["Puerto Rico"]["base"], P, tolerance_pct=0)
    cur, real = inc["Current Dollars"], inc["2025 Dollars"]
    add("pi01", "number", "What was the median household income in Colorado in 2023, in current dollars?", cur["Colorado"]["2023"][0], I, tolerance_pct=0)
    top = max((s for s in cur if s not in ("United States", "District of Columbia")), key=lambda s: cur[s]["2025"][0])
    add("pi02", "text", "Which state had the highest median household income in 2025 in current dollars, not counting the District of Columbia?", top, I)
    add("pi03", "number", "What was US median household income in 1984, expressed in 2025 dollars?", real["United States"]["1984"][0], I, tolerance_pct=0)
    add("pi04", "number", "What is the standard error of Alaska's 2025 median household income estimate?", cur["Alaska"]["2025"][1], I, tolerance_pct=0)
    R = ["census_income_in_the_us_2022_p60-279.pdf"]
    add("pr01", "number", "According to the Census Bureau's Income in the United States: 2022 report, what was real median household income in 2022?", 74580, R, tolerance_pct=0, source="page 7")
    add("pr02", "number", "What was the money income Gini index in 2022, according to that report?", 0.488, R, tolerance_pct=0, source="page 7")
    add("pr03", "number", "By what percentage did real median household income decline from 2021 to 2022, according to that report?", 2.3, R, tolerance_pct=0, source="page 7")
    T, H = ["uk_traffic_commissioners_travel_subsistence_v6.pdf"], ["uk_hmt_travel_and_subsistence_policy_2013.pdf"]
    add("pu01", "number", "Under the Traffic Commissioners' travel and subsistence guidance, what is the green-band hotel rate per night for a London booking, in pounds?", 150, T, tolerance_pct=0, source="page 9")
    add("pu02", "number", "Under HM Treasury's 2013 travel and subsistence policy, what is the overnight accommodation ceiling elsewhere in the UK (outside London), in pounds?", 100, H, tolerance_pct=0, source="page 16")
    add("pu03", "number", "How many pounds higher is the Traffic Commissioners' green-band London hotel rate than HM Treasury's 2013 London accommodation ceiling?", 10, T + H, tolerance_pct=0, source="TC page 9, HMT page 16")
    add("pu04", "number", "Under HM Treasury's 2013 policy, what is the per-head limit for dinner when senior officials extend hospitality to official guests, in pounds?", 35, H, tolerance_pct=0, source="page 20")
    add("pu05", "number", "What mileage rate do the Traffic Commissioners pay for car travel up to 10,000 miles, in pence per mile?", 45, T, tolerance_pct=0, source="page 12")
    return qs


def main() -> None:
    fetch()
    qs = questions()
    (ROOT / "questions.yaml").write_text(yaml.safe_dump({"corpus": "public", "retrieved": "2026-10-22", "questions": qs}, sort_keys=False, allow_unicode=True))
    for q in qs:
        print(q["id"], q["answer"])


if __name__ == "__main__":
    main()
