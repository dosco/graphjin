import { readFile } from "node:fs/promises";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";

const here = dirname(fileURLToPath(import.meta.url));
const adminSource = resolve(
  here,
  "../src/app/admin/settings/packs/MagentoPackAdmin.tsx",
);

describe("Magento pack administration copy", () => {
  it("uses approval and automation outcomes instead of numeric risk labels", async () => {
    const source = await readFile(adminSource, "utf8");

    expect(source).not.toMatch(/\bclass\s*[012]\b/i);
    expect(source).toContain("Administrator approval required");
    expect(source).toContain("Automatic under the configured store limits");
    expect(source).toContain("Allow routine changes to run automatically");
    expect(source).toContain("Actions OpenNeko will not perform");
  });

  it("uses shared controls and typography for Magento settings", async () => {
    const source = await readFile(adminSource, "utf8");

    expect(source).toContain('from "@/components/ui/checkbox"');
    expect(source).toContain('from "@/components/ui/field"');
    expect(source).toContain('from "@/components/ui/disclosure"');
    expect(source).toContain('from "@/components/ui/badge"');
    expect(source).toContain("<Checkbox");
    expect(source).toContain("<Field");
    expect(source).toContain("<Input");
    expect(source).toContain("<Disclosure");
    expect(source).toContain("<Badge");
    expect(source).not.toMatch(/<(?:button|input|select|textarea|details|summary)\b/);
    expect(source).not.toMatch(/\bconst\s+(?:FIELD|LABEL|HELP|INPUT)\b/);
    expect(source).toContain("font-display text-ui-body font-bold text-text");
  });

  it("presents change history as plain activity with technical details on demand", async () => {
    const source = await readFile(adminSource, "utf8");

    expect(source).toContain("Recent activity");
    expect(source).toContain("item.currentState");
    expect(source).toContain("Test activity");
    expect(source).toContain("rules.filter((item) => !item.isTest)");
    expect(source).toContain("View details");
    expect(source).not.toContain("Recent change-sets and handoffs");
    expect(source).not.toContain("Magento operator skills");
    expect(source).not.toContain('" · inverse"');
  });
});
