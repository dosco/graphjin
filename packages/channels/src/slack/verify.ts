import { createHmac, timingSafeEqual } from "node:crypto";

/** Reject requests stamped more than 5 minutes from now (replay guard). */
const MAX_SKEW_SECONDS = 60 * 5;

/**
 * Slack request signing: hex HMAC-SHA256 of `v0:<timestamp>:<rawBody>`
 * with the app's signing secret, compared (constant-time) to the
 * X-Slack-Signature header. https://api.slack.com/authentication/verifying-requests-from-slack
 */
export function verifySlackSignature(
  signingSecret: string,
  headers: Record<string, string>,
  body: string,
  nowSeconds: number = Math.floor(Date.now() / 1000),
): boolean {
  const lower: Record<string, string> = {};
  for (const [k, v] of Object.entries(headers)) lower[k.toLowerCase()] = v;
  const timestamp = lower["x-slack-request-timestamp"];
  const signature = lower["x-slack-signature"];
  if (!timestamp || !signature) return false;
  const ts = Number(timestamp);
  if (!Number.isFinite(ts) || Math.abs(nowSeconds - ts) > MAX_SKEW_SECONDS) {
    return false;
  }
  const expected = `v0=${createHmac("sha256", signingSecret)
    .update(`v0:${timestamp}:${body}`)
    .digest("hex")}`;
  const a = Buffer.from(expected);
  const b = Buffer.from(signature);
  if (a.length !== b.length) return false;
  return timingSafeEqual(a, b);
}
