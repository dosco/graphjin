import { createHmac, timingSafeEqual } from "node:crypto";

export function signBody(body: string, secret: string): string {
  return createHmac("sha256", secret).update(body).digest("hex");
}

export function signatureIsValid(
  value: string | undefined,
  body: string,
  secret: string,
): boolean {
  if (!value || !secret) return false;
  const supplied = value.startsWith("sha256=") ? value.slice(7) : value;
  if (!/^[a-f0-9]{64}$/i.test(supplied)) return false;
  const expected = signBody(body, secret);
  return timingSafeEqual(Buffer.from(supplied, "hex"), Buffer.from(expected, "hex"));
}
