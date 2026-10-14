import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn() }),
}));
import { RecordSchemaEditor } from "@/components/records/RecordSchemaEditor";

const field = (apiName: string, label: string, kind: "text" | "picklist" | "date" = "text") => ({
  apiName,
  label,
  kind,
  required: apiName === "title",
  readOnly: false,
  picklistValues: kind === "picklist" ? ["linkedin", "facebook"] : [],
  referenceTargets: [],
});

describe("RecordSchemaEditor", () => {
  it("lists fields, protects the name field and offers unplaced fields for the record page", () => {
    const html = renderToStaticMarkup(
      createElement(RecordSchemaEditor, {
        model: {
          revision: "3",
          appId: "social",
          appLabel: "Social calendar",
          objects: [
            {
              apiName: "post",
              label: "Post",
              pluralLabel: "Posts",
              nameField: "title",
              fields: [field("title", "Title"), field("platform", "Platform", "picklist"), field("budget", "Budget")],
              listColumns: ["title", "platform"],
              detailSections: [{ label: "Post", fields: ["title", "platform"] }],
            },
          ],
        },
      }),
    );
    expect(html).toContain("Choice list");
    expect(html).toContain("2 choices");
    expect(html).toContain("Record name");
    expect(html).toContain('aria-label="More actions for Platform"');
    expect(html).not.toContain('aria-label="More actions for Title"');
    expect(html).toContain("One field is not on the page yet.");
    expect(html).toContain("Add a field to this section…");
    expect(html).toContain("Show another column…");
  });
});
