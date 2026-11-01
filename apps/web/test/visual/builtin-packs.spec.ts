import { readFile } from "node:fs/promises";
import { resolve } from "node:path";
import { parse } from "yaml";
import { parseManifest } from "@neko/packs/manifest";
import { expect, test } from "@playwright/test";

// Only installation state is synthetic; provider content is read from the packs.
test("built-in installation unlocks each pack's declared configuration", async ({ page }, testInfo) => {
  const manifests = await Promise.all(["magento", "google-workspace"].map(async id => parseManifest(parse(await readFile(resolve("../../packs", id, "pack.yaml"), "utf8")))));
  const installed = new Set<string>();
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.route("**/api/settings/data-sources", route => route.fulfill({ json: { sources: [{ id: "source", name: "Data", enabled: true, graphqlUrl: "https://data.test/graphql" }] } }));
  await page.route("**/api/pack-accounts/**", route => route.fulfill({ json: { key: "workspace", configured: false, connected: false, callbackUrl: "https://app.test/callback" } }));
  await page.route(url => url.pathname.startsWith("/api/admin/packs"), async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/admin/packs") return route.fulfill({ json: { packs: manifests.map(manifest => ({ ...manifest.metadata, source: "embedded", installed: installed.has(manifest.metadata.id) })) } });
    const manifest = manifests.find(value => path.includes(`/${value.metadata.id}/`))!;
    const status = { status: "installed", version: manifest.metadata.version, configuration: { required: true, inputs: {}, sourceBindings: {} } };
    if (path.endsWith("/install")) {
      expect(route.request().postDataJSON()).toMatchObject({ deferConfiguration: true, version: manifest.metadata.version });
      expect(route.request().postDataJSON()).not.toHaveProperty("secrets");
      await new Promise(resolve => setTimeout(resolve, 250));
      installed.add(manifest.metadata.id);
      return route.fulfill({ json: status });
    }
    if (path.endsWith("/status")) return route.fulfill({ status: installed.has(manifest.metadata.id) ? 200 : 404, json: status });
    if (path.endsWith("/inspect")) return route.fulfill({ json: { source: "embedded", manifest, bindingRequirements: [], permissions: manifest.permissions } });
    return route.fulfill({ status: 404, json: {} });
  });
  await page.goto("/admin/settings/packs");
  await expect(page.getByRole("button", { name: "Install", exact: true })).toHaveCount(2);
  await expect(page.getByRole("group", { name: "Pack configuration" })).toHaveCount(0);
  for (const manifest of manifests) {
    const row = page.getByRole("listitem").filter({ has: page.getByRole("heading", { name: manifest.metadata.name, exact: true }) });
    await row.getByRole("button", { name: "Install", exact: true }).click();
    await expect(row.getByRole("button", { name: "Installing…" })).toBeDisabled();
    await expect(row.getByRole("button", { name: "Manage", exact: true })).toBeEnabled();
    await expect(page.getByText("Installed. Complete and apply the configuration below to activate this pack.")).toBeVisible();
    for (const input of manifest.inputs.filter(input => input.type !== "boolean")) await expect(page.locator(`[name="${input.key}"]`)).toBeVisible();
    for (const secret of manifest.secrets.filter(secret => secret.purpose !== "pack_oauth_token")) await expect(page.locator(`[name="${secret.key}"]`)).toBeVisible();
    for (const width of [1440, 390]) {
      await page.setViewportSize({ width, height: 900 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
      await page.screenshot({ path: testInfo.outputPath(`${manifest.metadata.id}-${width}.png`), fullPage: true });
    }
  }
  expect(errors).toEqual([]);
});
