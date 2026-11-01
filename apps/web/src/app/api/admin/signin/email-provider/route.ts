/**
 * POST /api/admin/signin/email-provider — configure magic-link email
 * delivery in one shot: the chosen provider's API key is written and
 * the other providers' keys are deleted (sign-in requires exactly
 * one), plus the From address. Keys live in the worker's encrypted
 * secrets store; this route never reads a secret back.
 *
 * Body: {
 *   provider: "resend" | "postmark" | "sendgrid",
 *   apiKey?: string,   // omit to keep the already-stored key
 *   from: string,      // e.g. "OpenNeko <signin@company.com>"
 * }
 */

import { NextRequest, NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getAuthGateStatus, setSignInSecrets } from "@/lib/auth";

const MAGIC_LINK = "magic-link";
const PROVIDER_KEYS: Record<string, string> = {
  resend: "RESEND_API_KEY",
  postmark: "POSTMARK_SERVER_TOKEN",
  sendgrid: "SENDGRID_API_KEY",
};

export async function POST(request: NextRequest) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;

  const gate = await getAuthGateStatus();
  const provider = (gate.provider ?? gate.pending)?.pluginName;
  if (provider !== MAGIC_LINK) {
    return NextResponse.json(
      { error: "email-link sign-in is not the chosen sign-in provider" },
      { status: 409 },
    );
  }

  let body: { provider?: unknown; apiKey?: unknown; from?: unknown };
  try {
    body = await request.json();
  } catch {
    return NextResponse.json({ error: "invalid JSON body" }, { status: 400 });
  }

  const emailProvider = typeof body.provider === "string" ? body.provider : "";
  const chosenKey = PROVIDER_KEYS[emailProvider];
  if (!chosenKey) {
    return NextResponse.json(
      { error: "provider must be resend, postmark, or sendgrid" },
      { status: 400 },
    );
  }
  const from = typeof body.from === "string" ? body.from.trim() : "";
  if (!from || from.length > 320) {
    return NextResponse.json(
      { error: "from address is required" },
      { status: 400 },
    );
  }
  const apiKey =
    body.apiKey === undefined || body.apiKey === ""
      ? undefined
      : typeof body.apiKey === "string"
        ? body.apiKey.trim()
        : null;
  if (apiKey === null) {
    return NextResponse.json(
      { error: "apiKey must be a string" },
      { status: 400 },
    );
  }
  const keepingStoredKey =
    apiKey === undefined &&
    (gate.pending?.configuredEnv ?? []).includes(chosenKey);
  if (apiKey === undefined && !keepingStoredKey && !gate.provider) {
    return NextResponse.json(
      { error: `paste the ${emailProvider} API key — none is stored yet` },
      { status: 400 },
    );
  }

  const values: Record<string, string | null> = { MAGIC_LINK_FROM: from };
  if (apiKey !== undefined) values[chosenKey] = apiKey;
  // Exactly-one-provider rule: clear the keys of the two others.
  for (const key of Object.values(PROVIDER_KEYS)) {
    if (key !== chosenKey) values[key] = null;
  }

  try {
    await setSignInSecrets(values);
    return NextResponse.json({ ok: true });
  } catch (err) {
    return NextResponse.json(
      { error: err instanceof Error ? err.message : String(err) },
      { status: 502 },
    );
  }
}
