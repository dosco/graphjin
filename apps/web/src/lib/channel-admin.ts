import { NextResponse } from "next/server";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";

export type ChannelSettingField = {
  key: string;
  required: boolean;
  secret: boolean;
  description: string;
  set: boolean;
  value?: string;
};

/** One built-in channel (Slack, Telegram, HUD) and its stored settings. */
export type ChannelSettings = {
  id: string;
  providerLabel: string;
  directions: string[];
  ingress: string;
  /** An installed pack lists the channel, so the worker delivers and listens on it. */
  enabled: boolean;
  fields: ChannelSettingField[];
  missing: string[];
};

function workerAdminBase(): string {
  return (process.env.WORKER_ADMIN_URL ?? "http://127.0.0.1:4100").replace(/\/+$/, "");
}

/** Proxies the worker's channel settings route. Secret values go to the worker and never come back. */
export async function proxyChannelSettings(request?: Request): Promise<NextResponse> {
  const allowed = await requireAdminActor();
  if (isDenied(allowed)) return allowed;
  try {
    const response = await fetch(`${workerAdminBase()}/admin/channels/settings`, {
      ...(request
        ? { method: "POST", headers: { "Content-Type": "application/json" }, body: await request.text() }
        : {}),
      cache: "no-store",
      signal: AbortSignal.timeout(30_000),
    });
    const body = await response.json().catch(() => ({ error: `Worker returned HTTP ${response.status}` }));
    return NextResponse.json(body, { status: response.status });
  } catch (error) {
    return NextResponse.json({ error: error instanceof Error ? error.message : String(error) }, { status: 502 });
  }
}
