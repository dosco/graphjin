import { NextResponse } from "next/server";
import type { GraphjinAgentTurn } from "@neko/llm/graphjin";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { askGraphjinAdminAgent } from "@/lib/graphjin-admin";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";
export const maxDuration = 200;

const MAX_INSTRUCTION_LENGTH = 8_000;
const MAX_HISTORY_TURNS = 10;

/** Workbench: one question to GraphJin's built-in agent, asked as admin. */
export async function POST(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const body = await request.json().catch(() => null);
  const instruction = typeof body?.instruction === "string" ? body.instruction.trim() : "";
  if (!instruction || instruction.length > MAX_INSTRUCTION_LENGTH) {
    return NextResponse.json({ error: "Enter a question for the agent." }, { status: 400 });
  }
  const history: GraphjinAgentTurn[] = (Array.isArray(body?.history) ? body.history : [])
    .filter(
      (turn: unknown): turn is GraphjinAgentTurn =>
        Boolean(turn) &&
        typeof turn === "object" &&
        ((turn as GraphjinAgentTurn).role === "user" || (turn as GraphjinAgentTurn).role === "assistant") &&
        typeof (turn as GraphjinAgentTurn).content === "string",
    )
    .slice(-MAX_HISTORY_TURNS)
    .map((turn: GraphjinAgentTurn) => ({
      role: turn.role,
      content: turn.content.slice(0, MAX_INSTRUCTION_LENGTH),
      ...(typeof turn.status === "string" ? { status: turn.status } : {}),
    }));
  const result = await askGraphjinAdminAgent(
    { orgId: await getOrgId(), userId: actor.userId },
    instruction,
    history,
  );
  if (!result.ok) return NextResponse.json({ error: result.error }, { status: 502 });
  return NextResponse.json(result.data);
}
