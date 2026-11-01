import "server-only";
import { getOrgId } from "@neko/db";
import { getCurrentUser } from "@/lib/auth";

export async function personalPackActor() {
  const user = await getCurrentUser();
  if (!user) throw new Error("Sign in to OpenNeko to manage your personal connections.");
  return { orgId: await getOrgId(), userId: user.id };
}
