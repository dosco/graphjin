import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it, vi } from "vitest";

vi.mock("next/navigation", () => ({
  useRouter: () => ({ push: vi.fn(), refresh: vi.fn(), replace: vi.fn() }),
}));
import {
  buildRecordDetailQuery,
  buildRecordListQuery,
  validateRecordIdentifier,
  type AppRegistrySnapshot,
  type RecordField,
  type RecordFieldKind,
} from "@neko/records";
import { RecordDetailCard } from "@/components/records/RecordDetailCard";
import { RecordTable } from "@/components/records/RecordTable";

const identifier = validateRecordIdentifier;
const OBJECT_ID = "object-support-case";
const LONG_OBJECT_LABEL =
  "Customer support requests requiring coordinated investigation across fulfillment billing and field service teams";
const LONG_FIELD_LABEL =
  "Detailed diagnostic narrative supplied by the requester including every attempted workaround and the exact observed outcome";

function field(
  apiName: string,
  label: string,
  kind: RecordFieldKind,
  options: Partial<Pick<RecordField, "archivedAt" | "picklistValues" | "readOnly" | "required" | "scale">> = {},
): RecordField {
  return {
    id: `field-${apiName}`,
    orgId: "org-adversarial",
    objectId: OBJECT_ID,
    apiName: identifier(apiName),
    sourceApiName: null,
    label,
    kind,
    columnName: identifier(apiName),
    required: options.required ?? false,
    readOnly: options.readOnly ?? false,
    archivedAt: options.archivedAt ?? null,
    picklistValues: options.picklistValues ?? null,
    referenceTargets: null,
    length: kind === "text" || kind === "textarea" ? 500 : null,
    scale: options.scale ?? null,
  };
}

function adversarialSnapshot(): AppRegistrySnapshot {
  const visibleFields = [
    field("subject", "Request summary", "text", { required: true }),
    field("status", "Status supplied by an external support system", "picklist", {
      picklistValues: [
        { value: "new", label: "New" },
        { value: "in_progress", label: "In progress" },
        { value: "closed", label: "Closed" },
      ],
    }),
    field("requester_email", "Requester email address", "email"),
    field("requester_phone", "Requester telephone number", "phone"),
    field("reported_on", "Date first reported", "date"),
    field("last_contact_at", "Most recent customer contact", "datetime"),
    field("first_response_minutes", "Minutes until first response", "integer"),
    field("satisfaction", "Customer satisfaction score", "decimal", { scale: 1 }),
    field("refund_amount", "Refund amount authorized", "currency", { scale: 2 }),
    field("sla_consumed", "Service-level allowance consumed", "percent", { scale: 1 }),
    field("needs_follow_up", "Requires another follow-up", "boolean"),
    field("tags", "Routing and diagnostic tags", "multipicklist", {
      picklistValues: ["billing", "delivery", "technical"],
    }),
    field("help_center_url", "Suggested help-center article", "url"),
    field("diagnostic_summary", LONG_FIELD_LABEL, "textarea"),
    field("resolution_health", "Calculated resolution health", "readonly_formula", {
      readOnly: true,
    }),
  ];
  return {
    revision: "19",
    app: {
      orgId: "org-adversarial",
      appId: "support_lab",
      label: "Support Lab",
      purpose: "Renderer acceptance fixture",
      status: "active",
      navOrder: 0,
      registryRevision: "19",
    },
    objects: [
      {
        id: OBJECT_ID,
        orgId: "org-adversarial",
        appId: "support_lab",
        apiName: identifier("support_case"),
        sourceApiName: "Case",
        label: "Support case",
        pluralLabel: LONG_OBJECT_LABEL,
        tableSchema: identifier("public"),
        tableName: identifier("support_lab__support_case"),
        nameField: identifier("subject"),
        visibility: "org",
        custom: false,
        archivedAt: null,
        recordCount: "1",
      },
    ],
    fields: [
      ...visibleFields,
      field("hidden_internal_note", "Internal note that is not in either layout", "text"),
      field("legacy_resolution", "Archived legacy resolution", "text", {
        archivedAt: new Date("2025-01-01T00:00:00.000Z"),
      }),
    ],
    layouts: [
      {
        objectId: OBJECT_ID,
        kind: "list",
        definition: {
          columns: [
            "subject",
            "status",
            "requester_email",
            "requester_phone",
            "reported_on",
            "first_response_minutes",
            "refund_amount",
            "sla_consumed",
            "needs_follow_up",
            "tags",
            "help_center_url",
            "legacy_resolution",
          ],
        },
      },
      {
        objectId: OBJECT_ID,
        kind: "detail",
        definition: {
          sections: [
            { fields: visibleFields.slice(0, 8).map((item) => item.apiName) },
            { fields: visibleFields.slice(8).map((item) => ({ field: item.apiName })) },
          ],
        },
      },
    ],
    pages: [],
    permissions: [
      {
        appId: "support_lab",
        role: "admin",
        objectApiName: identifier("support_case"),
        canRead: true,
        canCreate: true,
        canUpdate: true,
        canDelete: true,
      },
    ],
  };
}

const adversarialRow: Record<string, unknown> = {
  id: "case/with spaces and unicode-雪",
  subject: "Package arrived damaged after three unsuccessful delivery attempts",
  status: "waiting_on_vendor",
  requester_email: "requester@example.com",
  requester_phone: null,
  reported_on: "2026-07-28",
  last_contact_at: "2026-08-02T12:45:00.000Z",
  first_response_minutes: 0,
  satisfaction: "4.5",
  refund_amount: "1250.5",
  sla_consumed: "87.3",
  needs_follow_up: false,
  tags: ["billing", "not-in-registry", "technical", "vip"],
  help_center_url: "javascript:alert('not a link')",
  diagnostic_summary:
    "The issue persists after cache clearing, a device restart, and a second network test.",
  resolution_health: "Needs attention",
  hidden_internal_note: "INTERNAL ONLY — MUST NOT RENDER",
  legacy_resolution: "Old field — MUST NOT RENDER",
  nk_updated_at: "2026-08-02T13:00:00.000Z",
};

describe("generated records renderer adversarial fixture", () => {
  it("renders a wide list with null and unknown values without exposing hidden fields", () => {
    const built = buildRecordListQuery({
      snapshot: adversarialSnapshot(),
      objectApiName: "support_case",
      role: "admin",
      userId: "admin-1",
    });
    const html = renderToStaticMarkup(
      createElement(RecordTable, {
        appId: "support_lab",
        view: built.view,
        rows: [adversarialRow],
        owners: {},
        total: 1,
        cursor: null,
        page: 1,
        query: {},
      }),
    );

    expect(built.view.columns).toHaveLength(11);
    expect(html).toContain(LONG_OBJECT_LABEL);
    expect(html).toContain("waiting_on_vendor");
    expect(html).toContain('class="records-null">—</span>');
    expect(html).toContain("+2");
    expect(html).not.toContain("Owner");
    expect(html).not.toContain("INTERNAL ONLY");
    expect(html).not.toContain("Old field");
    expect(built.query).not.toContain("hidden_internal_note");
    expect(built.query).not.toContain("legacy_resolution");
  });

  it("renders the many-field detail layout and keeps malformed URLs inert", () => {
    const built = buildRecordDetailQuery({
      snapshot: adversarialSnapshot(),
      objectApiName: "support_case",
      role: "admin",
      recordId: String(adversarialRow.id),
    });
    const html = renderToStaticMarkup(
      createElement(RecordDetailCard, {
        appId: "support_lab",
        view: built.view,
        row: adversarialRow,
        owners: {},
      }),
    );

    expect(built.view.columns).toHaveLength(16);
    expect(html).toContain(LONG_FIELD_LABEL);
    expect(html).toContain("Needs attention");
    expect(html).toContain("javascript:alert(&#x27;not a link&#x27;)");
    expect(html).not.toContain('href="javascript:');
    expect(html).not.toContain("INTERNAL ONLY");
    expect(html).not.toContain("Archived legacy resolution");
    expect(html).not.toContain("Owner");
    expect(built.query).not.toContain("hidden_internal_note");
    expect(built.query).not.toContain("legacy_resolution");
  });
});
