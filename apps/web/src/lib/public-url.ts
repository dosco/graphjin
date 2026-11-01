import { NextResponse } from "next/server";

/**
 * Base URL for links that leave the app (emailed sign-in links, OAuth
 * callbacks). Set OPENNEKO_PUBLIC_URL; the Host header is not trusted,
 * and inside the container request.url carries the listen address.
 */
export function publicBaseUrl(requestUrl: string): string {
  const configured = process.env.OPENNEKO_PUBLIC_URL?.trim().replace(/\/+$/, "");
  return configured || new URL(requestUrl).origin;
}

/** Redirect within the app with a relative Location, so the browser keeps its own origin. */
export function appRedirect(path: string, status = 302): NextResponse {
  if (!path.startsWith("/") || path.startsWith("//")) path = "/";
  return new NextResponse(null, { status, headers: { Location: path } });
}
