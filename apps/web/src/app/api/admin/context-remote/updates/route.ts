import { NextResponse } from "next/server";
import { applyOrgUpdates, ContextRemoteError, diffOrgIncomingSkill, getContextRemote, type RemoteUpdates, type UpdateChoices } from "@neko/llm/config-vcs";
import { getOrgAgentRoot } from "@neko/llm/work";
import { isDenied, requireAdminActor } from "@/lib/admin-auth";
import { getOrgId } from "@/lib/db";
import { readJsonBody } from "@/lib/groups-admin";
import { requestPackWorker } from "@/lib/solution-packs";

export const runtime = "nodejs";
export const dynamic = "force-dynamic";

/** The worker checks the repository, because it validates packs. */
async function checkUpdates(orgId: string): Promise<RemoteUpdates> {
  if (!(await getContextRemote(orgId))) throw new ContextRemoteError("Connect a remote repository first.");
  const result = await requestPackWorker("/admin/context-remote/updates");
  if (result.status >= 400) throw new ContextRemoteError((result.body as { error?: string })?.error ?? `Update check failed (${result.status})`);
  return (result.body as { updates: RemoteUpdates }).updates;
}

function failure(error: unknown) {
  if (error instanceof ContextRemoteError) return NextResponse.json({ error: error.message }, { status: 400 });
  throw error;
}

/**
 * List skills and packs that changed in the remote repository.
 * With ?skill=…, the repository's version of that skill compared with OpenNeko's.
 */
export async function GET(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const orgId = await getOrgId();
  const skill = new URL(request.url).searchParams.get("skill");
  try {
    if (skill) return NextResponse.json(await diffOrgIncomingSkill(orgId, getOrgAgentRoot(orgId), skill));
    return NextResponse.json({ updates: await checkUpdates(orgId) });
  } catch (error) {
    return failure(error);
  }
}

/** Bring in the chosen skills and add the chosen packs as new versions. */
export async function POST(request: Request) {
  const actor = await requireAdminActor();
  if (isDenied(actor)) return actor;
  const body = (await readJsonBody(request)) as Partial<UpdateChoices> | null;
  const choices: UpdateChoices = {
    skills: Array.isArray(body?.skills)
      ? body!.skills.filter((s) => typeof s?.name === "string" && (s.choice === "remote" || s.choice === "local"))
      : [],
    packs: Array.isArray(body?.packs) ? body!.packs.filter((id): id is string => typeof id === "string") : [],
  };
  const orgId = await getOrgId();
  const orgRoot = getOrgAgentRoot(orgId);
  try {
    const { sha, packArchives } = await applyOrgUpdates(orgId, orgRoot, actor.userId, choices);
    const packs = [];
    for (const archive of packArchives) {
      const result = await requestPackWorker("/admin/packs/upload", {
        method: "POST",
        headers: { "Content-Type": "application/zip", "X-OpenNeko-Actor": encodeURIComponent(actor.userId ?? "") },
        body: new Uint8Array(archive.bytes),
      }).catch((error: Error) => ({ status: 502, body: { error: error.message } }));
      const error = result.status >= 400 ? (result.body as { error?: string })?.error ?? `Upload failed (${result.status})` : null;
      packs.push({ id: archive.id, ok: !error, ...(error ? { error } : {}) });
    }
    return NextResponse.json({ sha, packs, updates: await checkUpdates(orgId) });
  } catch (error) {
    return failure(error);
  }
}
