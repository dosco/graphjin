import { afterAll, afterEach, beforeAll, describe, expect, it } from "vitest";
import {
  clearProvider,
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import { pool } from "@neko/db";
import {
  DEFAULT_POLICY,
  INSTALL_POLICY_SCOPE,
  getInstallPolicy,
  getInstallPolicyPayload,
  saveInstallPolicyDraft,
} from "@/lib/install-policy-settings";

describe("install-policy-settings: pure helpers", () => {
  it("DEFAULT_POLICY is secure-by-default", () => {
    expect(DEFAULT_POLICY).toEqual({ allowGitUrlInstalls: false });
  });
});

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[install-policy-settings] skipping DB tests: metadata Postgres unreachable.");
}

describeIfDb("install-policy-settings: persistence", () => {
  let orgId: string;

  beforeAll(async () => {
    orgId = uniqueOrgId("install-policy");
    await createTestOrg(orgId);
  });

  afterAll(async () => {
    await deleteTestOrg(orgId);
    await pool().end();
  });

  afterEach(async () => {
    await clearProvider(orgId, INSTALL_POLICY_SCOPE);
  });

  it("getInstallPolicy returns DEFAULT_POLICY when no row exists", async () => {
    const policy = await getInstallPolicy(orgId);
    expect(policy).toEqual(DEFAULT_POLICY);
  });

  it("getInstallPolicyPayload reports source=default when no row exists", async () => {
    const payload = await getInstallPolicyPayload(orgId);
    expect(payload.source).toBe("default");
    expect(payload.policy).toEqual(DEFAULT_POLICY);
  });

  it("saveInstallPolicyDraft persists + getInstallPolicy reads it back", async () => {
    await saveInstallPolicyDraft(orgId, { allowGitUrlInstalls: true });
    expect(await getInstallPolicy(orgId)).toEqual({ allowGitUrlInstalls: true });
  });

  it("an empty draft leaves the stored value alone", async () => {
    await saveInstallPolicyDraft(orgId, { allowGitUrlInstalls: true });
    await saveInstallPolicyDraft(orgId, {});
    expect((await getInstallPolicy(orgId)).allowGitUrlInstalls).toBe(true);
  });

  it("getInstallPolicyPayload reports source=org after save", async () => {
    await saveInstallPolicyDraft(orgId, { allowGitUrlInstalls: true });
    const payload = await getInstallPolicyPayload(orgId);
    expect(payload.source).toBe("org");
  });

  it("flipping the switch back off persists", async () => {
    await saveInstallPolicyDraft(orgId, { allowGitUrlInstalls: true });
    await saveInstallPolicyDraft(orgId, { allowGitUrlInstalls: false });
    expect((await getInstallPolicy(orgId)).allowGitUrlInstalls).toBe(false);
  });
});
