import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn(), replace: vi.fn() }),
}));
import { validateRecordIdentifier, type RecordObjectView } from "@neko/records";
import { RecordDetailCard } from "@/components/records/RecordDetailCard";
import {
  RecordChangeTimeline,
  RecordRelatedLists,
} from "@/components/records/RecordDetailActivity";

const id = validateRecordIdentifier;

function view(): RecordObjectView {
  return {
    object: {
      id: "object-account",
      orgId: "org-a",
      appId: "crm",
      apiName: id("account"),
      sourceApiName: null,
      label: "Account",
      pluralLabel: "Accounts",
      tableSchema: id("public"),
      tableName: id("crm__account"),
      nameField: id("name"),
      visibility: "org",
      custom: false,
      archivedAt: null,
      recordCount: "1",
    },
    permission: {
      appId: "crm",
      role: "admin",
      objectApiName: id("account"),
      canRead: true,
      canCreate: true,
      canUpdate: true,
      canDelete: true,
    },
    columns: [
      {
        apiName: "name",
        columnName: "name",
        label: "Account name",
        kind: "text",
        required: true,
        readOnly: false,
        picklistValues: null,
        referenceTargets: null,
        scale: null,
      },
      {
        apiName: "industry",
        columnName: "industry",
        label: "Industry",
        kind: "picklist",
        required: false,
        readOnly: false,
        picklistValues: [
          {
            value: "technology",
            label: "Technology",
            color: "green",
            emphasis: "strong",
          },
        ],
        referenceTargets: null,
        scale: null,
      },
      {
        apiName: "account_manager",
        columnName: "account_manager",
        label: "Account manager",
        kind: "reference",
        required: false,
        readOnly: false,
        picklistValues: null,
        referenceTargets: ["contact"],
        scale: null,
      },
    ],
  };
}

describe("record detail metadata surfaces", () => {
  it("renders registry sections, related lists, and bound change history", () => {
    const current = view();
    const detail = renderToStaticMarkup(createElement(RecordDetailCard, {
      appId: "crm",
      view: current,
      row: {
        id: "account-1",
        name: "Meridian",
        industry: "technology",
        account_manager: "contact-1",
      },
      owners: {},
      references: {
        ["account_manager\u0000contact-1"]: {
          id: "contact-1",
          label: "Kavya Menon",
          objectApiName: "contact",
        },
      },
      layout: {
        sections: [
          { label: "Identity", fields: ["name"] },
          { label: "Classification", fields: ["industry"] },
        ],
      },
    }));
    expect(detail).toContain("Identity");
    expect(detail).toContain("Classification");
    expect(detail.indexOf("Account name")).toBeLessThan(detail.indexOf("Industry"));
    expect(detail).toContain("Kavya Menon");
    expect(detail).toContain("records-pill is-green is-emphasis");
    expect(detail).toContain('/a/crm/contact/contact-1');
    expect(detail).not.toContain(">contact-1</a>");

    const related = renderToStaticMarkup(createElement(RecordRelatedLists, {
      appId: "crm",
      lists: [{
        label: "Child accounts",
        fieldApiName: "parent",
        view: current,
        rows: [{ id: "account-2", name: "Meridian India", industry: "Technology" }],
        owners: {},
        references: {},
      }],
    }));
    expect(related).toContain("Child accounts");
    expect(related).toContain("/a/crm/account/account-2");

    const history = renderToStaticMarkup(createElement(RecordChangeTimeline, {
      view: current,
      history: [{
        id: "17",
        action: "update",
        actorUserId: "admin-1",
        actionRequestId: "request-1",
        changes: { industry: { old: "Retail", new: "Technology" } },
        at: "2026-08-03T12:00:00.000Z",
      }],
    }));
    expect(history).toContain("Change history");
    expect(history).toContain("Retail");
    expect(history).toContain("Technology");
    expect(history).not.toContain("mutation_id");
  });
});
