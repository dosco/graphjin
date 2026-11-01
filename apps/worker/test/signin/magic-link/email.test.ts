import { afterEach, describe, expect, it, vi } from "vitest";
import { buildSignInEmail, parseFrom, resolveProvider } from "../../../src/signin/magic-link/email.js";

describe("resolveProvider", () => {
  it("requires exactly one configured provider", () => {
    expect(() => resolveProvider({})).toThrow(/no email provider/);
    expect(() =>
      resolveProvider({ resendApiKey: "a", sendgridApiKey: "b" }),
    ).toThrow(/multiple email providers/);
    expect(resolveProvider({ resendApiKey: "a" }).name).toBe("resend");
    expect(resolveProvider({ postmarkServerToken: "a" }).name).toBe("postmark");
    expect(resolveProvider({ sendgridApiKey: "a" }).name).toBe("sendgrid");
  });
});

describe("provider senders", () => {
  afterEach(() => {
    vi.unstubAllGlobals();
  });

  const message = buildSignInEmail({
    from: "OpenNeko <signin@company.com>",
    to: "person@company.com",
    link: "https://neko.example/api/auth/callback?code=t&state=s",
    ttlMinutes: 10,
  });

  it("posts to the provider API and passes on success", async () => {
    const fetchMock = vi.fn(async () => new Response("{}", { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    await resolveProvider({ resendApiKey: "key" }).send(message);
    expect(fetchMock).toHaveBeenCalledWith(
      "https://api.resend.com/emails",
      expect.objectContaining({ method: "POST" }),
    );
  });

  it("shapes the Resend payload with bearer auth", async () => {
    const fetchMock = vi.fn(async () => new Response("{}", { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    await resolveProvider({ resendApiKey: "rk_123" }).send(message);
    const [, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect((init.headers as Record<string, string>).Authorization).toBe(
      "Bearer rk_123",
    );
    const body = JSON.parse(init.body as string);
    expect(body).toMatchObject({
      from: message.from,
      to: [message.to],
      subject: message.subject,
    });
  });

  it("shapes the Postmark payload with the server-token header", async () => {
    const fetchMock = vi.fn(async () => new Response("{}", { status: 200 }));
    vi.stubGlobal("fetch", fetchMock);
    await resolveProvider({ postmarkServerToken: "pm_123" }).send(message);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("https://api.postmarkapp.com/email");
    expect(
      (init.headers as Record<string, string>)["X-Postmark-Server-Token"],
    ).toBe("pm_123");
    const body = JSON.parse(init.body as string);
    expect(body).toMatchObject({
      From: message.from,
      To: message.to,
      Subject: message.subject,
      MessageStream: "outbound",
    });
  });

  it("shapes the SendGrid payload with parsed from and both content types", async () => {
    const fetchMock = vi.fn(async () => new Response("", { status: 202 }));
    vi.stubGlobal("fetch", fetchMock);
    await resolveProvider({ sendgridApiKey: "sg_123" }).send(message);
    const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
    expect(url).toBe("https://api.sendgrid.com/v3/mail/send");
    const body = JSON.parse(init.body as string);
    expect(body.from).toEqual({ email: "signin@company.com", name: "OpenNeko" });
    expect(body.personalizations).toEqual([
      { to: [{ email: message.to }] },
    ]);
    expect(body.content.map((c: { type: string }) => c.type)).toEqual([
      "text/plain",
      "text/html",
    ]);
  });

  it("surfaces provider rejections with status and truncated body", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response("sender not verified", { status: 422 })),
    );
    await expect(
      resolveProvider({ postmarkServerToken: "key" }).send(message),
    ).rejects.toThrow(/Postmark rejected the sign-in email \(422\)/);
  });
});

describe("parseFrom", () => {
  it("splits display-name form and passes bare addresses through", () => {
    expect(parseFrom("OpenNeko <signin@company.com>")).toEqual({
      email: "signin@company.com",
      name: "OpenNeko",
    });
    expect(parseFrom("signin@company.com")).toEqual({
      email: "signin@company.com",
    });
  });
});

describe("buildSignInEmail", () => {
  it("includes the link in text and HTML-escaped form", () => {
    const message = buildSignInEmail({
      from: "a@b.co",
      to: "person@company.com",
      link: "https://neko.example/cb?code=a&state=b",
      ttlMinutes: 10,
    });
    expect(message.text).toContain("https://neko.example/cb?code=a&state=b");
    expect(message.html).toContain("https://neko.example/cb?code=a&amp;state=b");
    expect(message.text).toContain("works once");
    expect(message.text).toContain("same browser");
  });

  it("builds an HTML email that names the recipient and links the button", () => {
    const message = buildSignInEmail({
      from: "OpenNeko <signin@company.com>",
      to: "person@company.com",
      link: "https://neko.example/cb?code=a&state=b",
      ttlMinutes: 10,
    });
    expect(message.html).toContain("person@company.com");
    expect(message.html).toContain(
      '<a href="https://neko.example/cb?code=a&amp;state=b"',
    );
    expect(message.html).toContain("expires in 10 minutes");
    // Gmail and Outlook drop stylesheets, so every style stays inline.
    expect(message.html).not.toContain("<style");
    expect(message.html).not.toContain("<link");
  });

  it("escapes a recipient address that carries markup", () => {
    const message = buildSignInEmail({
      from: "a@b.co",
      to: '"<script>x</script>"@company.com',
      link: "https://neko.example/cb",
      ttlMinutes: 10,
    });
    expect(message.html).not.toContain("<script>");
    expect(message.html).toContain("&lt;script&gt;");
  });
});
