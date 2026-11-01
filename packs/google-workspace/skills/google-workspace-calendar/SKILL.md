---
name: google-workspace-calendar
description: Read calendars and request approved event creation, updates, and deletion through the Google Workspace pack.
license: Apache-2.0
metadata:
  openneko:
    tags: [google-workspace, calendar, events]
    category: productivity
    requires_toolsets: [graphjin, pack-actions]
    related_skills: [google-workspace-read]
---

# Use Google Calendar

List calendars before choosing a calendar ID. Use `primary` only when the user
means the connected account's primary calendar. Read the event before an update
because the update operation replaces the full event.

Use the `google_workspace.calendar` action with `create_event`, `update_event`,
or `delete_event`. Include `calendarId` in `path`. Updates and deletes also need
`eventId`. Use RFC 3339 date-times with an offset, or all-day `date` values.
State the timezone. Include `sendUpdates` in `query` when guests are affected.

The approval intent must name the calendar, event, time, timezone, attendees,
and notification choice. Never delete a recurring series when the user asked
to change one occurrence.
