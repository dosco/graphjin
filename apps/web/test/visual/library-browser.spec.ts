import { expect, test } from "@playwright/test";

const id = (index: number) => `11111111-1111-4111-8111-${String(index).padStart(12, "0")}`;
const date = "2026-09-01T00:00:00.000Z";

test("large Library browsing, details and removal stay usable on desktop and phone", async ({ page }, testInfo) => {
  const removed = new Set<string>();
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.route("**/api/library?*", route => {
    const params = new URL(route.request().url()).searchParams;
    const q = params.get("q") ?? "";
    if (q === "server-error") return route.fulfill({ status: 503, json: { error: "Library could not be loaded. Try again." } });
    const name = (index: number) => index === 0 ? "Inventory replenishment.xlsx" : `Synthetic document ${index}.pdf`;
    const all = Array.from({ length: 125 }, (_, index) => index).filter(index => !removed.has(id(index)));
    const matching = all.filter(index => name(index).toLowerCase().includes(q.toLowerCase()));
    const current = Number(params.get("page") ?? 1);
    const documents = matching.slice((current - 1) * 50, current * 50).map(index => ({
      id: id(index), filename: name(index),
      relativePath: `neko_library/users/u/${index}/doc.pdf`, sizeBytes: 12000, status: index === 1 ? "failed" : "ready",
      error: index === 1 ? "The original file is missing. Remove this document and upload it again." : null, createdAt: date,
    }));
    return route.fulfill({ json: { total: matching.length, page: current, pageSize: 50, documents } });
  });
  await page.route("**/api/library/documents/*", route => {
    const docId = new URL(route.request().url()).pathname.split("/").pop()!;
    if (route.request().method() === "DELETE") {
      removed.add(docId);
      return route.fulfill({ json: { ok: true } });
    }
    return route.fulfill({ json: { document: { id: docId, userId: "owner", filename: "Inventory replenishment.xlsx", sizeBytes: 12000,
      status: "ready", error: null, createdAt: date, updatedAt: date } } });
  });
  for (const width of [1280, 390]) {
    removed.clear();
    await page.setViewportSize({ width, height: 844 });
    await page.goto("/library");
    await expect(page.getByRole("status", { name: "" }).filter({ hasText: "125 documents" })).toBeVisible();
    await expect(page.locator(".library-browser-list > li")).toHaveCount(50);
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    if (width === 390) {
      await expect(page.getByRole("combobox", { name: "Status", exact: true })).toBeHidden();
      await page.getByRole("button", { name: "Filters", exact: true }).click();
      await expect(page.getByRole("combobox", { name: "Status", exact: true })).toBeVisible();
      await page.getByRole("button", { name: "Filters", exact: true }).click();
    }
    await page.screenshot({ path: testInfo.outputPath(`library-list-${width}.png`) });
    await page.getByRole("button", { name: "Next", exact: true }).click();
    await expect(page).toHaveURL(/page=2/);
    await expect(page.getByRole("link", { name: "Synthetic document 50.pdf", exact: true })).toBeVisible();
    await page.goBack();
    const search = page.getByRole("searchbox", { name: "Search library" });
    await search.fill("inventory");
    await expect(page).toHaveURL(/q=inventory/);
    await expect(page.locator(".library-browser-list > li")).toHaveCount(1);
    await page.getByRole("link", { name: "Inventory replenishment.xlsx", exact: true }).click();
    const detail = page.getByRole("region", { name: "Document details" });
    await expect(detail.getByRole("heading", { name: "Inventory replenishment.xlsx", exact: true })).toBeVisible();
    await expect(detail).toContainText("Private to you");
    expect(await detail.evaluate(element => {
      const rect = element.getBoundingClientRect();
      const rail = document.querySelector(".app-rail-wrap")!;
      const railRight = getComputedStyle(rail).display === "none" ? 0 : rail.getBoundingClientRect().right;
      return rect.left >= railRight && rect.right <= innerWidth;
    })).toBe(true);
    await page.screenshot({ path: testInfo.outputPath(`library-detail-${width}.png`) });
    await detail.getByRole("button", { name: "Remove from library", exact: true }).click();
    const confirm = page.getByRole("alertdialog");
    await expect(confirm).toBeInViewport();
    await confirm.getByRole("button", { name: "Remove", exact: true }).click();
    await expect(detail).toHaveCount(0);
    await expect(page.getByRole("heading", { name: "No matches", exact: true })).toBeVisible();
    await search.fill("no-match");
    await expect(page.getByRole("heading", { name: "No matches", exact: true })).toBeVisible();
    await search.fill("server-error");
    await expect(page.locator(".library-error[role=alert]")).toContainText("Library could not be loaded");
  }
});
