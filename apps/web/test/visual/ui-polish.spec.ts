import { expect, test } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
});

test("history, nested confirmation and phone navigation stay above chrome without moving the page", async ({ page }) => {
  let deletes = 0;
  await page.route("**/api/onboarding/status", route => route.fulfill({ json: { state: "ready" } }));
  await page.route("**/api/work/threads**", route => {
    if (route.request().method() === "DELETE") deletes++;
    return route.fulfill({ json: { threads: [{ id: "layout-check", title: "Synthetic layout check",
      createdAt: "2026-09-01T00:00:00Z", updatedAt: "2026-09-01T00:00:00Z", lastMessageAt: "2026-09-01T00:00:00Z" }] } });
  });
  for (const width of [1280, 820, 390]) {
    await page.setViewportSize({ width, height: 844 });
    await page.goto("/work");
    const history = page.getByRole("button", { name: "History", exact: true, includeHidden: true });
    await expect(history).toBeVisible();
    const before = (await history.boundingBox())!;
    await history.click();
    const sheet = page.getByRole("dialog", { name: "Past work" });
    await expect(sheet).toBeInViewport();
    const geometry = await sheet.evaluate(element => {
      const bounds = element.getBoundingClientRect();
      return { position: getComputedStyle(element).position,
        top: bounds.top, right: bounds.right, bottom: bounds.bottom, width: innerWidth, height: innerHeight,
        clickable: element.contains(document.elementFromPoint(bounds.right - 30, bounds.top + 30)),
        scrimAboveChrome: element.contains(document.elementFromPoint(10, 10))
          || document.querySelector('[data-slot="sheet-overlay"]')!.contains(document.elementFromPoint(10, 10)) };
    });
    expect(geometry.position).toBe("fixed");
    expect(geometry.top).toBeGreaterThanOrEqual(0);
    expect(geometry.right).toBeLessThanOrEqual(geometry.width);
    expect(geometry.bottom).toBeLessThanOrEqual(geometry.height);
    expect(geometry.clickable).toBe(true);
    expect(geometry.scrimAboveChrome).toBe(true);
    expect((await history.boundingBox())!.x).toBeCloseTo(before.x, 1);
    expect((await history.boundingBox())!.y).toBeCloseTo(before.y, 1);
    await sheet.getByRole("button", { name: "Delete thread" }).click();
    const confirmation = page.getByRole("alertdialog");
    await expect(confirmation).toBeInViewport();
    expect(await confirmation.evaluate(element => {
      const bounds = element.getBoundingClientRect();
      return element.contains(document.elementFromPoint(bounds.left + bounds.width / 2, bounds.top + bounds.height / 2));
    })).toBe(true);
    await confirmation.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(confirmation).toHaveCount(0);
    await expect(sheet).toBeVisible();
    await expect(sheet.getByRole("link", { name: /Synthetic layout check/ })).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(sheet).toHaveCount(0);
    expect((await history.boundingBox())!.x).toBeCloseTo(before.x, 1);
    expect(deletes).toBe(0);

    if (width === 390) {
      const more = page.getByRole("button", { name: "More", exact: true });
      await more.click();
      const navigation = page.getByRole("dialog", { name: "More", exact: true });
      await expect(navigation).toBeInViewport();
      expect(await navigation.evaluate(element => {
        const bounds = element.getBoundingClientRect();
        return getComputedStyle(element).position === "fixed" && bounds.bottom <= innerHeight
          && element.contains(document.elementFromPoint(bounds.left + bounds.width / 2, bounds.bottom - 20))
          && document.querySelector('[data-slot="sheet-overlay"]')!.contains(document.elementFromPoint(10, 10));
      })).toBe(true);
      await page.keyboard.press("Escape");
      await expect(navigation).toHaveCount(0);
      await expect(more).toBeFocused();
    }
  }
});

test("skill counts stay readable and cancelling deletion preserves the skill", async ({ page }) => {
  let deletes = 0;
  await page.route("**/api/work/skills**", route => {
    if (route.request().method() === "DELETE") deletes++;
    return route.fulfill({ json: { skills: Array.from({ length: 22 }, (_, index) => ({
      name: `example-skill-${index}`, description: "Synthetic UI verification skill",
      fileCount: 12, updatedAt: "2026-09-01T00:00:00Z",
    })) } });
  });
  for (const width of [1280, 820, 390]) {
    await page.setViewportSize({ width, height: 844 });
    await page.goto("/skills");
    await expect(page.locator(".library-head-stats strong").first()).toHaveText("22");
    for (const count of await page.locator(".library-head-stats strong").all()) {
      expect(await count.evaluate(element => element.scrollWidth <= element.clientWidth)).toBe(true);
    }
    const skillAction = page.getByRole("button", { name: "Actions for example-skill-21", exact: true });
    await skillAction.scrollIntoViewIfNeeded();
    const pageBefore = (await page.locator(".library-page").boundingBox())!;
    const scrollBefore = await page.evaluate(() => window.scrollY);
    await skillAction.click();
    await page.getByRole("menuitem", { name: "Delete skill" }).click();
    const dialog = page.getByRole("alertdialog");
    await expect(dialog).toBeInViewport();
    const layout = await dialog.evaluate(element => {
      const bounds = element.getBoundingClientRect();
      const background = document.querySelector(".library-page")!.getBoundingClientRect();
      const overlay = document.querySelector('[data-slot="alert-dialog-overlay"]')!;
      return {
        position: getComputedStyle(element).position,
        centeredX: bounds.left + bounds.width / 2,
        centeredY: bounds.top + bounds.height / 2,
        width: innerWidth, height: innerHeight,
        backgroundX: background.x, backgroundY: background.y,
        aboveChrome: overlay.contains(document.elementFromPoint(10, 10)),
        clickable: element.contains(document.elementFromPoint(bounds.left + bounds.width / 2, bounds.top + bounds.height / 2)),
      };
    });
    expect(layout.position).toBe("fixed");
    expect(layout.centeredX).toBeCloseTo(layout.width / 2, 1);
    expect(layout.centeredY).toBeCloseTo(layout.height / 2, 1);
    expect(layout.backgroundX).toBeCloseTo(pageBefore.x, 1);
    expect(layout.backgroundY).toBeCloseTo(pageBefore.y, 1);
    expect(layout.aboveChrome).toBe(true);
    expect(layout.clickable).toBe(true);
    await page.getByRole("button", { name: "Cancel", exact: true }).click();
    await expect(dialog).toHaveCount(0);
    expect((await page.locator(".library-page").boundingBox())!.x).toBeCloseTo(pageBefore.x, 1);
    expect(await page.evaluate(() => window.scrollY)).toBeCloseTo(scrollBefore, 1);
    await expect(skillAction).toBeVisible();
    expect(deletes).toBe(0);
  }
  await page.getByRole("searchbox", { name: "Search skills" }).fill("no-matching-skill");
  await expect(page.getByText("No matching skills", { exact: true })).toBeVisible();
});

test("workflow drawer has a light header, padded shared actions and unobstructed phone controls", async ({ page }) => {
  const workflow = {
    id: "ui-polish", name: "Example workflow", description: "Synthetic workflow for layout checks.",
    goal: "Verify the UI", enabled: true, status: "active", cron: null, cronTimezone: "UTC",
    cronEnabled: false, steps: [], createdAt: "2026-09-01T00:00:00Z", updatedAt: "2026-09-01T00:00:00Z",
    systemPromptOverlay: "", dailyRunBudget: null, runsToday: 0, minutesSaved30d: 0,
    createdByThreadId: null, createdByRunId: null,
  };
  await page.route("**/api/workflows", route => route.fulfill({ json: { workflows: [workflow] } }));
  await page.route("**/api/workflows/ui-polish", route => route.fulfill({ json: {
    workflow, subscriptions: [], recentRuns: [], recentActions: [], activitySparkline: [],
  } }));
  await page.route("**/api/workflows/ui-polish/api-access", route => route.fulfill({ status: 403, json: { error: "Admin access required" } }));
  await page.route("**/api/policies", route => route.fulfill({ json: { policies: [] } }));
  for (const width of [1280, 1024, 901, 900, 820, 721, 720, 390]) {
    await page.setViewportSize({ width, height: 844 });
    await page.goto("/workflows?id=ui-polish");
    const run = page.getByRole("button", { name: "Run now", exact: true });
    await expect(run).toBeVisible();
    const geometry = await page.locator(".workflow-inspector").evaluate(panel => {
      const head = panel.querySelector(".workflow-inspector-head")!;
      const actions = panel.querySelector("[data-ui-action-group]")!;
      const rail = document.querySelector(".app-rail-wrap")!;
      const railRight = getComputedStyle(rail).display === "none" ? 0 : rail.getBoundingClientRect().right;
      const bounds = panel.getBoundingClientRect();
      return {
        headerBackground: getComputedStyle(head).backgroundColor,
        panelBackground: getComputedStyle(panel).backgroundColor,
        padding: parseFloat(getComputedStyle(actions).paddingLeft),
        gap: parseFloat(getComputedStyle(actions).gap),
        overflow: document.documentElement.scrollWidth > innerWidth,
        railRight,
        left: bounds.left,
        right: bounds.right,
        viewport: innerWidth,
        contentLeft: head.getBoundingClientRect().left,
      };
    });
    expect(geometry.headerBackground).toBe(geometry.panelBackground);
    expect(geometry.padding).toBeGreaterThanOrEqual(18);
    expect(geometry.gap).toBeGreaterThanOrEqual(8);
    expect(geometry.overflow).toBe(false);
    expect(geometry.left).toBeGreaterThanOrEqual(geometry.railRight);
    expect(geometry.contentLeft).toBeGreaterThanOrEqual(geometry.railRight);
    expect(geometry.right).toBeLessThanOrEqual(geometry.viewport);
    expect((await run.boundingBox())!.height).toBeCloseTo(width <= 720 ? 44 : 40, 2);
    if (width <= 900) await expect(page.locator(".workflow-inspector-scrim")).toBeVisible();
    if (width <= 720) await expect(page.locator(".cdock")).toBeHidden();
    await page.getByRole("button", { name: "Close workflow controls" }).click();
    await expect(page.locator(".workflow-inspector")).toHaveCount(0);
    if (width <= 720) await expect(page.locator(".cdock")).toBeVisible();
  }
});
