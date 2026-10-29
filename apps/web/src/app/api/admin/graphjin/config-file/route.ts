import { NextResponse } from "next/server";
import {
  applyEditedGraphjinConfig,
  checkEditedGraphjinConfig,
  graphjinAdminConnection,
  GraphjinConfigEditorError,
  readEditableGraphjinConfig,
} from "@neko/llm/graphjin/config-editor";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

const MAX_CONFIG_BYTES = 1024 * 1024;

function configFile(): string | null {
  return process.env.OPENNEKO_GRAPHJIN_CONFIG?.trim() || null;
}

function failure(error: unknown): NextResponse {
  if (error instanceof GraphjinConfigEditorError) {
    return NextResponse.json({ error: error.message }, { status: error.status });
  }
  const message = error instanceof Error ? error.message : String(error);
  console.error("[graphjin-config-file]", message);
  return NextResponse.json({ error: "GraphJin could not be reached. Try again in a moment." }, { status: 502 });
}

/** The GraphJin config file with its secrets hidden. Admins only. */
export async function GET() {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const file = configFile();
  if (!file) return NextResponse.json({ error: "This deployment has no GraphJin config file." }, { status: 404 });
  try {
    return NextResponse.json(await readEditableGraphjinConfig(file));
  } catch (error) {
    return failure(error);
  }
}

/**
 * Checks ("check") or applies ("apply") an edited config. Apply changes the
 * running GraphJin live and then writes the file.
 */
export async function POST(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const file = configFile();
  if (!file) return NextResponse.json({ error: "This deployment has no GraphJin config file." }, { status: 404 });
  const body = await request.json().catch(() => null);
  const action = body?.action;
  if ((action !== "check" && action !== "apply") || typeof body?.text !== "string" || typeof body?.baseHash !== "string") {
    return NextResponse.json({ error: "Send action (check or apply), text and baseHash." }, { status: 400 });
  }
  if (Buffer.byteLength(body.text) > MAX_CONFIG_BYTES) {
    return NextResponse.json({ error: "The config is larger than 1 MB." }, { status: 413 });
  }
  try {
    const connection = await graphjinAdminConnection(await getOrgId(), actor.userId ?? "graphjin-config-editor");
    if (!connection) return NextResponse.json({ error: "No GraphJin data source is connected." }, { status: 409 });
    const input = { configFile: file, edited: body.text, baseHash: body.baseHash, connection };
    if (action === "check") return NextResponse.json(await checkEditedGraphjinConfig(input));
    const { hash } = await applyEditedGraphjinConfig(input);
    return NextResponse.json({ applied: true, ...(await readEditableGraphjinConfig(file)), hash });
  } catch (error) {
    return failure(error);
  }
}
