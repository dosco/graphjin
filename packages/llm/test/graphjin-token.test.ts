import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { afterAll, beforeAll, describe, expect, it } from "vitest";
import { _resetSecretKeyCacheForTesting } from "@neko/secret-crypt";
import {
  graphjinSigningSecretB64,
  graphjinTokenNeedsRefresh,
  mintGraphjinToken,
  verifyGraphjinToken,
} from "../src/graphjin/token";

let dir: string;
const prevXdg = process.env.XDG_CONFIG_HOME;

beforeAll(async () => {
  dir = await mkdtemp(join(tmpdir(), "gj-token-"));
  process.env.XDG_CONFIG_HOME = dir;
  _resetSecretKeyCacheForTesting();
});

afterAll(async () => {
  if (prevXdg === undefined) delete process.env.XDG_CONFIG_HOME;
  else process.env.XDG_CONFIG_HOME = prevXdg;
  _resetSecretKeyCacheForTesting();
  await rm(dir, { recursive: true, force: true });
});

describe("GraphJin actor tokens (GJ4)", () => {
  it("mints a verifiable HS256 JWT with the K1 actor snapshot", () => {
    const t0 = 1_750_000_000_000;
    const token = mintGraphjinToken({
      orgId: "org-1",
      userId: "u-1",
      role: "member",
      nowMs: t0,
    });
    const claims = verifyGraphjinToken(token, "org-1", t0 + 1000);
	    expect(claims).toMatchObject({
	      sub: "u-1",
	      role: "member",
	      roles: ["member"],
	      account_id: "org-1",
	      org_id: "org-1",
	    });
    expect(claims!.exp - claims!.iat).toBe(300);
  });

  it("carries group roles and group slugs for union role mode", () => {
    const token = mintGraphjinToken({
      orgId: "org-1",
      userId: "u-1",
      role: "member",
      groupRoles: ["og_finance", "og_sales", "og_finance"],
      groups: ["everyone", "finance", "sales"],
    });
    expect(verifyGraphjinToken(token, "org-1")).toMatchObject({
      role: "member",
      roles: ["member", "og_finance", "og_sales"],
      groups: ["everyone", "finance", "sales"],
    });
    expect(verifyGraphjinToken(mintGraphjinToken({ orgId: "org-1", userId: "u-1", role: "member" }), "org-1")).not.toHaveProperty("groups");
  });

  it("service principal gets sub=service", () => {
    const token = mintGraphjinToken({
      orgId: "org-1",
      userId: null,
      role: "service",
    });
    expect(verifyGraphjinToken(token, "org-1")?.sub).toBe("service");
  });

  it("rejects cross-org tokens, tampering, and expiry", () => {
    const t0 = 1_750_000_000_000;
    const token = mintGraphjinToken({
      orgId: "org-1",
      userId: "u-1",
      role: "admin",
      nowMs: t0,
    });
    // wrong org → different signing secret AND org claim mismatch
    expect(verifyGraphjinToken(token, "org-2", t0 + 1000)).toBeNull();
    // tampered payload
    const [h, p, s] = token.split(".") as [string, string, string];
    const forged = Buffer.from(
      JSON.stringify({
        sub: "u-1",
        role: "admin",
        org_id: "org-1",
        iat: 0,
        exp: 9999999999,
      }),
    ).toString("base64url");
    expect(verifyGraphjinToken(`${h}.${forged}.${s}`, "org-1", t0)).toBeNull();
    void p;
    // expired
    expect(
      verifyGraphjinToken(token, "org-1", t0 + 301_000),
    ).toBeNull();
  });

  it("flags refresh inside the final minute", () => {
    const t0 = 1_750_000_000_000;
    const token = mintGraphjinToken({
      orgId: "org-1",
      userId: "u-1",
      role: "member",
      nowMs: t0,
    });
    expect(graphjinTokenNeedsRefresh(token, "org-1", t0 + 200_000)).toBe(false);
    expect(graphjinTokenNeedsRefresh(token, "org-1", t0 + 245_000)).toBe(true);
    expect(graphjinTokenNeedsRefresh(token, "org-1", t0 + 400_000)).toBe(true);
  });

  it("exposes the base64 secret for the GraphJin config side", () => {
    const b64 = graphjinSigningSecretB64("org-1");
    expect(Buffer.from(b64, "base64")).toHaveLength(32);
    expect(graphjinSigningSecretB64("org-2")).not.toBe(b64);
  });
});
