import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { db, eq, organization, pool, sso_setup } from "@neko/db";
import { SsoSetupService } from "../src/sso/sso-setup-service";
import { CoreSecrets } from "../src/secrets/core-secrets";
import { SignInService } from "../src/signin/signin-service";

const reachable = await pool().query("select 1").then(() => true, () => false);

async function signInFixture() {
  const dir = await mkdtemp(join(tmpdir(), "sso-setup-"));
  const secrets = new CoreSecrets({ configDir: dir, pollMs: 0 });
  await secrets.start();
  return { dir, secrets, signIn: new SignInService(secrets) };
}

(reachable ? describe : describe.skip)("SsoSetupService.setScalekitIds", () => {
  it("stores the organization for sign-in setup and for directory sync", async () => {
    const orgId = `sso-${Date.now().toString(36)}-${Math.random().toString(36).slice(2, 6)}`;
    await db().insert(organization).values({ id: orgId, name: "SSO" });
    const { dir, signIn } = await signInFixture();
    try {
      await new SsoSetupService(signIn).setScalekitIds(orgId, { environmentId: "env_1", organizationId: "org_9", tier: "dev" });
      const [row] = await db().select().from(sso_setup).where(eq(sso_setup.org_id, orgId));
      expect(row).toMatchObject({ scalekit_environment_id: "env_1", scalekit_organization_id: "org_9" });
      expect(signIn.providerValues("scalekit")).toMatchObject({
        SCALEKIT_ORGANIZATION_ID: "org_9",
        SCALEKIT_ENVIRONMENT_ID: "env_1",
      });
    } finally {
      await db().delete(sso_setup).where(eq(sso_setup.org_id, orgId));
      await db().delete(organization).where(eq(organization.id, orgId));
      await rm(dir, { recursive: true, force: true });
    }
  });
});

describe("SsoSetupService.setSignInCredentials", () => {
  it("stores the credentials and makes Scalekit the sign-in provider", async () => {
    const { dir, signIn } = await signInFixture();
    try {
      expect(signIn.getAuthProvider()).toBeNull();
      const service = new SsoSetupService(signIn);
      await service.setSignInCredentials("org-1", {
        environmentUrl: "https://acme.scalekit.dev",
        clientId: "skc_1",
        clientSecret: "secret",
      });
      expect(signIn.getAuthProvider()).toMatchObject({ pluginName: "scalekit", provisioning: "automatic" });
      expect(signIn.authSignInReady()).toBe(true);
      await expect(
        service.setSignInCredentials("org-1", { environmentUrl: "", clientId: "x", clientSecret: "y" }),
      ).rejects.toThrow(/required/);
    } finally {
      await rm(dir, { recursive: true, force: true });
    }
  });
});
