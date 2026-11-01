# Resend solution pack

This pack sends operational email through the Resend REST API, as a GraphJin
API source. It replaces the `@open-neko/plugin-resend` plugin: no plugin code
runs inside OpenNeko.

## Configure

1. In Resend, verify the sending domain and create an API key with full access.
   The install check lists templates, which a sending-only key cannot do.
2. Open Admin, Settings, Packs and install Resend.
3. Set `resend.from` to the sender, for example `Acme Ops <ops@mail.acme.com>`,
   and paste the API key. GraphJin seals the key in its keystore.
4. Review and apply the configuration.
5. Enable a write policy. Both install disabled:
   - **Resend governed send** asks an administrator to approve each email.
   - **Resend approved recipient domains** sends without approval when every
     recipient's domain is on its list. Add domains such as `acme.com` or
     `*.acme.com` first.

## What the pack enforces

- **Fixed sender.** `adapter.lockedBody` sets `from` from `resend.from` and
  replaces any value in the payload.
- **Recipient domains.** `targets` reads `body.to`, `body.cc`, and `body.bcc`
  as email domains, so policies match the real recipients, not the agent's
  description.
- **One send per request.** The runtime sends the action request id as
  Resend's `Idempotency-Key`, so a retry cannot send twice.
- **Reviewed shape.** The OpenAPI spec allows one address per list item, up to
  50 per list, and exactly one body: a template, or a subject with HTML and/or
  text. GraphJin refuses any other field before calling Resend.
- **Pinned host.** The source calls `https://api.resend.com` only.

## Coverage

| Plugin action | Pack equivalent |
|---|---|
| `send_email` | `resend.email` action, operation `send_email` |
| `list_email_templates` | `resend_list_templates` read and the `resend_email_templates` saved query |
| `get_email_template` | `resend_get_template` read |
| `get_email_status` | `resend_get_email` read |

The plugin rendered Markdown into a house HTML layout in code. The
`resend-email` skill now carries that layout as a reference, and the agent
writes the HTML. Logo, brand color, and footer live in each workflow's
"Email layout" block instead of plugin settings.
