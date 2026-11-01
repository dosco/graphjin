# Library files

The Library holds each user's uploaded documents and spreadsheets. OpenNeko
stores them; the customer GraphJin reads them.

## Accepted inputs

PDF (with a text layer), DOCX, PPTX, XLSX, CSV, Markdown and plain text
(`formats.ts`). Scanned documents are stored, but only their file name is
searchable until OCR exists.

## Storage

`source-files.ts` writes an upload to the customer GraphJin's `neko_library`
file source, beside its config:

```text
<GraphJin config dir>/files/library/users/<user id>/<sha256>/<filename>
```

`library_document.relative_path` stores `neko_library/<key>`. The upload is
content-addressed, so a later upload with the same name never overwrites an
earlier one. Removing a document deletes its row and its folder.

## Reading

The GraphJin config gains the source at boot (`patchGraphjinLibrary` in
`graphjin/sources-config.ts`) with `user_prefix: users/`, which keeps each
user's folder private to them, and `file_content` on, which makes GraphJin:

- search the files by content and return passages with their page, slide,
  sheet or section;
- return a file's text, PDF and Office files included;
- convert sheets and CSV files to tables and answer SQL over them with
  `query_file_tables`.

The agent's `mcp_neko_library_search` asks GraphJin as the run's user. Reading
a whole file or computing from a sheet goes through the GraphJin data tools.
Nothing is extracted, chunked or summarised on the OpenNeko side.
