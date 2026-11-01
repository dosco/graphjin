import { describe, expect, it } from "vitest";
import { canAccessWorkThread } from "@/lib/work-thread-auth";

describe("web Ask thread privacy", () => {
  const member = { userId: "member-1", role: "member" as const };
  const admin = { userId: "admin-1", role: "admin" as const };

  it("lets members access only their own threads", () => {
    expect(canAccessWorkThread(member, { created_by_user_id: "member-1" })).toBe(true);
    expect(canAccessWorkThread(member, { created_by_user_id: "member-2" })).toBe(false);
    expect(canAccessWorkThread(member, { created_by_user_id: "admin-1" })).toBe(false);
  });

  it("does not turn the admin role into chat surveillance", () => {
    expect(canAccessWorkThread(admin, { created_by_user_id: "admin-1" })).toBe(true);
    expect(canAccessWorkThread(admin, { created_by_user_id: "member-1" })).toBe(false);
    expect(canAccessWorkThread(admin, { created_by_user_id: "admin-2" })).toBe(false);
  });

  it("keeps the solo profile scoped to null-owned local threads", () => {
    const solo = { userId: null, role: "admin" as const };
    expect(canAccessWorkThread(solo, { created_by_user_id: null })).toBe(true);
    expect(canAccessWorkThread(solo, { created_by_user_id: "member-1" })).toBe(false);
  });
});
