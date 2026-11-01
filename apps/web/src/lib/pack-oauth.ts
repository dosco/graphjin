import "server-only";
import { createHmac, timingSafeEqual } from "node:crypto";
import { deriveSigningSecret } from "@neko/secret-crypt";
import { cookies } from "next/headers";

const COOKIE = "openneko_pack_oauth_state";
const MAX_AGE = 10 * 60;

export type PackOAuthState = {
  personal?: boolean;
  userId?: string;
  orgId?: string;
  expiresAt?: number;
  packId: string;
  connectionKey: string;
  state: string;
  codeVerifier: string;
  returnPath: string;
};

function secret(): string {
  const value = process.env.OPENNEKO_SESSION_SECRET;
  if (value !== undefined && value.length < 32) throw new Error("OPENNEKO_SESSION_SECRET must be at least 32 characters");
  return value || deriveSigningSecret("session-cookie:v1").toString("base64");
}

export async function writePackOAuthState(value: PackOAuthState): Promise<void> {
  const body = Buffer.from(JSON.stringify({ ...value, expiresAt: Date.now() + MAX_AGE * 1000 }), "utf8").toString("base64url");
  const mac = createHmac("sha256", secret()).update(body).digest("base64url");
  (await cookies()).set(COOKIE, `${body}.${mac}`, {
    httpOnly: true,
    secure: process.env.NODE_ENV === "production",
    sameSite: "lax",
    path: "/",
    maxAge: MAX_AGE,
  });
}

export async function readAndClearPackOAuthState(): Promise<PackOAuthState | null> {
  const jar = await cookies();
  const raw = jar.get(COOKIE)?.value;
  if (!raw) return null;
  jar.delete(COOKIE);
  const split = raw.lastIndexOf(".");
  if (split < 1) return null;
  const body = raw.slice(0, split);
  const actual = Buffer.from(raw.slice(split + 1));
  const expected = Buffer.from(createHmac("sha256", secret()).update(body).digest("base64url"));
  if (actual.length !== expected.length || !timingSafeEqual(actual, expected)) return null;
  try {
    const value = JSON.parse(Buffer.from(body, "base64url").toString("utf8")) as Record<string, unknown>;
    if (["packId", "connectionKey", "state", "codeVerifier", "returnPath"].some((key) => typeof value[key] !== "string" || !value[key])) return null;
    if (typeof value.expiresAt !== "number" || value.expiresAt <= Date.now()) return null;
    if (value.personal && (typeof value.userId !== "string" || typeof value.orgId !== "string")) return null;
    return value as PackOAuthState;
  } catch {
    return null;
  }
}

export function packOAuthCallbackUri(
  request: Pick<Request, "url" | "headers">,
  packId: string,
  connectionKey: string,
): string {
  const override = process.env.OPENNEKO_PUBLIC_URL?.replace(/\/+$/, "");
  const requestUrl = new URL(request.url);
  const forwardedHost = request.headers.get("x-forwarded-host")?.split(",", 1)[0]?.trim();
  const host = forwardedHost || request.headers.get("host") || requestUrl.host;
  const forwardedProto = request.headers.get("x-forwarded-proto")?.split(",", 1)[0]?.trim();
  const protocol = forwardedProto === "http" || forwardedProto === "https"
    ? forwardedProto
    : requestUrl.protocol.replace(/:$/, "");
  const base = override ?? `${protocol}://${host}`;
  return `${base}/api/pack-accounts/${encodeURIComponent(packId)}/${encodeURIComponent(connectionKey)}/callback`;
}
