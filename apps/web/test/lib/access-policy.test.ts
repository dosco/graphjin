import { readdir } from "node:fs/promises";
import { join } from "node:path";
import { describe, expect, it } from "vitest";
import { ACCESS_POLICIES, accessPolicyFor } from "@/lib/access-policy";

const appDirectory = join(import.meta.dirname, "..", "..", "src", "app");

/** Route path for a file under src/app, with route groups and dynamic segments removed. */
function routePath(file: string): string {
  const segments = file
    .split("/")
    .slice(0, -1)
    .filter((segment) => !segment.startsWith("(") && segment !== "src" && segment !== "app");
  return `/${segments.join("/")}`;
}

async function routeFiles(directory: string, prefix = ""): Promise<string[]> {
  const entries = await readdir(directory, { withFileTypes: true });
  const found: string[] = [];
  for (const entry of entries) {
    const path = prefix ? `${prefix}/${entry.name}` : entry.name;
    if (entry.isDirectory()) found.push(...(await routeFiles(join(directory, entry.name), path)));
    else if (entry.name === "route.ts" || entry.name === "page.tsx") found.push(path);
  }
  return found;
}

describe("access policy", () => {
  it("covers every route and page", async () => {
    const uncovered = (await routeFiles(appDirectory))
      .map(routePath)
      .filter((path) => accessPolicyFor(path) === null);
    expect(uncovered).toEqual([]);
  });

  it("refuses a path no entry covers", () => {
    expect(accessPolicyFor("/api/brand-new-area/thing")).toBeNull();
    expect(accessPolicyFor("/brand-new-area")).toBeNull();
  });

  it("takes the longest matching prefix", () => {
    expect(accessPolicyFor("/api/admin/groups")?.rule).toBe("admin");
    expect(accessPolicyFor("/api/integrations/list")?.rule).toBe("signed-in");
    expect(accessPolicyFor("/api/integrations/connect/slack/start")?.rule).toBe("token");
    expect(accessPolicyFor("/admin/settings/sso")?.rule).toBe("public");
    expect(accessPolicyFor("/admin/users")?.rule).toBe("admin");
    expect(accessPolicyFor("/")?.rule).toBe("signed-in");
    expect(accessPolicyFor("/api/work/skills/")?.rule).toBe("signed-in");
  });

  it("keeps the sign-in flow reachable without a session", () => {
    for (const path of ["/signin", "/api/auth/begin", "/api/auth/callback", "/api/sso/setup/status", "/api/version"]) {
      expect(accessPolicyFor(path)?.rule).toBe("public");
    }
  });

  it("gives every admin area an entry of its own", async () => {
    const adminRoutes = (await routeFiles(appDirectory))
      .map(routePath)
      .filter((path) => path.startsWith("/admin") || path.startsWith("/api/admin"));
    expect(adminRoutes.length).toBeGreaterThan(20);
    for (const path of adminRoutes) {
      if (path.startsWith("/admin/settings/sso")) continue;
      expect(accessPolicyFor(path)?.rule).toBe("admin");
    }
  });

  it("lets every file in public/ past the gate", async () => {
    const { config } = await import("@/proxy");
    const matcher = new RegExp(`^${config.matcher[0]}$`);
    // The gate 404s a path it does not recognise, so a matched asset
    // would lose the mascot and the tab icon.
    for (const file of ["/cat.png", "/icon.png", "/file.svg", "/robots.txt"]) {
      expect(matcher.test(file)).toBe(false);
    }
    // The hot-reload socket and the RSC payloads keep the app alive in
    // dev; a 404 there leaves a page that renders but never hydrates.
    for (const internal of ["/_next/webpack-hmr", "/_next/static/chunks/main.js", "/_next/image"]) {
      expect(matcher.test(internal)).toBe(false);
    }
    for (const page of ["/admin/users", "/api/admin/groups", "/profile", "/"]) {
      expect(matcher.test(page)).toBe(true);
    }
  });

  it("explains every entry that is not signed-in", () => {
    for (const policy of ACCESS_POLICIES) {
      if (policy.rule === "public" || policy.rule === "token") expect(policy.why ?? policy.prefix).toBeTruthy();
    }
  });
});
