import { expect, test } from "@playwright/test";

// UI contract only. The worker lifecycle suite checks real persistence.
test("configures a skills-only pack without requesting data sources", async ({ page }, testInfo) => {
  let dataRequests = 0;
  let installed = false;
  let reviewAttempts = 0;
  const inspection = {
    source: "uploaded", bundleHash: "fixture",
    manifest: {
      metadata: { id: "skills-only", name: "Skills only", version: "1.0.0", publisher: "fixture" },
      inputs: [], secrets: [], oauth: [], artifacts: { skills: ["skills/review"] },
    }, bindingRequirements: [], permissions: { database: "none", apiWrite: "blocked" },
  };
  await page.route("**/api/settings/data-sources", route => { dataRequests++; return route.fulfill({ status: 503, json: { error: "No data service" } }); });
  await page.route(url => url.pathname.startsWith("/api/admin/packs"), async route => {
    const path = new URL(route.request().url()).pathname;
    if (path === "/api/admin/packs") return route.fulfill({ json: { packs: [{ source: "uploaded", id: "skills-only", name: "Skills only", version: "1.0.0", installed }] } });
    if (path.endsWith("/inspect")) return route.fulfill({ json: inspection });
    if (path.endsWith("/status")) return route.fulfill({ status: installed ? 200 : 404, json: { status: "installed", version: "1.0.0", configuration: { inputs: {}, sourceBindings: {} } } });
    if (path.endsWith("/review")) {
      expect(route.request().postDataJSON()).not.toHaveProperty("dataSourceId");
      await new Promise(resolve => setTimeout(resolve, 300));
      if (++reviewAttempts === 1) return route.fulfill({ status: 503, json: { error: "Review is temporarily unavailable" } });
      return route.fulfill({ json: { ...inspection, reviewHash: "review", inputs: {}, runtime: { bindings: {} }, plan: { entries: [{ action: "create", kind: "skill", targetRef: "review" }] } } });
    }
    if (path.endsWith("/install")) {
      installed = true;
      return route.fulfill({ json: { status: "installed", version: "1.0.0" } });
    }
    return route.fulfill({ status: 404, json: {} });
  });
  await page.goto("/admin/settings/packs");
  const section = page.getByRole("region", { name: "Solution packs", exact: true });
  await page.getByLabel("Uploaded pack", { exact: true }).selectOption("skills-only");
  await expect(section.getByRole("heading", { name: "Skills only", exact: true })).toBeVisible();
  await expect(page.getByLabel("Data connection", { exact: true })).toHaveCount(0);
  for (const width of [1280, 390]) {
    await page.setViewportSize({ width, height: 900 });
    const review = section.getByRole("button", { name: "Review changes", exact: true });
    await review.focus();
    expect(await review.evaluate(el => { const style = getComputedStyle(el); return el === document.activeElement && (style.outlineStyle !== "none" || style.boxShadow !== "none"); })).toBe(true);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)).toBe(true);
    if (width === 390) expect((await review.boundingBox())!.height).toBeGreaterThanOrEqual(44);
    await section.screenshot({ path: testInfo.outputPath(`skills-only-${width}.png`) });
  }
  await section.getByRole("button", { name: "Review changes", exact: true }).click();
  await expect(section.getByRole("button", { name: "Reviewing…", exact: true })).toBeDisabled();
  await expect(section.getByRole("alert")).toBeVisible();
  await expect(section.getByRole("alert")).toBeFocused();
  await section.getByRole("button", { name: "Review changes", exact: true }).click();
  await section.getByRole("button", { name: "Approve and install", exact: true }).click();
  await expect(section.getByText("Installed", { exact: true })).toBeVisible();
  expect(dataRequests).toBe(0);
});
