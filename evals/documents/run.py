"""Run the document eval: the same questions, model and instructions against
each knowledge design, graded against answers known in advance.

  uv run python run.py ingest-files <corpusDir>
  uv run python run.py run <corpusDir> --system files|concepts [--org doc-eval-x] [--only id,id]

Concept runs need scripts/dev-env.sh sourced and the concept library already
ingested with systems/concepts.ts.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import threading
import time
from concurrent.futures import ThreadPoolExecutor
from datetime import date, datetime
from pathlib import Path

import yaml

from gemini import Gemini
from systems.files import FilesTools, ingest

HERE = Path(__file__).parent
REPO = HERE.parent.parent
MODEL = os.environ.get("EVAL_MODEL", "gemini-3.8-flash")
MAX_STEPS = 16

SYSTEM = """You answer questions about the company using its document library, through the tools you have.
Look things up; never guess or use outside knowledge. When documents disagree, use the most recent or the one that says it replaces the other, and say which you used.
Work out exact figures from the source data rather than estimating.
End your reply with one final line in the form:
ANSWER: <the answer only: a number without units or currency, a name, an identifier, a date as YYYY-MM-DD, or a comma-separated list>"""


class ConceptsTools:
    """OpenNeko's neko_library.search, served by systems/concepts.ts."""

    declarations = [{
        "name": "search",
        "description": ("Semantic search over the document library — knowledge distilled from uploaded business documents "
                        "(policies, contracts, SOPs). Returns top-N concepts ranked by similarity, each citing its source "
                        "upload. Prefer this over guessing when the operator asks about company documents, agreements, "
                        "or written policy."),
        "parameters": {"type": "object", "properties": {"query": {"type": "string"}, "limit": {"type": "integer"}},
                       "required": ["query"]},
    }]

    def __init__(self, org: str):
        self.lock = threading.Lock()
        self.proc = subprocess.Popen(
            ["pnpm", "exec", "tsx", "../../evals/documents/systems/concepts.ts", "serve", org],
            cwd=REPO / "apps" / "worker", stdin=subprocess.PIPE, stdout=subprocess.PIPE, text=True, bufsize=1)

    def call(self, name: str, args: dict) -> str:
        with self.lock:
            self.proc.stdin.write(json.dumps({"query": args.get("query", ""), "limit": args.get("limit", 5)}) + "\n")
            self.proc.stdin.flush()
            return self.proc.stdout.readline().strip()

    def close(self):
        self.proc.stdin.close()
        self.proc.wait(timeout=30)


def run_question(gemini: Gemini, tools, q: dict) -> dict:
    contents = [{"role": "user", "parts": [{"text": q["prompt"]}]}]
    usage = {"prompt": 0, "output": 0, "thoughts": 0}
    calls, started, final = [], time.time(), ""
    for step in range(MAX_STEPS):
        last = step == MAX_STEPS - 1
        if last:
            # Same for every design: the tool budget is spent, so answer from what was found.
            contents.append({"role": "user", "parts": [{"text": "You have used your tool budget. Give your final answer now from what you found."}]})
        try:
            out = gemini.generate(contents, system=SYSTEM, tools=None if last else tools.declarations)
        except Exception as e:
            final = f"[model error: {e}]"
            break
        u = out.get("usageMetadata", {})
        usage["prompt"] += u.get("promptTokenCount", 0)
        usage["output"] += u.get("candidatesTokenCount", 0)
        usage["thoughts"] += u.get("thoughtsTokenCount", 0)
        cand = (out.get("candidates") or [{}])[0]
        content = cand.get("content") or {"role": "model", "parts": []}
        contents.append(content)
        fcalls = [p["functionCall"] for p in content.get("parts", []) if "functionCall" in p]
        if not fcalls:
            final = "".join(p.get("text", "") for p in content.get("parts", []) if not p.get("thought"))
            break
        responses = []
        for fc in fcalls:
            result = tools.call(fc["name"], fc.get("args", {}))
            calls.append({"tool": fc["name"], "args": fc.get("args", {}), "result_chars": len(result)})
            responses.append({"functionResponse": {"name": fc["name"], "response": {"result": result}}})
        contents.append({"role": "user", "parts": responses})
    else:
        final = "[ran out of steps]"
    verdict = grade(q, final)
    return {"id": q["id"], "prompt": q["prompt"], "expected": q["answer"], "final": final, **verdict,
            "tool_calls": calls, "steps": len(calls), "tokens": usage, "seconds": round(time.time() - started, 1)}


# ---- grading -------------------------------------------------------------

def answer_line(text: str) -> str:
    m = re.findall(r"(?im)^\s*\**ANSWER:?\**:?\s*(.+)$", text)
    return m[-1].strip() if m else text.strip()


def norm(s: str) -> str:
    return re.sub(r"[^0-9a-z]+", " ", str(s).casefold()).strip()


def numbers(s: str) -> list[float]:
    out = []
    for raw in re.findall(r"-?\d[\d,]*(?:\.\d+)?", s):
        try:
            out.append(float(raw.replace(",", "")))
        except ValueError:
            pass
    return out


def parse_dates(s: str) -> set[str]:
    found = set(re.findall(r"\d{4}-\d{2}-\d{2}", s))
    for fmt in ("%d %B %Y", "%B %d, %Y", "%d %b %Y", "%b %d, %Y", "%d.%m.%Y"):
        for m in re.finditer(r"\d{1,2}\.\d{1,2}\.\d{4}|\d{1,2} [A-Za-z]+ \d{4}|[A-Za-z]+ \d{1,2}, \d{4}", s):
            try:
                found.add(datetime.strptime(m.group(0), fmt).date().isoformat())
            except ValueError:
                pass
    return found


def grade(q: dict, final: str) -> dict:
    line = answer_line(final)
    expected = q["answer"]
    kind = q["kind"]
    if kind == "number":
        tol = float(q.get("tolerance_pct", 0))
        exp = float(expected)
        ok = any(abs(n - exp) <= max(abs(exp) * tol / 100, 0.01 if tol == 0 and not float(exp).is_integer() else 1e-9)
                 for n in numbers(line))
    elif q.get("match") == "date":
        ok = str(expected) in parse_dates(line)
    elif kind == "set":
        got = norm(line)
        missing = [e for e in expected if norm(e) not in got]
        return {"answer": line, "pass": not missing, "missing": missing}
    else:
        ok = norm(expected) in norm(line)
    return {"answer": line, "pass": bool(ok)}


# ---- commands ------------------------------------------------------------

def cmd_ingest_files(corpus: Path) -> None:
    out = HERE / "runs" / "files" / corpus.name
    started = time.time()
    report = ingest(corpus, out, Gemini(MODEL))
    report["seconds"] = round(time.time() - started, 1)
    print(json.dumps(report))


def cmd_run(corpus: Path, system: str, org: str | None, only: list[str] | None, workers: int) -> None:
    qs = yaml.safe_load((corpus / "questions.yaml").read_text())["questions"]
    if only:
        qs = [q for q in qs if q["id"] in only]
    tools = FilesTools(HERE / "runs" / "files" / corpus.name) if system == "files" else ConceptsTools(org or f"doc-eval-{corpus.name}")
    gemini = Gemini(MODEL)
    with ThreadPoolExecutor(max_workers=workers) as pool:
        results = list(pool.map(lambda q: run_question(gemini, tools, q), qs))
    if system == "concepts":
        tools.close()
    passed = sum(r["pass"] for r in results)
    summary = {"corpus": corpus.name, "system": system, "model": MODEL, "questions": len(results), "passed": passed,
               "prompt_tokens": sum(r["tokens"]["prompt"] for r in results),
               "output_tokens": sum(r["tokens"]["output"] + r["tokens"]["thoughts"] for r in results),
               "seconds": round(sum(r["seconds"] for r in results), 1)}
    stamp = datetime.now().strftime("%Y%m%dT%H%M%S")
    out = HERE / "runs" / f"{corpus.name}-{system}-{stamp}.json"
    out.write_text(json.dumps({"summary": summary, "results": results}, indent=1, default=str))
    for r in results:
        print(f"{'PASS' if r['pass'] else 'FAIL'} {r['id']:6} expected={str(r['expected'])[:40]!r:44} got={r['answer'][:60]!r}")
    print(json.dumps(summary))
    print(f"results: {out}")


def main() -> None:
    p = argparse.ArgumentParser()
    sub = p.add_subparsers(dest="cmd", required=True)
    a = sub.add_parser("ingest-files")
    a.add_argument("corpus", type=Path)
    b = sub.add_parser("run")
    b.add_argument("corpus", type=Path)
    b.add_argument("--system", choices=["files", "concepts"], required=True)
    b.add_argument("--org")
    b.add_argument("--only")
    b.add_argument("--workers", type=int, default=3)
    args = p.parse_args()
    if args.cmd == "ingest-files":
        cmd_ingest_files(args.corpus.resolve())
    else:
        cmd_run(args.corpus.resolve(), args.system, args.org, args.only.split(",") if args.only else None, args.workers)


if __name__ == "__main__":
    main()
