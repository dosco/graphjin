import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";
import { validateRecordIdentifier, type RecordObjectView } from "@neko/records";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn() }),
}));
import { RecordTable } from "@/components/records/RecordTable";

const id = validateRecordIdentifier;

const view: RecordObjectView = {
  object: {
    id: "object-post",
    orgId: "org-a",
    appId: "social",
    apiName: id("post"),
    sourceApiName: null,
    label: "Post",
    pluralLabel: "Posts",
    tableSchema: id("public"),
    tableName: id("social__post"),
    nameField: id("title"),
    visibility: "org",
    custom: false,
    archivedAt: null,
    recordCount: "120",
  },
  permission: { appId: "social", role: "admin", objectApiName: id("post"), canRead: true, canCreate: true, canUpdate: true, canDelete: true },
  columns: [
    { apiName: "title", columnName: "title", label: "Title", kind: "text", required: true, readOnly: false, picklistValues: null, referenceTargets: null, scale: null },
  ],
};

function render(query: Record<string, string | undefined>, page: number, cursor: string | null) {
  const rows = Array.from({ length: 25 }, (_, index) => ({ id: `r${index}`, title: `Post ${index}` }));
  return renderToStaticMarkup(
    createElement(RecordTable, { appId: "social", view, rows, owners: {}, total: 120, cursor, page, pageSize: 25, query }),
  ).replaceAll("&amp;", "&");
}

describe("RecordTable pager", () => {
  it("links back through the cursor trail and forward with the next cursor", () => {
    const html = render({ after: "c2", page: "3", back: "c1", size: "25" }, 3, "c3");
    expect(html).toContain("Showing 51–75 of 120");
    expect(html).toContain('rel="prev" href="/a/social/post?after=c1&page=2&size=25"');
    expect(html).toContain('rel="next" href="/a/social/post?after=c3&page=4&back=c1%2Cc2&size=25"');
  });

  it("returns to the first page without a cursor", () => {
    const html = render({ after: "c1", page: "2", size: "25" }, 2, "c2");
    expect(html).toContain('rel="prev" href="/a/social/post?size=25"');
    expect(html).toContain('rel="next" href="/a/social/post?after=c2&page=3&size=25&back=c1"');
  });

  it("disables previous on the first page", () => {
    const html = render({}, 1, "c1");
    expect(html).toMatch(/aria-disabled="true"[^>]*>.*Previous/);
  });
});
