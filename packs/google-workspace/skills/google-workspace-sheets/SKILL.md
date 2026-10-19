---
name: google-workspace-sheets
description: Read Google Sheets and request approved value updates, appends, clears, and batch updates through the Google Workspace pack.
license: Apache-2.0
metadata:
  openneko:
    tags: [google-workspace, sheets, spreadsheets]
    category: productivity
    requires_toolsets: [graphjin, pack-actions]
    related_skills: [google-workspace-read]
---

# Use Google Sheets

Read the target ranges before changing values. Preserve row order and empty
cells. Use A1 notation and include the sheet name when the spreadsheet has more
than one sheet.

Use the `google_workspace.sheets` action with `update_values`, `append_values`,
`clear_values`, or `batch_update_values`. Updates and appends need
`valueInputOption` in `query`. Use `RAW` when values must remain literal. Use
`USER_ENTERED` only when Sheets should parse formulas, dates, or numbers.

The approval intent must name the spreadsheet, every range, the number of rows,
and whether formulas are present. Never clear a wider range than the user asked
for.
