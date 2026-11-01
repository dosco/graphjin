# Slack solution pack

This pack posts Slack messages and reactions and looks up users and channels
through the Slack Web API, as a GraphJin API source pinned to `slack.com`. It
replaces the action half of the `@open-neko/plugin-slack` plugin: no plugin
code runs inside OpenNeko. Inbound Slack messages and threads belong to the
Slack channel in OpenNeko core, not to this pack.

## Configure

1. Create a Slack app and install it in the workspace. Give the bot token these
   scopes: `chat:write`, `reactions:write`, `users:read`, `users:read.email`,
   `channels:read`, and `groups:read`. Add `im:write` to message users who
   have not opened the app.
2. Open Admin, Settings, Packs and install Slack. Paste the bot token
   (`xoxb-…`). GraphJin seals it in its keystore.
3. Review and apply the configuration.
4. Enable a write policy. Both install disabled:
   - **Slack governed messages** asks an administrator to approve each post
     and reaction.
   - **Slack approved channels** runs posts and reactions without approval in
     the listed channel and user ids.

## Coverage

| Plugin action | Pack equivalent |
|---|---|
| `send_slack_message` | `slack.message`, operation `post_message` |
| `send_slack_dm` | `slack.message`, `post_message` with a user id as `channel` |
| `react_slack_message` | `slack.message`, operation `add_reaction` |
| `lookup_slack_entity` | `slack_lookup_user_by_email`, `slack_get_user`, `slack_list_users`, `slack_get_conversation`, `slack_list_conversations` reads |

The action accepts channel and user ids only, so policies match the real
destination. The plugin resolved names inside the action; the skill now looks
them up with the reads first.

Slack reports errors as `ok: false` with HTTP 200. The pack runtime judges an
action by its HTTP status, so such a call is recorded as succeeded; the skill
tells the agent to check `response.ok`.
