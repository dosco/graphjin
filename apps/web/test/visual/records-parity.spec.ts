import { expect, test } from "@playwright/test";

test.beforeEach(async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce", colorScheme: "light" });
  await page.clock.setFixedTime(new Date("2026-08-03T12:00:00.000Z"));
});

test("CRM generated records and toolbar match the pinned baseline", async ({
  page,
}) => {
  await page.goto("/a/crm/opportunity", { waitUntil: "networkidle" });

  await expect(page.getByRole("heading", { name: "Opportunities" })).toBeVisible();
  await expect(page.getByText("Meridian Logistics").first()).toBeVisible();
  await expect(page.getByText("Pending change", { exact: true })).toBeVisible();
  await expect(page.getByText("SF: j.keller")).toBeVisible();
  await expect(page.getByText("Unlinked", { exact: true })).toBeVisible();
  // Contextual chat now lives in AppChatSidebar, outside this list fixture.
  await expect(page.getByRole("searchbox", { name: /^Search Opportunities/ })).toBeVisible();
  await expect(page.getByRole("complementary", { name: "Records substrate status" })).toContainText("Healthy");
  const controls = await page.locator(".records-object-header").evaluate((header) => {
    const search = header.querySelector("[data-slot=input-group]")!;
    const action = header.querySelector(".records-primary-action")!;
    return [search, action].map((element) => {
      const { height, top } = element.getBoundingClientRect();
      return { height, top };
    });
  });
  expect(controls[0].height).toBe(40);
  expect(controls[1].height).toBe(40);
  expect(Math.abs(controls[0].top - controls[1].top)).toBeLessThan(1);

  await expect(page.locator(".records-list-main")).toHaveScreenshot(
    "crm-generated-content.png",
  );
  await expect(page).toHaveScreenshot("crm-complete-scenario.png", {
    fullPage: true,
  });
});

test("non-CRM adversarial fixture stays generic and visually stable", async ({ page }) => {
  await page.goto("/a/support_lab/support_case", { waitUntil: "networkidle" });

  await expect(
    page.getByRole("heading", {
      name: "Customer support requests requiring coordinated investigation across fulfillment, billing, and field service teams",
    }),
  ).toBeVisible();
  await expect(page.getByText("waiting_on_vendor")).toBeVisible();
  await expect(page.locator(".records-null")).toContainText("—");
  await expect(page.getByText("+2")).toBeVisible();
  await expect(page.getByText("Owner", { exact: true })).toHaveCount(0);
  await expect(page.getByText("INTERNAL ONLY")).toHaveCount(0);
  await expect(page.locator('a[href^="javascript:"]')).toHaveCount(0);

  await expect(page.locator(".records-list-main")).toHaveScreenshot(
    "support-adversarial-generated-content.png",
  );
  const tableScroll = page.locator(".records-table-scroll");
  await tableScroll.evaluate((element) => {
    element.scrollLeft = element.scrollWidth;
  });
  await expect(page.locator(".records-list-card")).toHaveScreenshot(
    "support-adversarial-table-tail.png",
  );
});

for (const viewport of [
  { name: "tablet", width: 900, height: 1_100 },
  { name: "phone", width: 390, height: 844 },
] as const) {
  test(`generated record pages stay inside the ${viewport.name} viewport`, async ({
    page,
  }) => {
    await page.setViewportSize(viewport);
    await page.goto("/a/support_lab/support_case", {
      waitUntil: "networkidle",
    });

    const heading = page.getByRole("heading", {
      name: "Customer support requests requiring coordinated investigation across fulfillment, billing, and field service teams",
    });
    await expect(heading).toBeVisible();

    const layout = await page.evaluate(() => {
      const title = document.querySelector<HTMLElement>(
        ".records-object-title h1",
      );
      const tableScroll = document.querySelector<HTMLElement>(
        ".records-table-scroll",
      );
      const titleStyle = title ? getComputedStyle(title) : null;
      const titleRect = title?.getBoundingClientRect();
      const sharedControls = Array.from(
        document.querySelectorAll<HTMLElement>(
          ":where([data-ui-button], .ui-button)",
        ),
      )
        .filter((element) => {
          const style = getComputedStyle(element);
          const rect = element.getBoundingClientRect();
          return style.display !== "none" && rect.width > 0 && rect.height > 0;
        })
        .map((element) => Math.round(element.getBoundingClientRect().height));

      return {
        viewportWidth: document.documentElement.clientWidth,
        documentWidth: document.documentElement.scrollWidth,
        titleRight: titleRect?.right ?? 0,
        titleLeft: titleRect?.left ?? 0,
        titleOverflow: titleStyle?.overflow,
        titleTextOverflow: titleStyle?.textOverflow,
        tableOverflowX: tableScroll ? getComputedStyle(tableScroll).overflowX : null,
        sharedControls,
      };
    });

    expect(layout.documentWidth).toBeLessThanOrEqual(layout.viewportWidth + 1);
    expect(layout.titleLeft).toBeGreaterThanOrEqual(0);
    expect(layout.titleRight).toBeLessThanOrEqual(layout.viewportWidth + 1);
    expect(layout.titleOverflow).not.toBe("hidden");
    expect(layout.titleTextOverflow).not.toBe("ellipsis");
    expect(layout.tableOverflowX).toBe("auto");

    if (viewport.name === "phone") {
      expect(layout.sharedControls.length).toBeGreaterThan(0);
      expect(Math.min(...layout.sharedControls)).toBeGreaterThanOrEqual(44);
    }
  });
}
