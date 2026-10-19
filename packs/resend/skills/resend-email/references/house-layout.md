# House email layout

One table-based layout that holds up in Gmail, Outlook, and Apple Mail. Fill
the four placeholders, then put the content where `{{CONTENT}}` is.

- `{{SUBJECT}}`: the email subject, HTML-escaped.
- `{{BRAND}}`: a hex color such as `#1f6feb`. Use `#2563eb` when none is set.
- `{{LOGO_ROW}}`: empty, or the logo row below with an HTTPS image URL.
- `{{FOOTER_ROW}}`: empty, or the footer row below with one escaped line.

```html
<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="color-scheme" content="light">
<title>{{SUBJECT}}</title>
</head>
<body style="margin:0;padding:0;background:#f3f4f6">
<table role="presentation" width="100%" cellpadding="0" cellspacing="0" style="background:#f3f4f6">
<tr><td align="center" style="padding:24px 12px">
<table role="presentation" width="600" cellpadding="0" cellspacing="0" style="width:100%;max-width:600px;background:#ffffff;border-top:4px solid {{BRAND}};border-radius:6px;font-family:-apple-system, BlinkMacSystemFont, 'Segoe UI', Helvetica, Arial, sans-serif">
{{LOGO_ROW}}<tr><td style="padding:24px 32px 12px">{{CONTENT}}</td></tr>
{{FOOTER_ROW}}</table>
</td></tr>
</table>
</body>
</html>
```

Logo row:

```html
<tr><td style="padding:24px 32px 0"><img src="LOGO_URL" alt="" height="32" style="height:32px;width:auto;border:0;display:block"></td></tr>
```

Footer row:

```html
<tr><td style="padding:16px 32px 24px;border-top:1px solid #d1d9e0;font-size:12px;line-height:1.5;color:#59636e">FOOTER_TEXT</td></tr>
```

## Content elements

Write every element with inline styles; mail clients drop `<style>` blocks.

| Element | Markup |
|---|---|
| Heading | `<h2 style="margin:24px 0 8px;font-size:18px;line-height:1.3;color:#1f2328">…</h2>` |
| Paragraph | `<p style="margin:0 0 14px;font-size:15px;line-height:1.55;color:#1f2328">…</p>` |
| Link | `<a href="https://…" style="color:{{BRAND}}">…</a>` |
| List | `<ul style="margin:0 0 14px;padding-left:22px;font-size:15px;line-height:1.55;color:#1f2328"><li style="margin:0 0 6px">…</li></ul>` |
| Table | `<table role="presentation" cellpadding="0" cellspacing="0" style="border-collapse:collapse;margin:0 0 14px;font-size:14px;color:#1f2328">` |
| Header cell | `<th style="border:1px solid #d1d9e0;padding:6px 10px;text-align:left;vertical-align:top;background:#f6f8fa">…</th>` |
| Cell | `<td style="border:1px solid #d1d9e0;padding:6px 10px;text-align:left;vertical-align:top">…</td>` |
| Code | `<code style="font-family:Menlo,Consolas,monospace;font-size:13px;background:#f6f8fa;padding:1px 4px;border-radius:4px">…</code>` |

## Safety rules

- Escape every value that comes from data: `&`, `<`, `>`, `"`, and `'`.
- Links use `https:`, `http:`, or `mailto:` only. Images use `https:` only.
- No scripts, forms, iframes, or remote CSS.
- The `text` body carries the same content as plain text, with the footer
  after a `--` line.
