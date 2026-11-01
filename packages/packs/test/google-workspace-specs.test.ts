import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { parse as parseYaml } from "yaml";
import { describe, expect, it } from "vitest";
import { loadSolutionPack } from "../src/bundle.js";

const here = dirname(fileURLToPath(import.meta.url));
const specsRoot = resolve(here, "../../../packs/google-workspace/graphjin/specs");
const services = ["gmail", "drive", "calendar", "sheets", "docs", "slides"];
const writableServices = new Set(["gmail", "drive", "calendar", "sheets"]);

describe("Google Workspace curated REST specifications", () => {
  it("loads as an installable OAuth pack with customer-owned credentials", async () => {
    const bundle = await loadSolutionPack(resolve(specsRoot, "../.."));
    expect(bundle.manifest.metadata.id).toBe("google-workspace");
    expect(bundle.manifest.oauth).toEqual([
      expect.objectContaining({
        key: "workspace",
        scope: "user",
        experience: expect.objectContaining({ description: expect.any(String), setupInstructions: expect.any(String) }),
        clientIdInput: "google-workspace.oauth_client_id",
        clientSecret: "google-workspace.oauth_client_secret",
        accessToken: "google-workspace.access_token",
        refreshToken: "google-workspace.refresh_token",
      }),
    ]);
    expect(bundle.manifest.oauth[0]?.scopes).toContain("https://www.googleapis.com/auth/userinfo.email");
    expect(bundle.manifest.oauth[0]?.scopes).not.toContain("email");
    expect(bundle.manifest.oauth[0]?.authorizationParams).not.toHaveProperty("include_granted_scopes");
    expect(bundle.manifest.permissions.network).toEqual(expect.arrayContaining([
      "accounts.google.com",
      "oauth2.googleapis.com",
      "gmail.googleapis.com",
    ]));
    expect(bundle.artifacts.filter((artifact) => artifact.kind === "source")).toHaveLength(6);
    expect(bundle.artifacts.filter((artifact) => artifact.kind === "skill")).toHaveLength(6);
    expect(bundle.artifacts.filter((artifact) => artifact.kind === "saved_query")).toHaveLength(1);
    expect(bundle.artifacts.filter((artifact) => artifact.kind === "action")).toHaveLength(4);
    expect(bundle.artifacts.filter((artifact) => artifact.kind === "policy")).toHaveLength(1);
  });

  it.each(services)("keeps %s operations explicit and bearer-authenticated", async (service) => {
    const spec = parseYaml(await readFile(resolve(specsRoot, `${service}.yaml`), "utf8")) as {
      openapi: string;
      paths: Record<string, Record<string, unknown>>;
      components: { securitySchemes: { oauthBearer: { type: string; scheme: string } } };
    };

    expect(spec.openapi).toBe("3.0.3");
    expect(spec.components.securitySchemes.oauthBearer).toEqual({
      type: "http",
      scheme: "bearer",
    });
    expect(Object.keys(spec.paths).length).toBeGreaterThan(0);
    for (const operations of Object.values(spec.paths)) {
      for (const operation of Object.values(operations)) {
        expect(operation).toMatchObject({ operationId: expect.any(String) });
      }
    }
    const methods = Object.values(spec.paths).flatMap((operations) => Object.keys(operations));
    expect(methods).toContain("get");
    expect(methods.some((method) => method !== "get")).toBe(writableServices.has(service));
  });

  it("keeps the initial skills REST-based and read-only", async () => {
    const skillsRoot = resolve(specsRoot, "../../skills");
    const skills = ["google-workspace-read", "google-workspace-cross-app-research"];
    for (const skill of skills) {
      const content = await readFile(resolve(skillsRoot, skill, "SKILL.md"), "utf8");
      expect(content).not.toMatch(/\bgws\s+(?:gmail|drive|calendar|sheets|docs|slides)\b/i);
      expect(content).toMatch(/read-only|does not perform writes/i);
    }
  });

  it("binds every service and requests writes only for governed services", async () => {
    const sourceFile = parseYaml(
      await readFile(resolve(specsRoot, "../sources.yaml"), "utf8"),
    ) as {
      sources: Array<{
        name: string;
        kind: string;
        base_url: string;
        openapi: string;
        read_only: boolean;
        capabilities?: Record<string, boolean>;
        auth: { type: string; token: string };
      }>;
    };

    expect(sourceFile.sources).toHaveLength(services.length);
    expect(sourceFile.sources.map((source) => source.name).sort()).toEqual(
      services.map((service) => `google_workspace_${service}`).sort(),
    );
    for (const source of sourceFile.sources) {
      expect(source.kind).toBe("api");
      const service = source.name.replace("google_workspace_", "");
      expect(source.read_only).toBe(!writableServices.has(service));
      if (writableServices.has(service)) {
        expect(source.capabilities?.["api.write"]).toBe(true);
      }
      expect(source.auth).toEqual({
        type: "bearer",
        token: "{{secret.google-workspace.access_token}}",
      });
      await expect(
        readFile(resolve(specsRoot, source.openapi.replace("graphjin/specs/", "")), "utf8"),
      ).resolves.toContain("openapi: 3.0.3");
    }
  });
});
