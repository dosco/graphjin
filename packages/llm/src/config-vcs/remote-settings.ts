import { join } from "node:path";
import { context_remote, db, eq } from "@neko/db";
import { unmodifiedBuiltinSkills } from "../work/workspace";
import { maybeDecryptSecret, maybeEncryptSecret } from "../secrets";
import { recordAuditEvent } from "../workflows/audit-chain";
import { git } from "./git-shell";
import { insertConfigChangeRow } from "./index";
import {
  bringInRemoteSkills,
  CONTEXT_REMOTE_KINDS,
  ContextRemoteError,
  PullRequestNotOpenedError,
  DEFAULT_CONTEXT_REMOTE_KINDS,
  parseRemoteUrl,
  incomingSkillDiff,
  previewPublish,
  publishContext,
  publishItemDiff,
  remotePackArchive,
  remoteProvider,
  remoteTransport,
  skillTreesAt,
  validBranchName,
  type ContextRemoteKind,
  type ContextRemoteMode,
  type ContextRemoteProvider,
  type ItemDiff,
  type PublishPreviewItem,
  type PublishResult,
  type RemoteAccess,
} from "./remote";
import { generateDeployKey, hostKeyFingerprints, scanHostKeys } from "./ssh";

export type ContextRemoteSettings = {
  url: string;
  provider: ContextRemoteProvider;
  mode: ContextRemoteMode;
  branch: string;
  kinds: ContextRemoteKind[];
  username: string | null;
  hasToken: boolean;
  transport: "https" | "ssh";
  /** SSH remotes: the deploy key to add to the repository, and the server host keys to confirm. */
  ssh: { publicKey: string; hostFingerprints: string[]; hostConfirmed: boolean } | null;
  lastPublish: { at: string; status: "ok" | "failed"; detail: string; link: string | null } | null;
};

export type ContextRemoteDraft = {
  url: string;
  mode: ContextRemoteMode;
  branch: string;
  kinds: ContextRemoteKind[];
  username?: string | null;
  /** A new token. Omit to keep the saved token; null removes it. */
  token?: string | null;
  /** SSH remotes: replace the deploy key with a new one. */
  newSshKey?: boolean;
};

export type Row = typeof context_remote.$inferSelect;

async function toSettings(row: Row): Promise<ContextRemoteSettings> {
  const transport = remoteTransport(new URL(row.url)) === "ssh" ? "ssh" : "https";
  return {
    url: row.url,
    transport,
    ssh: transport === "ssh" && row.ssh_public_key
      ? {
          publicKey: row.ssh_public_key,
          hostFingerprints: await hostKeyFingerprints(row.ssh_known_hosts ?? ""),
          hostConfirmed: row.ssh_host_confirmed,
        }
      : null,
    provider: remoteProvider(new URL(row.url)),
    mode: row.mode as ContextRemoteMode,
    branch: row.branch,
    kinds: storedKinds(row.kinds),
    username: row.username,
    hasToken: Boolean(row.token),
    lastPublish: row.last_published_at && row.last_publish_status
      ? {
          at: row.last_published_at.toISOString(),
          status: row.last_publish_status as "ok" | "failed",
          detail: row.last_publish_detail ?? "",
          link: row.last_publish_link,
        }
      : null,
  };
}

export async function readRow(orgId: string): Promise<Row | null> {
  const [row] = await db().select().from(context_remote).where(eq(context_remote.org_id, orgId)).limit(1);
  return row ?? null;
}

export async function getContextRemote(orgId: string): Promise<ContextRemoteSettings | null> {
  const row = await readRow(orgId);
  return row ? await toSettings(row) : null;
}

export async function saveContextRemote(
  orgId: string,
  actorUserId: string | null,
  draft: ContextRemoteDraft,
): Promise<ContextRemoteSettings> {
  const parsed = parseRemoteUrl(String(draft.url ?? ""));
  const url = parsed.toString();
  if (draft.mode !== "push" && draft.mode !== "pull_request") {
    throw new ContextRemoteError("Choose push or pull request.");
  }
  const branch = String(draft.branch ?? "").trim();
  if (!validBranchName(branch)) throw new ContextRemoteError("Enter a valid branch name, such as main.");
  const kinds = [...new Set(draft.kinds ?? [])];
  if (kinds.length === 0 || kinds.some((kind) => !CONTEXT_REMOTE_KINDS.includes(kind))) {
    throw new ContextRemoteError("Choose at least one kind of context to publish.");
  }
  const username = draft.username?.trim() || null;
  const tokenUpdate = draft.token === undefined
    ? {}
    : { token: draft.token?.trim() ? maybeEncryptSecret(draft.token.trim()) : null };
  const sshUpdate: Partial<Row> = {};
  if (remoteTransport(parsed) === "ssh") {
    const previous = await readRow(orgId);
    if (!previous?.ssh_private_key || draft.newSshKey) {
      const key = await generateDeployKey(`openneko-${orgId}`);
      sshUpdate.ssh_private_key = maybeEncryptSecret(key.privateKey);
      sshUpdate.ssh_public_key = key.publicKey;
    }
    const endpoint = (value: string) => {
      const target = new URL(value);
      return target.protocol === "ssh:" ? `${target.hostname}:${target.port || "22"}` : null;
    };
    if (!previous?.ssh_known_hosts || endpoint(previous.url) !== endpoint(url)) {
      try {
        sshUpdate.ssh_known_hosts = await scanHostKeys(parsed.hostname, Number(parsed.port || 22));
      } catch (error) {
        throw new ContextRemoteError((error as Error).message);
      }
      sshUpdate.ssh_host_confirmed = false;
    }
  }
  const values = { url, mode: draft.mode, branch, kinds, username, ...sshUpdate, updated_by_user_id: actorUserId, updated_at: new Date() };
  await db()
    .insert(context_remote)
    .values({ org_id: orgId, ...values, ...tokenUpdate })
    .onConflictDoUpdate({ target: context_remote.org_id, set: { ...values, ...tokenUpdate } });
  await recordAuditEvent({
    orgId,
    entityKind: "context_remote",
    entityId: orgId,
    event: "context:remote_changed",
    payload: {
      actorUserId, url, mode: draft.mode, branch, kinds,
      tokenChanged: draft.token !== undefined,
      sshKeyChanged: Boolean(sshUpdate.ssh_public_key),
      hostKeysChanged: sshUpdate.ssh_known_hosts !== undefined,
    },
  });
  return (await getContextRemote(orgId))!;
}

/** The administrator confirms the SSH server host keys OpenNeko read on save. */
export async function confirmContextRemoteHostKey(orgId: string, actorUserId: string | null): Promise<ContextRemoteSettings> {
  const row = await readRow(orgId);
  if (!row?.ssh_known_hosts) throw new ContextRemoteError("This remote has no SSH host key to confirm.");
  await db().update(context_remote).set({ ssh_host_confirmed: true }).where(eq(context_remote.org_id, orgId));
  await recordAuditEvent({
    orgId,
    entityKind: "context_remote",
    entityId: orgId,
    event: "context:host_key_confirmed",
    payload: { actorUserId, fingerprints: await hostKeyFingerprints(row.ssh_known_hosts) },
  });
  return (await getContextRemote(orgId))!;
}

export async function deleteContextRemote(orgId: string, actorUserId: string | null): Promise<void> {
  await db().delete(context_remote).where(eq(context_remote.org_id, orgId));
  await recordAuditEvent({
    orgId,
    entityKind: "context_remote",
    entityId: orgId,
    event: "context:remote_removed",
    payload: { actorUserId },
  });
}

async function previewOptions(row: Row, orgRoot: string) {
  return {
    orgRoot,
    access: remoteAccess(row),
    branch: row.branch,
    kinds: storedKinds(row.kinds),
    excludeSkills: await unmodifiedBuiltinSkills(join(orgRoot, "skills")),
    skillBases: row.skill_bases ?? {},
  };
}

async function requireRow(orgId: string): Promise<Row> {
  const row = await readRow(orgId);
  if (!row) throw new ContextRemoteError("Connect a remote repository first.");
  return row;
}

/** What publishing would send to the org's remote now. */
export async function previewOrgPublish(orgId: string, orgRoot: string): Promise<PublishPreviewItem[]> {
  return previewPublish(await previewOptions(await requireRow(orgId), orgRoot));
}

/** The diff a publish would send for one item. */
export async function diffOrgPublishItem(orgId: string, orgRoot: string, kind: string, name: string): Promise<ItemDiff> {
  const row = await requireRow(orgId);
  const options = await previewOptions(row, orgRoot);
  if (!options.kinds.includes(kind as ContextRemoteKind)) throw new ContextRemoteError("This remote does not publish that kind.");
  return publishItemDiff({ ...options, kind: kind as ContextRemoteKind, name });
}

/** The repository's version of a skill compared with OpenNeko's. */
export async function diffOrgIncomingSkill(orgId: string, orgRoot: string, name: string): Promise<ItemDiff> {
  const row = await requireRow(orgId);
  return incomingSkillDiff({ orgRoot, branch: row.branch, name });
}

/** Publish the org's company context to its remote and record the outcome. */
export async function publishOrgContext(
  orgId: string,
  orgRoot: string,
  actorUserId: string | null,
): Promise<PublishResult> {
  const row = await readRow(orgId);
  if (!row) throw new ContextRemoteError("Connect a remote repository first.");
  const record = (status: "ok" | "failed", detail: string, link: string | null) =>
    db().update(context_remote).set({
      last_published_at: new Date(),
      last_publish_status: status,
      last_publish_detail: detail.slice(0, 1000),
      last_publish_link: link,
    }).where(eq(context_remote.org_id, orgId));
  try {
    const result = await publishContext({
      orgRoot,
      access: remoteAccess(row),
      excludeSkills: await unmodifiedBuiltinSkills(join(orgRoot, "skills")),
      skillBases: row.skill_bases ?? {},
      mode: row.mode as ContextRemoteMode,
      branch: row.branch,
      kinds: storedKinds(row.kinds),
    });
    await record("ok", result.detail, result.link);
    if (result.status === "pushed" && result.sha && result.head) {
      const remote = await skillTreesAt(orgRoot, result.sha);
      const local = await skillTreesAt(orgRoot, result.head);
      const bases = { ...row.skill_bases };
      for (const [name, tree] of Object.entries(remote)) if (local[name]) bases[name] = { remote: tree, local: local[name] };
      await db().update(context_remote).set({ skill_bases: bases }).where(eq(context_remote.org_id, orgId));
    }
    await recordAuditEvent({
      orgId,
      entityKind: "context_remote",
      entityId: orgId,
      event: "context:published",
      payload: { actorUserId, status: result.status, branch: result.branch, sha: result.sha, link: result.link },
    });
    return result;
  } catch (error) {
    if (!(error instanceof ContextRemoteError)) console.warn(`[config-vcs] publish failed: ${(error as Error).message}`);
    const detail = error instanceof ContextRemoteError ? error.message : "Publishing failed.";
    await record("failed", detail, error instanceof PullRequestNotOpenedError ? error.link : null);
    throw error instanceof ContextRemoteError ? error : new ContextRemoteError(detail);
  }
}

export function remoteAccess(row: Row): RemoteAccess {
  const url = parseRemoteUrl(row.url);
  const ssh = remoteTransport(url) === "ssh";
  if (ssh && !row.ssh_host_confirmed) throw new ContextRemoteError("Confirm the server's SSH host key first.");
  return {
    url,
    ...(row.token ? { token: maybeDecryptSecret(row.token) } : {}),
    ...(row.username ? { username: row.username } : {}),
    ...(ssh && row.ssh_private_key
      ? { ssh: { privateKey: maybeDecryptSecret(row.ssh_private_key), knownHosts: row.ssh_known_hosts ?? "" } }
      : {}),
  };
}

export type UpdateChoices = {
  skills: Array<{ name: string; choice: "remote" | "local" }>;
  packs: string[];
};

/**
 * Apply the admin's choices against the remote tip from the last check.
 * Skills are committed here. Pack archives are returned for the pack
 * uploader, which validates and stores them as new versions.
 */
export async function applyOrgUpdates(
  orgId: string,
  orgRoot: string,
  actorUserId: string | null,
  choices: UpdateChoices,
): Promise<{ sha: string | null; packArchives: Array<{ id: string; bytes: Buffer }> }> {
  const row = await readRow(orgId);
  if (!row) throw new ContextRemoteError("Connect a remote repository first.");
  const tip = (await git(orgRoot, ["rev-parse", "--verify", "--quiet", `refs/openneko/remote/${row.branch}`]).catch(() => ({ stdout: "" }))).stdout.trim();
  if (!tip) throw new ContextRemoteError("Check for updates first.");
  const remoteSkills = await skillTreesAt(orgRoot, tip);
  const unknown = [...choices.skills.map((s) => s.name).filter((name) => !remoteSkills[name])];
  if (unknown.length) throw new ContextRemoteError(`The repository has no skill named ${unknown.join(", ")}.`);

  const bringIn = choices.skills.filter((s) => s.choice === "remote").map((s) => s.name).sort();
  const source = `${new URL(row.url).hostname}${new URL(row.url).pathname.replace(/\.git$/, "")}`;
  const sha = await bringInRemoteSkills({
    orgRoot,
    tip,
    names: bringIn,
    message: `Brought in ${bringIn.length === 1 ? "skill" : "skills"} from ${source}: ${bringIn.join(", ")}`,
  });
  if (sha) {
    await insertConfigChangeRow({ orgId, artifactKind: "skill", artifactRef: bringIn.join(", "), actorUserId, commitSha: sha, summary: `Brought in from ${source}` });
  }
  const head = (await git(orgRoot, ["rev-parse", "HEAD"])).stdout.trim();
  const localSkills = await skillTreesAt(orgRoot, head);
  const bases = { ...row.skill_bases };
  for (const { name } of choices.skills) bases[name] = { remote: remoteSkills[name]!, ...(localSkills[name] ? { local: localSkills[name] } : {}) };
  await db().update(context_remote).set({ skill_bases: bases }).where(eq(context_remote.org_id, orgId));

  const packArchives = [];
  for (const id of choices.packs) packArchives.push({ id, bytes: await remotePackArchive(orgRoot, tip, id) });
  await recordAuditEvent({
    orgId,
    entityKind: "context_remote",
    entityId: orgId,
    event: "context:pulled",
    payload: { actorUserId, tip, skills: choices.skills, packs: choices.packs, sha },
  });
  return { sha, packArchives };
}

/** Saved kinds the product still publishes; retired kinds such as library drop out. */
function storedKinds(kinds: readonly string[] | null | undefined): ContextRemoteKind[] {
  return (kinds ?? DEFAULT_CONTEXT_REMOTE_KINDS).filter((kind): kind is ContextRemoteKind =>
    (CONTEXT_REMOTE_KINDS as readonly string[]).includes(kind));
}
