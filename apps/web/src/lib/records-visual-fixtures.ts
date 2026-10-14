import "server-only";

import {
  validateRecordIdentifier,
  type RecordObjectView,
  type RecordFieldKind,
  type RecordSavedView,
  type RecordSavedViewDefinition,
  type RecordViewColumn,
} from "@neko/records";
import type { RecordAskScenario } from "@/components/records/RecordAskBox";
import type { RecordListResult } from "@/lib/records";
import { recordReferenceIdentityKey } from "@/lib/records-reference";

const id = validateRecordIdentifier;
const FIXED_AT = "2026-08-03T12:00:00.000Z";

type VisualListFixture = {
  kind: "active";
  result: RecordListResult;
  substrate: {
    availability: "active";
    reason: null;
    config: Record<string, unknown>;
    unlinkedIdentities: number;
    latestSync: { watermark: unknown; updatedAt: string } | null;
  };
  actor: { role: "admin" | "member" };
  views: RecordSavedView[];
  selectedView: RecordSavedView | null;
  filterFields: Array<{
    apiName: string;
    label: string;
    kind: RecordFieldKind;
    picklistValues: unknown[] | null;
    referenceTargets?: string[] | null;
  }>;
  nameLabel: string;
  definition: RecordSavedViewDefinition;
  page: number;
  size: number;
  base: string;
  query: Record<string, string | undefined>;
  askScenario?: RecordAskScenario;
};

function column(
  apiName: string,
  label: string,
  kind: RecordViewColumn["kind"],
  options: Partial<RecordViewColumn> = {},
): RecordViewColumn {
  return {
    apiName,
    columnName: apiName,
    label,
    kind,
    required: false,
    readOnly: false,
    picklistValues: null,
    referenceTargets: null,
    scale: null,
    ...options,
  };
}

function crmFixture(): VisualListFixture {
  const stageValues = [
    { value: "discovery", label: "Discovery", color: "slate", semantic: "open" },
    { value: "qualification", label: "Qualified", color: "violet", semantic: "open" },
    { value: "proposal", label: "Proposal", color: "amber", semantic: "open" },
    { value: "negotiation", label: "Negotiation", color: "amber", emphasis: "strong", semantic: "open" },
    { value: "closed_won", label: "Closed Won", color: "green", emphasis: "strong", semantic: "closed" },
  ];
  const view: RecordObjectView = {
    object: {
      id: "visual-object-opportunity",
      orgId: "visual-org",
      appId: "crm",
      apiName: id("opportunity"),
      sourceApiName: "Opportunity",
      label: "Opportunity",
      pluralLabel: "Opportunities",
      tableSchema: id("public"),
      tableName: id("crm__opportunity"),
      nameField: id("name"),
      visibility: "owner",
      custom: false,
      archivedAt: null,
      recordCount: "1,290",
    },
    permission: {
      appId: "crm",
      role: "member",
      objectApiName: id("opportunity"),
      canRead: true,
      canCreate: true,
      canUpdate: true,
      canDelete: false,
    },
    columns: [
      column("name", "Opportunity", "text", { required: true }),
      column("account", "Account", "reference", { referenceTargets: ["account"] }),
      column("stage", "Stage", "picklist", { picklistValues: stageValues }),
      column("amount", "Amount", "currency", { scale: 0 }),
      column("close_date", "Close", "date"),
      column("owner_user_id", "Owner", "owner", { readOnly: true }),
      column("last_activity_at", "Last activity", "datetime", { readOnly: true }),
    ],
  };
  const rows: Array<Record<string, unknown>> = [
    { id: "006-meridian", name: "Renewal FY27", account: "001-meridian", stage: "negotiation", amount: 184000, close_date: "2026-08-14", owner_user_id: "kavya", last_activity_at: "2026-08-02T12:00:00.000Z" },
    { id: "006-castellan", name: "Cold-chain expansion", account: "001-castellan", stage: "proposal", amount: 96500, close_date: "2026-08-22", owner_user_id: "kavya", last_activity_at: "2026-07-31T12:00:00.000Z" },
    { id: "006-bluewater-pilot", name: "Fleet telematics pilot", account: "001-bluewater", stage: "qualification", amount: 41200, close_date: "2026-09-02", owner_user_id: "rui", last_activity_at: "2026-07-28T12:00:00.000Z" },
    { id: "006-halvorsen", name: "Warehouse retrofit — Phase 2", account: "001-halvorsen", stage: "negotiation", amount: 220000, close_date: "2026-09-09", owner_user_id: "sf-j-keller", last_activity_at: "2026-02-12T10:00:00.000Z" },
    { id: "006-meridian-addon", name: "Route optimization add-on", account: "001-meridian", stage: "qualification", amount: 28750, close_date: "2026-09-15", owner_user_id: "rui", last_activity_at: "2026-08-01T12:00:00.000Z" },
    { id: "006-ostrander", name: "Packaging line sensors", account: "001-ostrander", stage: "proposal", amount: 67900, close_date: "2026-09-18", owner_user_id: "dana", last_activity_at: FIXED_AT },
    { id: "006-bluewater-tender", name: "Port authority tender", account: "001-bluewater", stage: "discovery", amount: 310000, close_date: "2026-09-26", owner_user_id: "dana", last_activity_at: "2026-07-27T12:00:00.000Z" },
    { id: "006-seasonal", name: "Seasonal capacity contract", account: "001-castellan", stage: "closed_won", amount: 54300, close_date: "2026-07-28", owner_user_id: "kavya", last_activity_at: "2026-07-30T12:00:00.000Z" },
  ];
  const references = Object.fromEntries([
    ["001-meridian", "Meridian Logistics"],
    ["001-castellan", "Castellan Foods"],
    ["001-bluewater", "Bluewater Marine"],
    ["001-halvorsen", "Halvorsen & Berg"],
    ["001-ostrander", "Ostrander Dairy Co-op"],
  ].map(([recordId, label]) => [
    recordReferenceIdentityKey("account", recordId),
    { id: recordId, label, objectApiName: "account" },
  ]));
  const pending = {
    id: "visual-action-meridian",
    kind: "record_update" as const,
    status: "pending_approval" as const,
    summary: "Push the Meridian renewal close date out two weeks",
    fields: {
      close_date: "2026-08-28",
      next_step: "Follow up after redlines, week of Aug 18",
    },
    expected: {
      close_date: "2026-08-14",
      next_step: "Contract redlines",
    },
    createdAt: FIXED_AT,
  };
  const definition: RecordSavedViewDefinition = {
    version: 1,
    filter: {
      op: "and",
      clauses: [
        { field: "stage", operator: "is_open" },
        { field: "close_date", operator: "relative", value: { macro: "this_quarter" } },
      ],
    },
    sort: { field: "close_date", direction: "asc" },
    search: null,
    myRecords: false,
    columns: ["name", "account", "stage", "amount", "close_date"],
  };
  const selectedView: RecordSavedView = {
    id: "visual-view-open-pipeline",
    label: "Open pipeline",
    ownerUserId: null,
    shared: true,
    definition,
    createdAt: FIXED_AT,
    updatedAt: FIXED_AT,
  };
  return {
    kind: "active",
    result: {
      rows,
      cursor: "visual-next-page",
      total: 214,
      view,
      app: {
        orgId: "visual-org",
        appId: "crm",
        label: "CRM",
        purpose: "Customer relationships and pipeline",
        status: "active",
        navOrder: 20,
        registryRevision: "38",
      },
      owners: {
        kavya: { userId: "kavya", label: "Kavya M.", email: "kavya@example.com", status: "linked" },
        rui: { userId: "rui", label: "Rui T.", email: "rui@example.com", status: "linked" },
        dana: { userId: "dana", label: "Dana A.", email: "dana@example.com", status: "linked" },
        "sf-j-keller": { userId: "sf-j-keller", label: "SF: j.keller", email: "j.keller@example.com", status: "unlinked" },
      },
      references,
      pendingActions: { "006-meridian": [pending] },
    },
    substrate: {
      availability: "active",
      reason: null,
      config: { registryRevision: 38, import: { source: "salesforce", records: 214382 } },
      unlinkedIdentities: 3,
      latestSync: { watermark: { at: FIXED_AT }, updatedAt: "2026-08-03T10:00:00.000Z" },
    },
    actor: { role: "member" },
    views: [selectedView],
    selectedView,
    filterFields: [
      { apiName: "stage", label: "Stage", kind: "picklist", picklistValues: stageValues },
      { apiName: "close_date", label: "Close date", kind: "date", picklistValues: null },
    ],
    nameLabel: "Opportunity",
    definition,
    page: 1,
    size: 50,
    base: "/a/crm/opportunity",
    query: {
      sort: "close_date",
      direction: "asc",
      filter: JSON.stringify(definition.filter),
      view: selectedView.id,
      mine: "false",
    },
    askScenario: {
      request: "Push the Meridian renewal close date out two weeks and log yesterday’s call with their ops director.",
      response: "Found Renewal FY27 — Meridian Logistics, owned by you. One update needs approval; the activity can auto-fire under your rule.",
      autoAction: "Activity — call with Dana Okafor logged · auto per rule",
    },
  };
}

function supportFixture(): VisualListFixture {
  const objectId = "visual-object-support-case";
  const columns = [
    column("subject", "Request summary", "text", { required: true }),
    column("status", "Status supplied by an external support system", "picklist", {
      picklistValues: [
        { value: "new", label: "New", color: "blue" },
        { value: "in_progress", label: "In progress", color: "violet" },
        { value: "closed", label: "Closed", color: "green" },
      ],
    }),
    column("requester_email", "Requester email address", "email"),
    column("requester_phone", "Requester telephone number", "phone"),
    column("reported_on", "Date first reported", "date"),
    column("first_response_minutes", "Minutes until first response", "integer"),
    column("refund_amount", "Refund amount authorized", "currency", { scale: 2 }),
    column("sla_consumed", "Service-level allowance consumed", "percent", { scale: 1 }),
    column("needs_follow_up", "Requires another follow-up", "boolean"),
    column("tags", "Routing and diagnostic tags", "multipicklist", {
      picklistValues: ["billing", "delivery", "technical"],
    }),
    column("help_center_url", "Suggested help-center article", "url"),
  ];
  const view: RecordObjectView = {
    object: {
      id: objectId,
      orgId: "visual-org",
      appId: "support_lab",
      apiName: id("support_case"),
      sourceApiName: "Case",
      label: "Support case",
      pluralLabel: "Customer support requests requiring coordinated investigation across fulfillment, billing, and field service teams",
      tableSchema: id("public"),
      tableName: id("support_lab__support_case"),
      nameField: id("subject"),
      visibility: "org",
      custom: true,
      archivedAt: null,
      recordCount: "1",
    },
    permission: {
      appId: "support_lab",
      role: "admin",
      objectApiName: id("support_case"),
      canRead: true,
      canCreate: true,
      canUpdate: true,
      canDelete: true,
    },
    columns,
  };
  const definition: RecordSavedViewDefinition = {
    version: 1,
    filter: null,
    sort: { field: "subject", direction: "asc" },
    search: null,
    myRecords: false,
    columns: columns.map((item) => item.apiName),
  };
  return {
    kind: "active",
    result: {
      rows: [{
        id: "case/with spaces and unicode-雪",
        subject: "Package arrived damaged after three unsuccessful delivery attempts",
        status: "waiting_on_vendor",
        requester_email: "requester@example.com",
        requester_phone: null,
        reported_on: "2026-07-28",
        first_response_minutes: 0,
        refund_amount: 1250.5,
        sla_consumed: 87.3,
        needs_follow_up: false,
        tags: ["billing", "not-in-registry", "technical", "vip"],
        help_center_url: "javascript:alert('not a link')",
      }],
      cursor: null,
      total: 1,
      view,
      app: {
        orgId: "visual-org",
        appId: "support_lab",
        label: "Support Lab",
        purpose: "Adversarial renderer fixture",
        status: "active",
        navOrder: 30,
        registryRevision: "19",
      },
      owners: {},
      references: {},
      pendingActions: {},
    },
    substrate: {
      availability: "active",
      reason: null,
      config: { registryRevision: 19 },
      unlinkedIdentities: 0,
      latestSync: null,
    },
    actor: { role: "admin" },
    views: [],
    selectedView: null,
    filterFields: columns.flatMap((item) =>
      item.kind === "owner" || item.kind === "system_datetime"
        ? []
        : [{
            apiName: item.apiName,
            label: item.label,
            kind: item.kind,
            picklistValues: item.picklistValues,
          }],
    ),
    nameLabel: "Subject",
    definition,
    page: 1,
    size: 50,
    base: "/a/support_lab/support_case",
    query: { sort: "subject", direction: "asc" },
  };
}

export function recordsVisualTestEnabled(): boolean {
  return (
    process.env.NODE_ENV !== "production" &&
    (process.env.OPENNEKO_RECORDS_VISUAL_TEST === "true" ||
      process.env.NEXT_PUBLIC_RECORDS_VISUAL_TEST === "true")
  );
}

export function loadRecordsVisualListFixture(
  appId: string,
  objectApiName: string,
): VisualListFixture | null {
  if (!recordsVisualTestEnabled()) return null;
  if (appId === "crm" && objectApiName === "opportunity") return crmFixture();
  if (appId === "support_lab" && objectApiName === "support_case") return supportFixture();
  return null;
}

export type { VisualListFixture };
