---
name: google-workspace-read
description: Read Gmail, Drive, Calendar, Sheets, Docs, and Slides through the Google Workspace pack. Use for mailbox searches, file discovery, calendar review, spreadsheet values, document content, and presentation content.
license: Apache-2.0
metadata:
  openneko:
    tags: [google-workspace, gmail, drive, calendar, sheets, docs, slides]
    category: productivity
    requires_toolsets: [graphjin]
    related_skills: [google-workspace-cross-app-research]
---

# Read Google Workspace

Use only the Google Workspace GraphJin sources installed by this pack. Select
the smallest operation that answers the request.

## Available read operations

- Gmail: profile, message, thread, draft, label, history, and attachment reads.
- Drive: file metadata, permission, and account storage reads.
- Calendar: calendar lists and event lists or details.
- Sheets: spreadsheet metadata plus single-range and batch value reads.
- Docs: `getDocument`.
- Slides: `getPresentation`.

For Gmail, start with a narrow Gmail search query. Fetch complete message or
thread content only for matching results. For Drive, request only the fields
needed for the answer. For Calendar, always state the time window and timezone.
For Sheets, preserve the returned row and column order. Do not infer missing
cells. For Docs and Slides, distinguish extracted text from comments, speaker
notes, suggestions, and other structures that are not present in the response.

The connected Google account defines what is visible. A missing item may mean
that the account lacks access. Do not claim that an item does not exist unless
the relevant search completed without pagination or access errors.

This skill is read-only. Do not send mail, change labels, modify files, create
events, update cells, or edit documents and presentations.
