/**
 * Sign-in email delivery. Three interchangeable HTTPS providers —
 * Resend, Postmark, SendGrid — selected by which API-key env var is
 * set.
 */

export interface EmailMessage {
  from: string;
  to: string;
  subject: string;
  text: string;
  html: string;
}

export type EmailSender = (message: EmailMessage) => Promise<void>;

export interface ProviderEnv {
  resendApiKey?: string;
  postmarkServerToken?: string;
  sendgridApiKey?: string;
}

export function resolveProvider(env: ProviderEnv): {
  name: "resend" | "postmark" | "sendgrid";
  send: EmailSender;
} {
  const configured = [
    env.resendApiKey && ("resend" as const),
    env.postmarkServerToken && ("postmark" as const),
    env.sendgridApiKey && ("sendgrid" as const),
  ].filter((value): value is "resend" | "postmark" | "sendgrid" =>
    Boolean(value),
  );
  if (configured.length === 0) {
    throw new Error(
      "no email provider configured. Set one of RESEND_API_KEY, POSTMARK_SERVER_TOKEN, or SENDGRID_API_KEY",
    );
  }
  if (configured.length > 1) {
    throw new Error(
      `multiple email providers configured (${configured.join(", ")}). Set exactly one API key`,
    );
  }
  if (env.resendApiKey) {
    return { name: "resend", send: resendSender(env.resendApiKey) };
  }
  if (env.postmarkServerToken) {
    return { name: "postmark", send: postmarkSender(env.postmarkServerToken) };
  }
  return { name: "sendgrid", send: sendgridSender(env.sendgridApiKey!) };
}

async function expectOk(res: Response, provider: string): Promise<void> {
  if (res.ok) return;
  const body = await res.text().catch(() => "");
  // Provider errors can echo the recipient address; truncate and pass
  // through — this surfaces in worker logs only, never to the visitor
  // (the core masks manual-provisioning begin failures).
  throw new Error(
    `${provider} rejected the sign-in email (${res.status}): ${body.slice(0, 300)}`,
  );
}

function resendSender(apiKey: string): EmailSender {
  return async (message) => {
    const res = await fetch("https://api.resend.com/emails", {
      method: "POST",
      headers: {
        Authorization: `Bearer ${apiKey}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        from: message.from,
        to: [message.to],
        subject: message.subject,
        text: message.text,
        html: message.html,
      }),
    });
    await expectOk(res, "Resend");
  };
}

function postmarkSender(serverToken: string): EmailSender {
  return async (message) => {
    const res = await fetch("https://api.postmarkapp.com/email", {
      method: "POST",
      headers: {
        "X-Postmark-Server-Token": serverToken,
        "Content-Type": "application/json",
        Accept: "application/json",
      },
      body: JSON.stringify({
        From: message.from,
        To: message.to,
        Subject: message.subject,
        TextBody: message.text,
        HtmlBody: message.html,
        MessageStream: "outbound",
      }),
    });
    await expectOk(res, "Postmark");
  };
}

function sendgridSender(apiKey: string): EmailSender {
  return async (message) => {
    const res = await fetch("https://api.sendgrid.com/v3/mail/send", {
      method: "POST",
      headers: {
        Authorization: `Bearer ${apiKey}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({
        personalizations: [{ to: [{ email: message.to }] }],
        from: parseFrom(message.from),
        subject: message.subject,
        content: [
          { type: "text/plain", value: message.text },
          { type: "text/html", value: message.html },
        ],
      }),
    });
    await expectOk(res, "SendGrid");
  };
}

/** "Name <addr@x>" → { name, email }; bare address → { email }. */
export function parseFrom(from: string): { email: string; name?: string } {
  const match = /^\s*(.*?)\s*<([^<>\s]+@[^<>\s]+)>\s*$/.exec(from);
  const email = match?.[2];
  if (match && email) {
    const name = (match[1] ?? "").replace(/^"|"$/g, "").trim();
    return name ? { email, name } : { email };
  }
  return { email: from.trim() };
}

export function buildSignInEmail(input: {
  from: string;
  to: string;
  link: string;
  ttlMinutes: number;
}): EmailMessage {
  const { to, link, ttlMinutes } = input;
  return {
    from: input.from,
    to,
    subject: "Your OpenNeko sign-in link",
    text: [
      `Sign in to OpenNeko`,
      ``,
      `Open this link to sign in as ${to}:`,
      ``,
      link,
      ``,
      `The link works once and expires in ${ttlMinutes} minutes. Open it in`,
      `the same browser you requested it from.`,
      ``,
      `Someone asked for a sign-in link for this address. If that was not`,
      `you, ignore this email. Nobody can sign in without the link.`,
      ``,
      `OpenNeko · AI that helps run your business.`,
    ].join("\n"),
    html: signInHtml({ to, link, ttlMinutes }),
  };
}

/**
 * Table layout with inline styles, because Gmail and Outlook drop
 * stylesheets, and system fonts, because they drop web fonts too.
 */
function signInHtml(input: { to: string; link: string; ttlMinutes: number }): string {
  const href = escapeHtml(input.link);
  const address = escapeHtml(input.to);
  const minutes = String(input.ttlMinutes);
  const sans = "-apple-system,BlinkMacSystemFont,'Segoe UI',Roboto,Helvetica,Arial,sans-serif";
  const mono = "ui-monospace,SFMono-Regular,Menlo,Consolas,'Liberation Mono',monospace";
  return `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light">
<meta name="supported-color-schemes" content="light">
<title>Your OpenNeko sign-in link</title>
</head>
<body style="margin:0;padding:0;background:#FAFAF7;">
<div style="display:none;max-height:0;overflow:hidden;opacity:0;">The link works once and expires in ${minutes} minutes.</div>
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="background:#FAFAF7;">
<tr><td align="center" style="padding:32px 16px;">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="max-width:480px;background:#FFFFFF;border:1px solid #EEEBE4;border-radius:14px;">
<tr><td style="padding:32px 32px 8px 32px;font-family:${sans};">
<div style="font-size:15px;font-weight:700;letter-spacing:-0.01em;color:#2D2A24;">OpenNeko</div>
</td></tr>
<tr><td style="padding:16px 32px 0 32px;font-family:${sans};">
<h1 style="margin:0;font-size:22px;line-height:1.3;font-weight:700;letter-spacing:-0.02em;color:#2D2A24;">Sign in to OpenNeko</h1>
<p style="margin:12px 0 0 0;font-size:15px;line-height:1.55;color:#6F6A60;">Select the button to sign in as <span style="color:#2D2A24;font-weight:600;">${address}</span>.</p>
</td></tr>
<tr><td style="padding:24px 32px 0 32px;">
<table role="presentation" cellpadding="0" cellspacing="0" border="0"><tr>
<td align="center" bgcolor="#6B5CE7" style="border-radius:10px;">
<a href="${href}" style="display:inline-block;padding:14px 28px;font-family:${sans};font-size:15px;font-weight:600;color:#FFFFFF;text-decoration:none;border-radius:10px;">Sign in</a>
</td></tr></table>
<p style="margin:16px 0 0 0;font-family:${sans};font-size:13px;line-height:1.55;color:#6F6A60;">The link works once and expires in ${minutes} minutes. Open it in the same browser you requested it from.</p>
</td></tr>
<tr><td style="padding:24px 32px 0 32px;font-family:${sans};">
<p style="margin:0 0 8px 0;font-size:13px;color:#6F6A60;">Or paste this link into your browser:</p>
<div style="padding:12px 14px;background:#F7F4ED;border:1px solid #EEEBE4;border-radius:10px;font-family:${mono};font-size:12px;line-height:1.6;color:#2D2A24;word-break:break-all;">${href}</div>
</td></tr>
<tr><td style="padding:24px 32px 32px 32px;font-family:${sans};">
<div style="height:1px;background:#EEEBE4;font-size:0;line-height:0;">&nbsp;</div>
<p style="margin:20px 0 0 0;font-size:12px;line-height:1.6;color:#756F65;">Someone asked for a sign-in link for this address. If that was not you, ignore this email. Nobody can sign in without the link.</p>
<p style="margin:12px 0 0 0;font-size:12px;line-height:1.6;color:#756F65;">OpenNeko · AI that helps run your business.</p>
</td></tr>
</table>
</td></tr>
</table>
</body>
</html>`;
}

function escapeHtml(value: string): string {
  return value
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;");
}
