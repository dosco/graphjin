import { getCurrentActor } from "@/lib/actor";
import { getAuthProvider, getCurrentUser, SESSION_COOKIE_NAME } from "@/lib/auth";
import { cookies } from "next/headers";
import type { RailIdentity } from "@/components/AppRail";

/**
 * The rail's identity, resolved on the server so the name and Sign out
 * paint with the first frame. A failure keeps the app usable: the rail
 * falls back to its own fetch.
 */
export async function railIdentity(): Promise<RailIdentity | undefined> {
  try {
    const [user, jar] = await Promise.all([getCurrentUser(), cookies()]);
    const signedIn = Boolean(jar.get(SESSION_COOKIE_NAME)?.value);
    if (!user) return undefined;
    const [actor, provider] = await Promise.all([getCurrentActor(), getAuthProvider()]);
    return {
      user: { email: user.email, name: user.name },
      mode: !provider ? "solo" : actor?.role === "member" ? "member" : "admin",
      signedIn,
    };
  } catch {
    return undefined;
  }
}
