import { expect, test, type Page, type TestInfo } from "@playwright/test";

function captureBrowserErrors(page: Page) {
  const errors: string[] = [];
  page.on("console", (message) => {
    if (message.type() === "error") errors.push(message.text());
  });
  page.on("pageerror", (error) => errors.push(error.message));
  return errors;
}

async function verifyScreenshot(page: Page, testInfo: TestInfo, name: string) {
  if (process.env.CI === "true") {
    // Font rasterization and wrapping differ between macOS development and
    // Linux runners. CI enforces the platform-neutral geometry above and
    // publishes its own full-page render for review.
    await page.screenshot({ path: testInfo.outputPath(name), fullPage: true });
    return;
  }
  await expect(page).toHaveScreenshot(name, { fullPage: true });
}

test.beforeEach(async ({ page }) => {
  await page.route("**/api/admin/packs", route => route.fulfill({ json: { packs: [] } }));
  await page.emulateMedia({ reducedMotion: "reduce", colorScheme: "light" });
  await page.clock.setFixedTime(new Date("2026-08-29T08:00:00.000Z"));
});

test("pack catalog has loading and empty states at desktop and phone widths", async ({ page }, testInfo) => {
  for (const width of [1280, 390]) {
    await page.setViewportSize({ width, height: 900 });
    let release!: () => void;
    const pending = new Promise<void>(resolve => { release = resolve; });
    await page.route("**/api/admin/packs", async route => {
      await pending;
      await route.fulfill({ json: { packs: [] } });
    });
    await page.goto("/admin/settings/packs", { waitUntil: "domcontentloaded" });
    await expect(page.getByText("Loading packs…", { exact: true })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath(`pack-loading-${width}.png`) });
    release();
    await expect(page.getByText("Upload a custom pack", { exact: true })).toBeVisible();
    await page.screenshot({ path: testInfo.outputPath(`pack-empty-${width}.png`) });
    await page.getByText("Upload a custom pack", { exact: true }).click();
    await expect(page.getByLabel("Pack archive", { exact: true })).toBeVisible();
    const controlHeight = await page.getByLabel("Pack archive", { exact: true }).evaluate(el => el.getBoundingClientRect().height);
    expect(controlHeight).toBeGreaterThanOrEqual(width === 390 ? 44 : 40);
    await page.unroute("**/api/admin/packs");
  }
});

test("Magento settings use the shared visual language and plain activity copy", async ({
  page,
}, testInfo) => {
  const browserErrors = captureBrowserErrors(page);
  await page.goto("/admin/settings/packs/magento/operations", { waitUntil: "domcontentloaded" });

  await expect(page.getByRole("heading", { name: "Store operations", exact: true })).toBeVisible();
  await expect(page.getByText(/Version 2\.0\.0 · installed Aug 27, 2026/)).toBeVisible();
  await expect(page.getByText("Prices restored", { exact: true })).toBeVisible();
  await expect(page.getByText("Restored the original prices for 2 products.")).toBeVisible();
  await expect(page.getByText("No current price change", { exact: false })).toBeVisible();
  await expect(page.getByText("Content restored", { exact: true })).toBeHidden();
  await expect(page.getByText("Local acceptance internal-run", { exact: true })).toBeHidden();
  const internalCopy = page.getByText(/acceptance|async|inverse|operator/i);
  for (let index = 0; index < await internalCopy.count(); index += 1) {
    await expect(internalCopy.nth(index)).toBeHidden();
  }

  const sharedStyles = await page.evaluate(() => {
    const checkbox = document.querySelector<HTMLInputElement>("[data-ui-checkbox-control]");
    const checkboxLabel = document.querySelector<HTMLElement>("[data-ui-checkbox]");
    const sectionHeading = Array.from(document.querySelectorAll("h3")).find(
      (element) => element.textContent === "Recent activity",
    );
    const checkboxRect = checkbox?.getBoundingClientRect();
    return {
      checkboxWidth: checkboxRect?.width ?? 0,
      checkboxHeight: checkboxRect?.height ?? 0,
      checkboxAccent: checkbox ? getComputedStyle(checkbox).accentColor : "",
      checkboxFont: checkboxLabel ? getComputedStyle(checkboxLabel).fontFamily : "",
      headingFont: sectionHeading ? getComputedStyle(sectionHeading).fontFamily : "",
      viewportWidth: document.documentElement.clientWidth,
      documentWidth: document.documentElement.scrollWidth,
    };
  });

  expect(sharedStyles.checkboxWidth).toBe(16);
  expect(sharedStyles.checkboxHeight).toBe(16);
  expect(sharedStyles.checkboxAccent).toBe("rgb(107, 92, 231)");
  expect(sharedStyles.checkboxFont).toContain("Manrope");
  expect(sharedStyles.headingFont).toContain("Archivo");
  expect(sharedStyles.documentWidth).toBeLessThanOrEqual(sharedStyles.viewportWidth + 1);

  await verifyScreenshot(page, testInfo, "magento-design-system-desktop.png");

  await page.getByText("View details", { exact: true }).first().click();
  await expect(page.getByText("Requested as", { exact: true }).first()).toBeVisible();
  await expect(page.getByText("product bulk update", { exact: true })).toBeVisible();
  expect(browserErrors).toEqual([]);
});

test("Magento controls keep phone geometry and do not overflow", async ({ page }, testInfo) => {
  const browserErrors = captureBrowserErrors(page);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/admin/settings/packs/magento/operations", { waitUntil: "domcontentloaded" });
  await expect(page.getByRole("heading", { name: "Store operations", exact: true })).toBeVisible();
  await expect(page.getByText(/Version 2\.0\.0 · installed Aug 27, 2026/)).toBeVisible();
  await page.getByText("Edit limits", { exact: true }).first().click();

  const geometry = await page.evaluate(() => {
    const visible = (element: Element) => {
      const rect = element.getBoundingClientRect();
      return rect.width > 0 && rect.height > 0 && getComputedStyle(element).display !== "none";
    };
    const controls = Array.from(
      document.querySelectorAll<HTMLElement>(
        ":where([data-ui-button], [data-ui-field-control], [data-ui-checkbox], [data-ui-disclosure])",
      ),
    ).filter(visible);
    const enabledLabel = Array.from(document.querySelectorAll<HTMLElement>("[data-ui-checkbox]"))
      .find((element) => element.textContent?.trim() === "Enabled");
    return {
      viewportWidth: document.documentElement.clientWidth,
      documentWidth: document.documentElement.scrollWidth,
      minimumControlHeight: Math.min(...controls.map((element) => element.getBoundingClientRect().height)),
      enabledWhiteSpace: enabledLabel ? getComputedStyle(enabledLabel).whiteSpace : "",
      enabledHeight: enabledLabel?.getBoundingClientRect().height ?? 0,
    };
  });

  expect(geometry.documentWidth).toBeLessThanOrEqual(geometry.viewportWidth + 1);
  expect(geometry.minimumControlHeight).toBeGreaterThanOrEqual(44);
  expect(geometry.enabledWhiteSpace).toBe("nowrap");
  expect(geometry.enabledHeight).toBeGreaterThanOrEqual(44);

  await verifyScreenshot(page, testInfo, "magento-design-system-phone.png");
  expect(browserErrors).toEqual([]);
});
