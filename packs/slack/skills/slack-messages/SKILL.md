---
name: slack-messages
description: Post Slack messages and reactions through the Slack pack (slack.message), look up Slack users and channels, and draft messages that survive the approval queue. Use when the operator wants to alert someone, escalate a finding, acknowledge an event, or chase a stalled item in Slack.
license: Apache-2.0
metadata:
  hermes:
    tags: [slack, messaging]
    category: communication
    requires_toolsets: [graphjin, pack-actions]
---

# Slack messages

## Look up ids first

The `slack.message` action takes ids only: channels (`C…`, `G…`), DMs (`D…`),
and users (`U…`, `W…`). It refuses `#channel` names and display names. When
the operator names a person or a channel, resolve the id with a GraphJin read.
Write arguments as literals.

| GraphJin query | Use it to |
|---|---|
| `slack_lookup_user_by_email(email: "amit@acme.com") { ok error user { id name real_name } }` | Find a user by email |
| `slack_get_user(user: "U0123ABCD") { ok error user { id name real_name tz profile { email title } } }` | Read one user |
| `slack_list_users { ok error members { id name real_name deleted } response_metadata { next_cursor } }` | Find a user by name; page with `cursor` |
| `slack_get_conversation(channel: "C0123ABCD") { ok error channel { id name is_private is_member } }` | Read one channel |
| `slack_list_conversations(types: "public_channel,private_channel", exclude_archived: true) { ok error channels { id name is_member } response_metadata { next_cursor } }` | Find a channel by name; page with `cursor` |

The saved query `slack_channels` runs the last one. Slack reports errors in the
body with `ok: false` and an `error` code, so always check `ok`.

## Post or react

```json
{
  "intent": "Alert #ops that Germany revenue dropped.",
  "payload": {
    "operation": "post_message",
    "body": { "channel": "C0123ABCD", "text": "Germany hourly revenue is down 62% against its baseline." }
  }
}
```

- `post_message`: `channel` and `text`, with optional `blocks`, `thread_ts`
  (reply in a thread), `reply_broadcast`, `unfurl_links`, `unfurl_media`, and
  `mrkdwn`. Always give `text`; it is the notification and the fallback for
  `blocks`.
- **Direct message:** post with `channel` set to the user's id (`U…`). Slack
  delivers it to that user's DM with the app.
- `add_reaction`: `channel`, `timestamp` (the message `ts`), and `name`, the
  emoji name without colons, such as `eyes` or `white_check_mark`.

**Check the result.** The action result holds Slack's reply in `response`.
Slack answers HTTP 200 even when it refuses, so the action can succeed while
`response.ok` is `false`. Read `response.ok` and `response.error` every time,
and report a refusal to the operator:

- `not_in_channel` or `channel_not_found`: invite the Slack app to the channel.
- `invalid_auth` or `token_revoked`: an administrator replaces the bot token in
  the pack settings.
- `ratelimited`: wait and try once more; say so if the wait is long.

## Approval

`slack.message` asks an administrator for approval by default. An
administrator can list approved channel and user ids; posts and reactions there
run without approval. The policy reads the real `channel` from the payload.

## Drafting

1. Lead with the conclusion. Operators read Slack on their phones.
2. One thing per message. Split an alert from a follow-up question.
3. No emoji unless the operator's own messages use them.
4. Write dates as ISO dates.
5. Name the source once at the top of a new thread ("From the OpenNeko
   revenue watcher:"), not in replies.
6. Post to a channel when several people need to see it; DM when only one
   person can act.
7. A reaction is a quiet acknowledgement: `white_check_mark` when a tracked
   item completes, `warning` when a finding needs action, `mag` when you start
   to dig.
