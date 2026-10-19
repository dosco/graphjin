---
name: resend-email
description: Send operational email through the Resend pack (resend.email) and design an email with the operator in plain language before a workflow sends it. Use when the operator asks to email someone, to set up an email alert or report, to change how an email looks, or to check whether an email arrived.
license: Apache-2.0
metadata:
  hermes:
    tags: [resend, email]
    category: communication
    requires_toolsets: [graphjin, pack-actions]
---

# Operational email with Resend

Every email comes from one fixed sender, the pack's `resend.from` setting.
OpenNeko sets `from` itself and replaces any value you send, so leave it out.
You choose the recipients, the subject, and one body.

## Send an email

Request the `resend.email` action with `operation: send_email`. Put every email
field inside `body`. Keep `intent` beside the payload; it explains the request
for approval and is not an email field.

```json
{
  "intent": "Send the scheduled inventory summary to the operations team.",
  "payload": {
    "operation": "send_email",
    "body": {
      "to": ["ops@acme.com"],
      "subject": "Low stock: 3 SKUs below reorder point",
      "html": "<!doctype html>…",
      "text": "Low stock\n\n3 SKUs are below their reorder point…"
    }
  }
}
```

- `to`, `cc`, `bcc`, and `reply_to` are lists. Each item is one address,
  `ops@acme.com` or `Acme Ops <ops@acme.com>`. Do not join addresses with
  commas. Each list takes up to 50 addresses.
- Give exactly one body: a `template`, or a `subject` with `html`, `text`, or
  both.
- Do not set an idempotency key. OpenNeko sends the action request id as
  Resend's `Idempotency-Key`, so a retry cannot send twice.

## Pick one body

1. **HTML in the house layout** (the default). Write the content, then wrap it
   in the layout in [references/house-layout.md](references/house-layout.md).
   Always send `text` too: the same content as plain text. Use this for alerts
   and reports whose rows change from run to run.
2. **`template`**. Use it when the operator names a Resend template, or when
   the template list shows one that fits. Read the template first, then pass
   every variable key in `template.variables`. The template sets the subject
   and the look, so you fill values only. A template variable holds one value,
   so a template cannot repeat a row for each item.
3. **`text` only**. Use it for a short note to one person.

Put the most important fact in the first line. A reader sees it in the inbox
preview.

## Reads

| GraphJin query | Use it to |
|---|---|
| `resend_list_templates { id alias name status }` | Find a template the team designed in Resend |
| `resend_get_template(template_id: "daily-ops-summary") { subject variables { key type fallback_value } }` | Read a template's subject and variable keys |
| `resend_get_email(email_id: "…") { last_event to subject created_at }` | Check if an email was delivered, bounced, or opened |

Write the arguments as literals; GraphJin does not take variables for them. The
saved query `resend_email_templates` lists templates.

## Design an email with the operator

Operators describe an email in plain words, for example "email me low stock
every 6 hours". They never edit HTML. Design the email with them in the chat,
then save the design in the workflow.

1. **Get real data first.** Run the query the email will use, so your
   questions and the preview use real rows.
2. **Ask about the display.** Ask one to three questions, each with two to
   four concrete options. Ask only what the request leaves open: layout (a
   table, a short list, one summary line), which columns and their order, the
   sort order, what goes first, what to do on an empty result, and a subject
   pattern that carries a number. Ask for the logo URL, brand color, and footer
   line the first time; the house layout uses them.
3. **Show a preview** in the chat with the real data. Ask if it is right.
4. **Offer a test send** to the operator's own address.
5. **Save the design in the workflow.** In the step that sends the email,
   write an "Email layout" block: recipients, the subject pattern, the content
   skeleton with columns and sort order, the empty-result rule, and the logo,
   brand color, and footer. Each run fills the skeleton with new data.

```
Email layout
- To: ops@acme.com; cc: buyer@acme.com
- Subject: "Low stock: {count} SKUs below reorder point"
- If no SKU is below its reorder point: send no email.
- House layout: logo https://acme.com/logo.png, color #1f6feb,
  footer "Sent by OpenNeko for Acme Operations."
- Content: one sentence with how many SKUs, and how many are at zero.
  Table: SKU | Product | On hand | Reorder point | Days of cover.
  Sort: on hand ascending. Show at most 25 rows; say how many more.
  Last line: link to the inventory page.
```

## Recipients and approval

- `resend.email` asks an administrator for approval by default.
- An administrator can list approved recipient domains, such as `acme.com`
  or `*.acme.com`. When every recipient's domain is on that list, the email
  sends without approval. OpenNeko reads the domains from `to`, `cc`, and
  `bcc` in the payload, so the target text you write does not change the
  decision. A scheduled workflow that emails anyone else waits for approval.
- A policy can also refuse some domains outright. If a recipient is refused,
  tell the operator which domain needs an administrator. Do not retry with
  other addresses.

## After sending

The action result holds Resend's response, whose `id` is the email id. When
the operator asks whether an email arrived, read its `last_event` with
`resend_get_email`. Report it in plain words: delivered, bounced, complained,
or still queued.
