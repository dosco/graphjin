# Document knowledge eval

Compares two ways for an agent to answer questions from a company's
documents. Both run on the same model with the same instructions and step
limit, and every answer is graded against a value known in advance.

- **concepts**: today's library. The librarian (Docling) converts each file to
  Markdown, the org's primary model distills it into OKF concepts in pieces of
  up to 48,000 characters, concepts are embedded, and the agent searches them
  with `neko_library.search`. `systems/concepts.ts` drives OpenNeko's own code
  for every step.
- **files**: a prototype that keeps each file whole. Ingest writes a plain-text
  copy with page, slide and sheet markers, loads every sheet and CSV into DuckDB
  (a typed table from the detected header row, plus a raw grid by column
  letter), and asks the model for a two-sentence catalog description. The agent
  gets four tools: `list_files`, `search_files`, `read_file` (whole file or a
  page range) and `sql`. Nothing is split or rewritten. See `systems/files.py`.

## Corpora

| Corpus | Built by | Files | Questions |
|---|---|---|---|
| `synthetic` | `generate.py` (seeded) | 9: messy XLSX workbooks, CSV, PDF, DOCX, PPTX, Markdown for a fictional company | 22 |
| `public` | `public.py` | 5 real files from the US Census Bureau and UK government, pinned by URL and SHA-256 | 17 |
| `private` | your own `private/build_questions.py` | files people share for testing; git-ignored | any |

Synthetic answers are computed from the generator's data. Public spreadsheet
answers are computed from the files; public PDF answers cite the page they
were read from.

## Running

```sh
cd evals/documents
uv sync
uv run python generate.py            # corpus/synthetic
uv run python public.py              # corpus/public (downloads and checks hashes)
export GEMINI_API_KEY=...            # EVAL_MODEL defaults to gemini-3.8-flash

# files
uv run python run.py ingest-files corpus/synthetic
uv run python run.py run corpus/synthetic --system files

# concepts: needs the dev stack (pnpm dev:up), `pnpm dev:agent` for the model
# gateway, and the dev org's primary model, which the scratch org copies.
cd ../../apps/worker && . ../../scripts/dev-env.sh
pnpm exec tsx ../../evals/documents/systems/concepts.ts ingest ../../evals/documents/corpus/synthetic doc-eval-synthetic <dev-org-id>
cd ../../evals/documents && uv run python run.py run corpus/synthetic --system concepts
```

Results land in `runs/` as JSON with every answer, tool call, token count and
timing. On its last step the agent is asked to answer from what it found,
for both designs.

## What it doesn't cover

- The product agent also gets the original uploads and a terminal in its
  sandbox, which can route around weak concepts. This eval compares the two
  retrieval designs head to head.
- Scanned documents: the librarian rejects them today, and the files prototype
  has no OCR yet.
