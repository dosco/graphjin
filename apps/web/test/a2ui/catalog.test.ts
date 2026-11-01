import { describe, expect, it } from "vitest";
import { CATALOG_ID, WORK_CATALOG_ID, ComponentTypes } from "@/a2ui/catalog";

describe("A2UI catalog", () => {
  it("CATALOG_ID is a non-empty URN", () => {
    expect(CATALOG_ID.length).toBeGreaterThan(0);
    expect(CATALOG_ID).toMatch(/^urn:/);
  });

  it("publishes a distinct catalog for v1 conversational surfaces", () => {
    expect(WORK_CATALOG_ID).toBe("urn:openneko:catalog:work:v2");
  });

  it("ComponentTypes keys equal their string values (no typos)", () => {
    for (const [key, value] of Object.entries(ComponentTypes)) {
      expect(value).toBe(key);
    }
  });

  it("includes the components the answer surface can render", () => {
    expect(ComponentTypes).toMatchObject({
      Answer: "Answer",
      MetricCard: "MetricCard",
      Chart: "Chart",
      Confirmation: "Confirmation",
      Markdown: "Markdown",
      Table: "Table",
      Section: "Section",
      Callout: "Callout",
      Choice: "Choice",
      Divider: "Divider",
      Text: "Text",
      Row: "Row",
      Column: "Column",
      Tabs: "Tabs",
      Button: "Button",
      TextField: "TextField",
      CheckBox: "CheckBox",
      ChoicePicker: "ChoicePicker",
      Conditional: "Conditional",
      OpenApiSpecInput: "OpenApiSpecInput",
      ManagedFileSourceInput: "ManagedFileSourceInput",
    });
  });

  it("keeps Briefing/BriefingCard as dashboard + back-compat aliases", () => {
    expect(ComponentTypes).toMatchObject({
      Briefing: "Briefing",
      BriefingCard: "BriefingCard",
    });
  });
});
