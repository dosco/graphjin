---
name: google-workspace-gmail
description: Read Gmail and request approved changes to drafts, messages, and labels through the Google Workspace pack.
license: Apache-2.0
metadata:
  openneko:
    tags: [google-workspace, gmail, email]
    category: productivity
    requires_toolsets: [graphjin, pack-actions]
    related_skills: [google-workspace-read]
---

# Use Gmail

Use GraphJin reads to find messages, threads, drafts, labels, history, and
attachments. Use `userId: me` for the connected account. Search first. Fetch a
full message only after its ID matches the request.

Use the `google_workspace.gmail` action for changes. Supported operations are
`create_draft`, `send_message`, `modify_message_labels`, `create_label`,
`update_label`, and `delete_label`. A draft or sent message body must contain a
base64url-encoded RFC 2822 message in `raw`. Keep `To`, `Cc`, `Bcc`, `Subject`,
and the body in that message. To reply in a thread, also supply `threadId` and
the correct reply headers.

Resolve label names to label IDs before modifying a message. Show recipients,
subject, message purpose, label changes, and target IDs in the approval intent.
Never send a draft unless the user asked to send it.
