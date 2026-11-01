import { expect, test } from "@playwright/test";

// The worker integration suite supplies a real PackService, PostgreSQL,
// packaged GraphJin, HTTP provider, and archive. No pack API is substituted.
test.skip(!process.env.OPENNEKO_PACK_UI_ARCHIVE, "run through the pack connector integration suite");

test("uploads, reviews, corrects a failed install, and installs through Admin Packs", async ({ page }, testInfo) => {
  const errors: string[] = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.emulateMedia({ reducedMotion: "reduce" });
  await page.goto("/admin/settings/packs");
  await expect(page.getByRole("heading", { name: "Packs", exact: true })).toBeVisible();
  const section = page.getByRole("region", { name: "Solution packs", exact: true });
  await expect(section.getByText("Loading packs…")).toBeHidden();
  await page.getByText("Upload a custom pack", { exact: true }).click();
  await page.getByLabel("Pack archive", { exact: true }).setInputFiles({ name: "bad.zip", mimeType: "application/zip", buffer: Buffer.from("invalid ZIP") });
  await section.getByRole("button", { name: "Upload pack", exact: true }).click();
  await expect(section.getByRole("alert")).toBeVisible();
  await expect(section.getByRole("alert")).toBeFocused();
  await page.screenshot({ path: testInfo.outputPath("pack-upload-error-desktop.png") });
  await page.setViewportSize({ width: 390, height: 844 });
  await section.screenshot({ path: testInfo.outputPath("pack-upload-error-phone.png") });
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.getByLabel("Pack archive", { exact: true }).setInputFiles(process.env.OPENNEKO_PACK_UI_ARCHIVE!);
  await section.getByRole("button", { name: "Upload pack", exact: true }).click();
  await expect(section.getByRole("heading", { name: "Service health", exact: true })).toBeVisible();
  await page.getByLabel("Service base url", { exact: true }).fill(process.env.OPENNEKO_PACK_UI_PROVIDER!);
  await page.getByLabel("Data connection", { exact: true }).selectOption(process.env.OPENNEKO_PACK_UI_SOURCE!);
  await page.getByLabel("Service api token", { exact: true }).fill("wrong-fixture-token");
  await section.getByRole("button", { name: "Review changes", exact: true }).click();
  await expect(section.getByRole("button", { name: "Approve and install" })).toBeVisible();
  // Any edit revokes the displayed review, so its old approval cannot be used.
  await page.getByLabel("Service timezone", { exact: true }).fill("Asia/Kolkata");
  await expect(section.getByRole("button", { name: "Approve and install" })).toHaveCount(0);
  await section.getByRole("button", { name: "Review changes", exact: true }).click();
  await section.getByRole("button", { name: "Approve and install" }).click();
  await expect(section.getByRole("button", { name: "Applying…" })).toBeDisabled();
  await expect(page.getByLabel("Service api token", { exact: true })).toBeDisabled();
  await expect(section.getByRole("alert")).toContainText("Check the connection and credentials", { timeout: 60_000 });
  await page.getByText("Error details", { exact: true }).click();
  await expect(section.getByText(/failed preflight/)).toBeVisible();
  await page.getByText("Error details", { exact: true }).click();
  await page.screenshot({ path: testInfo.outputPath("pack-install-error-desktop.png") });
  await page.setViewportSize({ width: 390, height: 844 });
  await section.screenshot({ path: testInfo.outputPath("pack-install-error-phone.png") });
  await page.setViewportSize({ width: 1280, height: 900 });
  await page.getByLabel("Service api token", { exact: true }).fill(process.env.OPENNEKO_PACK_UI_TOKEN!);
  await section.getByRole("button", { name: "Review changes", exact: true }).click();
  await page.getByText("Configuration and change details", { exact: true }).click();
  await page.getByRole("button", { name: "Approve and install" }).focus();
  await page.keyboard.press("Tab");
  await page.keyboard.press("Shift+Tab");
  const focus = await page.getByRole("button", { name: "Approve and install" }).evaluate(element => {
    const style = getComputedStyle(element);
    return { focused: element === document.activeElement, outline: style.outlineStyle, shadow: style.boxShadow };
  });
  expect(focus.focused).toBe(true);
  expect(focus.outline !== "none" || focus.shadow !== "none").toBe(true);
  await page.getByText("Configuration and change details", { exact: true }).click();
  await section.screenshot({ path: testInfo.outputPath("pack-reviewed-desktop.png") });
  await page.setViewportSize({ width: 390, height: 844 });
  const geometry = await section.evaluate(element => {
    const controls = [...element.querySelectorAll<HTMLElement>("[data-ui-button], [data-ui-field-control], [data-ui-checkbox], [data-ui-disclosure]")].filter(control => control.getBoundingClientRect().height > 0);
    return { width: document.documentElement.scrollWidth, viewport: innerWidth, minimumHeight: Math.min(...controls.map(control => control.getBoundingClientRect().height)) };
  });
  expect(geometry.width).toBeLessThanOrEqual(geometry.viewport + 1);
  expect(geometry.minimumHeight).toBeGreaterThanOrEqual(44);
  await section.screenshot({ path: testInfo.outputPath("pack-reviewed-phone.png") });
  await section.getByRole("button", { name: "Approve and install" }).click();
  await expect(section.getByText("Installed", { exact: true })).toBeVisible({ timeout: 60_000 });
  await expect(page.getByLabel("Service api token", { exact: true })).toHaveValue("");
  await page.reload();
  await page.getByLabel("Uploaded pack", { exact: true }).selectOption("service-health");
  await expect(section.getByText("Installed", { exact: true })).toBeVisible();
  await expect(page.getByLabel("Service timezone", { exact: true })).toHaveValue("Asia/Kolkata");
  await section.screenshot({ path: testInfo.outputPath("pack-installed-phone.png") });
  await section.getByRole("button", { name: "Remove pack", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "Remove this pack?" })).toBeVisible();
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(section.getByRole("button", { name: "Remove pack", exact: true })).toBeFocused();
  await expect(section.getByText("Installed", { exact: true })).toBeVisible();
  expect(errors).toEqual([]);
});
