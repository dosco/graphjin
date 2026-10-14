import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
}));
import { RecordViewBar } from "@/components/records/RecordViewBar";

describe("RecordViewBar", () => {
  it("renders semantic filter labels instead of an opaque count", () => {
    const filter = {
      op: "and" as const,
      clauses: [
        { field: "stage", operator: "is_open" as const },
        {
          field: "close_date",
          operator: "relative" as const,
          value: { macro: "this_quarter" },
        },
      ],
    };
    const html = renderToStaticMarkup(createElement(RecordViewBar, {
      base: "/a/crm/opportunity",
      objectLabel: "Opportunities",
      query: { filter: JSON.stringify(filter) },
      ownerScoped: true,
      appId: "crm",
      objectApiName: "opportunity",
      views: [],
      selectedView: null,
      definition: {
        version: 1,
        filter,
        sort: null,
        search: null,
        myRecords: false,
        columns: [],
      },
      fields: [
        {
          apiName: "stage",
          label: "Stage",
          kind: "picklist",
          picklistValues: [],
        },
        {
          apiName: "close_date",
          label: "Close date",
          kind: "date",
          picklistValues: null,
        },
      ],
      canShare: false,
    }));

    expect(html).toContain("Stage <b>is open</b>");
    expect(html).toContain("Close date <b>is this quarter</b>");
    expect(html).not.toContain("2 active filters");
    expect(html).toContain('aria-label="Remove filter: Stage is open"');
    expect(html).toContain("Clear filters");
    expect(html).toContain("Match all");
  });
});
