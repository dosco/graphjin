---
name: google-workspace-drive
description: Find Drive files and request approved file, folder, move, copy, and sharing changes through the Google Workspace pack.
license: Apache-2.0
metadata:
  openneko:
    tags: [google-workspace, drive, files, sharing]
    category: productivity
    requires_toolsets: [graphjin, pack-actions]
    related_skills: [google-workspace-read]
---

# Use Google Drive

Search files with `listDriveFiles`, then confirm IDs and current parents with
`getDriveFile`. Use permission reads before changing sharing.

Use the `google_workspace.drive` action. `create_file` creates file or folder
metadata. `copy_file` copies an existing file. `update_file` changes metadata;
to move a file, pass the new folder ID as `addParents` and the current folder ID
as `removeParents` in `query`. Permission operations create, update, or delete a
specific permission.

The approval intent must name each file ID, old and new parent for a move, or
the recipient and role for sharing. Avoid `anyone` and domain sharing unless the
user states that scope. This pack does not upload binary content.
