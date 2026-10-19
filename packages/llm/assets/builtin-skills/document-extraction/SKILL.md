---
type: Skill
name: document-extraction
description: Extract plain text from any uploaded document — PDF (including scanned pages via OCR), DOCX, PPTX, XLSX, HTML, and unknown or oddly-encoded files. Use whenever you need the text content of a non-plain-text file before summarizing, cataloging, or answering questions about it.
version: 1.0.0
author: OpenNeko
license: Apache-2.0
platforms: [linux, macos]
metadata:
  openneko:
    tags: [documents, pdf, docx, pptx, xlsx, ocr, text-extraction, library]
    category: documents
    requires_toolsets: [terminal]
    related_skills: [pdf, docx, pptx, xlsx]
---

# Document Text Extraction

One command that turns any supported document into plain UTF-8 text on
stdout, degrading gracefully by trying the best available tool first:

```bash
python3 scripts/extract_text.py FILE [--max-chars=N]
```

## What it handles

| Format | Preferred | Fallbacks |
|---|---|---|
| `.pdf` | pypdf | `pdftotext -layout`; then `pdftoppm` + `tesseract` OCR for scanned pages |
| `.docx` | python-docx | stdlib zipfile + XML strip (no deps needed) |
| `.pptx` | python-pptx | stdlib zipfile per-slide XML strip |
| `.xlsx` | openpyxl (values, tab-separated) | stdlib sharedStrings extraction |
| `.md .txt .csv .tsv .json .log` | direct read, utf-8 → utf-8-sig → latin-1 | — |
| `.html .htm .xml` | direct read + tag strip | — |
| anything else | printable-run fallback (like `strings`) | — |

Exit 0 with text on stdout on success; non-zero with a reason on stderr
when a format genuinely can't be handled (e.g. PDF with no extraction
tool installed).

## Usage notes

- **Encoding**: never assume ASCII. The script already tries utf-8 then
  latin-1; for exotic encodings, check first with `file --mime FILE`
  and convert with `iconv -f <charset> -t UTF-8` before extraction.
- **Scanned PDFs**: if normal extraction returns almost nothing, the
  document is likely image-only; the script OCRs the first 10 pages
  automatically when `tesseract` is installed. For longer scans, run
  `pdftoppm -png -r 200 FILE page && tesseract page-N.png -` per page.
- **Spreadsheets**: output is tab-separated values per sheet with a
  `# SheetName` header — suitable for skimming, not for computation.
  Use the `xlsx` skill when you need real spreadsheet operations.
- **Big files**: output is capped (default 400k chars). Raise with
  `--max-chars=` only when you truly need the tail.
- Prefer this script over ad-hoc extraction one-liners so encoding and
  fallback handling stay consistent.
