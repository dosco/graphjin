import { and, app_user, channel_identity, db, eq, holds, isNull, resolveUserGroups, sql } from "@neko/db";

/**
 * A linked user acts through their groups, and needs the channel item for
 * the channel plugin; without it the inbound is dropped.
 */
async function linkedActor(orgId: string, channelPlugin: string, userId: string): Promise<ChannelActor> {
  const allowed = (await holds({ orgId, kind: "user", userId }, "channel", channelPlugin)).allowed;
  if (!allowed) return { userId: null, role: "member", blocked: true };
  const groups = await resolveUserGroups(orgId, userId);
  return { userId, role: groups.administrator ? "admin" : "member" };
}

/**
 * CH3 — resolve the acting principal for an inbound channel message.
 * A linked identity acts as its app_user (personal layer, persona, K2
 * grade — identical threading to web). Everything else stays anonymous
 * member-grade; an unknown channel actor is never silently an admin.
 */
export type ChannelActor = {
  userId: string | null;
  role: "admin" | "member";
  /** True when the identity is blocked — drop the inbound entirely. */
  blocked?: boolean;
};

export async function resolveChannelActor(
  orgId: string,
  channelPlugin: string,
  sender?: {
    id: string;
    displayName?: string;
    workspaceId?: string;
    email?: string;
  },
): Promise<ChannelActor> {
  const anonymous: ChannelActor = { userId: null, role: "member" };
  if (!sender) return anonymous;

  const tuple = and(
    eq(channel_identity.org_id, orgId),
    eq(channel_identity.channel_plugin, channelPlugin),
    eq(channel_identity.workspace_id, sender.workspaceId ?? ""),
    eq(channel_identity.channel_user_id, sender.id),
  );
  await db()
    .insert(channel_identity)
    .values({
      org_id: orgId,
      channel_plugin: channelPlugin,
      workspace_id: sender.workspaceId ?? "",
      channel_user_id: sender.id,
      display_name: sender.displayName ?? null,
      email: sender.email ?? null,
    })
    .onConflictDoNothing();
  let [identity] = await db().select().from(channel_identity).where(tuple).limit(1);
  if (!identity) return anonymous;

  // Keep the channel-provided profile fresh (it feeds the admin-map UI).
  if (
    (sender.displayName && sender.displayName !== identity.display_name) ||
    (sender.email && sender.email !== identity.email)
  ) {
    [identity] = await db()
      .update(channel_identity)
      .set({
        ...(sender.displayName ? { display_name: sender.displayName } : {}),
        ...(sender.email ? { email: sender.email } : {}),
        updated_at: new Date(),
      })
      .where(eq(channel_identity.id, identity.id))
      .returning();
  }

  if (identity.status === "blocked") {
    return { userId: null, role: "member", blocked: true };
  }

  if (identity.status === "linked" && identity.app_user_id) {
    const user = await getActiveUser(orgId, identity.app_user_id);
    if (user) return linkedActor(orgId, channelPlugin, user.id);
    return anonymous;
  }

  // SSO email match: the channel and the auth directory key on the same
  // email — auto-link with no user action.
  const email = sender.email ?? identity.email;
  if (email) {
    const [match] = await db()
      .select({ id: app_user.id })
      .from(app_user)
      .where(
        and(
          eq(app_user.org_id, orgId),
          sql`lower(${app_user.email}) = lower(${email})`,
          isNull(app_user.disabled_at),
        ),
      )
      .limit(1);
    if (match) {
      const now = new Date();
      await db()
        .update(channel_identity)
        .set({
          app_user_id: match.id,
          status: "linked",
          verified_at: now,
          updated_at: now,
        })
        .where(eq(channel_identity.id, identity.id));
      console.log(
        `[channel-identity] auto-linked ${channelPlugin}/${sender.id} → ${match.id} (email match)`,
      );
      return linkedActor(orgId, channelPlugin, match.id);
    }
  }

  return anonymous;
}

async function getActiveUser(
  orgId: string,
  userId: string,
): Promise<{ id: string } | null> {
  const [user] = await db()
    .select({ id: app_user.id })
    .from(app_user)
    .where(
      and(
        eq(app_user.org_id, orgId),
        eq(app_user.id, userId),
        isNull(app_user.disabled_at),
      ),
    )
    .limit(1);
  return user ?? null;
}
