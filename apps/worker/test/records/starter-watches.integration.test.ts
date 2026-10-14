import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { db, eq, records_watch_binding } from "@neko/db";
import {
  createTestOrg,
  dbReachable,
  deleteTestOrg,
  uniqueOrgId,
} from "@neko/db/test-helpers";
import {
  buildRecordArtifactImportPlan,
  type AppRegistrySnapshot,
  type RecordsGraphjinTransport,
} from "@neko/records";
import {
  SALESFORCE_GOLDEN_DIRECTORY,
  salesforceGoldenArtifactFiles,
} from "../../../../packages/records/test/fixtures/salesforce-golden.js";
import { createRecordsWatchEvaluator } from "../../src/jobs/records-watch-evaluate.js";
import {
  createRecordsStarterWatchSeeder,
  enqueueRecordsWatchFallbackSweep,
  reconcileRecordsNativeWatchDeliveries,
  receiveRecordsNativeWatchEvent,
} from "../../src/records/starter-watches.js";

const reachable = await dbReachable();
const describeIfDb = reachable ? describe : describe.skip;

if (!reachable) {
  console.warn("[starter-watches] skipping: Postgres unreachable.");
}

describeIfDb("records starter watch lifecycle", () => {
  let orgId: string;
  const graphjinWatchId = "00000000-0000-4000-a000-000000000912";

  beforeEach(async () => {
    orgId = uniqueOrgId("starter-watch");
    await createTestOrg(orgId);
  });

  afterEach(async () => {
    await deleteTestOrg(orgId);
    vi.clearAllMocks();
  });

  async function seedOneWatch() {
    const graphjin: RecordsGraphjinTransport = {
      async execute<T>(input): Promise<T> {
        if (input.operationName === "FindRecordsNativeWatch") {
          return { gj_watch: [] } as T;
        }
        if (input.operationName === "UpsertRecordsNativeWatch") {
          return {
            gj_watch: [
              {
                id: graphjinWatchId,
                name: "watch",
                status: "paused",
                approval: "pending",
                enabled: false,
                action_hash: "b".repeat(64),
                action_approval: "pending",
              },
            ],
          } as T;
        }
        if (input.operationName === "ReviewRecordsNativeWatchAction") {
          return {
            gj_watch: [
              {
                id: graphjinWatchId,
                status: "active",
                approval: "approved",
                enabled: true,
                action_hash: "b".repeat(64),
                action_approval: "approved",
              },
            ],
          } as T;
        }
        throw new Error(`unexpected operation ${input.operationName}`);
      },
    };
    const seed = createRecordsStarterWatchSeeder({
      graphjin,
      token: () => "watch-service-token",
      webhookUrl: "http://172.19.0.5:4100/admin/events/records-watch",
    });
    await expect(
      seed({
        orgId,
        appId: "crm",
        actionPayload: {
          preview_hash: "a".repeat(64),
          starter_workflows: [
            { key: "opportunities_without_activity", enabled: true },
            { key: "deals_closing_this_month", enabled: false },
          ],
        },
      }),
    ).resolves.toEqual({ seeded: ["opportunities_without_activity"] });
    const [binding] = await db()
      .select()
      .from(records_watch_binding)
      .where(eq(records_watch_binding.graphjin_watch_id, graphjinWatchId));
    expect(binding).toMatchObject({
      org_id: orgId,
      app_id: "crm",
      watch_key: "opportunities_without_activity",
      status: "active",
      enabled: true,
    });
    return binding!;
  }

  it("persists a native event receipt once and schedules the fallback path", async () => {
    const binding = await seedOneWatch();
    const enqueued = vi.fn(async () => "job-id");

    await expect(
      receiveRecordsNativeWatchEvent(
        { watchId: graphjinWatchId, eventId: "event-1", payload: { changed: 1 } },
        enqueued,
      ),
    ).resolves.toEqual({ accepted: true });
    await expect(
      receiveRecordsNativeWatchEvent(
        { watchId: graphjinWatchId, eventId: "event-1", payload: { changed: 1 } },
        enqueued,
      ),
    ).resolves.toEqual({ accepted: false });
    expect(enqueued).toHaveBeenCalledTimes(1);
    expect(enqueued).toHaveBeenCalledWith(
      {
        orgId,
        bindingId: binding.id,
        triggerKind: "gj_watch",
        eventId: "event-1",
      },
      "records-watch-event:event-1",
    );

    const scheduled = vi.fn(async () => "job-id");
    await expect(enqueueRecordsWatchFallbackSweep(orgId, scheduled)).resolves.toBe(1);
    expect(scheduled).toHaveBeenCalledWith(
      { orgId, bindingId: binding.id, triggerKind: "schedule" },
      expect.stringMatching(new RegExp(`^records-watch-schedule:${binding.id}:\\d+$`)),
    );

    const rebind = vi
      .fn()
      .mockResolvedValueOnce({
        gj_watch: [{
          id: graphjinWatchId,
          action_hash: "c".repeat(64),
          action_approval: "pending",
        }],
      })
      .mockResolvedValueOnce({
        gj_watch: [{
          id: graphjinWatchId,
          action_hash: "c".repeat(64),
          action_approval: "approved",
        }],
      });
    await expect(
      reconcileRecordsNativeWatchDeliveries({
        graphjin: { execute: rebind },
        token: () => "watch-service-token",
        webhookUrl: "http://172.19.0.8:4100/admin/events/records-watch",
      }),
    ).resolves.toBe(1);
    expect(rebind).toHaveBeenCalledWith(
      expect.objectContaining({
        operationName: "UpdateRecordsNativeWatchDelivery",
        token: "watch-service-token",
      }),
    );

    // Transport failures must REJECT (not resolve partially): the worker's
    // boot path relies on that contract to catch, warn, and retry in the
    // background instead of crash-looping while records-watch-graphjin is
    // still coming up.
    const down = vi.fn(async () => {
      throw new Error("connect ECONNREFUSED records-watch-graphjin:8090");
    });
    await expect(
      reconcileRecordsNativeWatchDeliveries({
        graphjin: { execute: down },
        token: () => "watch-service-token",
        webhookUrl: "http://172.19.0.8:4100/admin/events/records-watch",
      }),
    ).rejects.toThrow(/ECONNREFUSED/);
  });

  it("runs the same deterministic evaluator for native and scheduled triggers", async () => {
    const binding = await seedOneWatch();
    const graphjin: RecordsGraphjinTransport = {
      async execute<T>(input): Promise<T> {
        expect(input.operationName).toBe("EvaluateRecordsStaleOpportunities");
        expect(input.variables).toEqual({ cutoff: "2026-07-03T12:00:00.000Z" });
        return {
          opportunities: [
            {
              id: "opportunity-1",
              owner_user_id: "owner-1",
              data: { name: "Needs attention", stage: "proposal", close_date: "2026-08-20" },
            },
          ],
          activities: [],
        } as T;
      },
    };
    const publish = vi.fn(async () => undefined);
    const evaluate = createRecordsWatchEvaluator({
      graphjin,
      token: () => "watch-service-token",
      now: () => new Date("2026-08-02T12:00:00.000Z"),
      publish,
    });

    await expect(
      evaluate({
        orgId,
        bindingId: binding.id,
        triggerKind: "gj_watch",
        eventId: "event-2",
      }),
    ).resolves.toEqual({ findings: 1 });
    await expect(
      evaluate({ orgId, bindingId: binding.id, triggerKind: "schedule" }),
    ).resolves.toEqual({ findings: 1 });

    expect(publish).toHaveBeenCalledTimes(2);
    expect(publish.mock.calls[0]?.[0]).toMatchObject({
      orgId,
      workflowId: binding.workflow_id,
      triggerKind: "watcher",
      triggerPayload: { eventId: "event-2" },
      finding: { topic: "opportunities_without_activity", mood: "act" },
    });
    expect(publish.mock.calls[1]?.[0]).toMatchObject({
      orgId,
      workflowId: binding.workflow_id,
      triggerKind: "schedule",
      finding: { topic: "opportunities_without_activity", mood: "act" },
    });
  });

  it("runs every seed in both trigger modes against the golden migration shape", async () => {
    const staged = salesforceGoldenArtifactFiles();
    const plan = await buildRecordArtifactImportPlan({
      directory: SALESFORCE_GOLDEN_DIRECTORY,
      app: "crm",
      label: "CRM",
      readFile: async (path) => {
        const bytes = staged.get(path);
        if (!bytes) throw new Error(`missing golden artifact ${path}`);
        return bytes;
      },
    });
    const objects = plan.definition.objects.map((object, index) => ({
      id: `golden-object-${index}`,
      orgId,
      appId: plan.definition.appId,
      apiName: object.apiName,
      sourceApiName: object.sourceApiName,
      label: object.label,
      pluralLabel: object.pluralLabel,
      tableSchema: "public" as const,
      tableName: object.tableName,
      nameField: object.nameField,
      visibility: object.visibility,
      custom: object.custom,
      archivedAt: null,
      recordCount: null,
    }));
    const snapshot: AppRegistrySnapshot = {
      revision: "golden",
      app: {
        orgId,
        appId: plan.definition.appId,
        label: plan.definition.label,
        purpose: plan.definition.purpose,
        status: "active",
        navOrder: plan.definition.navOrder,
        registryRevision: "golden",
      },
      objects,
      fields: plan.definition.objects.flatMap((object, objectIndex) =>
        object.fields.map((field, fieldIndex) => ({
          id: `golden-field-${objectIndex}-${fieldIndex}`,
          orgId,
          objectId: objects[objectIndex]!.id,
          apiName: field.apiName,
          sourceApiName: field.sourceApiName,
          label: field.label,
          kind: field.kind,
          columnName: field.columnName,
          required: field.required,
          readOnly: field.readOnly,
          archivedAt: null,
          picklistValues: field.picklistValues,
          referenceTargets: field.referenceTargets,
          length: field.length,
          scale: field.scale,
        })),
      ),
      layouts: [],
      pages: [],
      permissions: [],
    };
    const sampleRows = (objectApiName: string) => {
      const item = plan.imports.find((candidate) => candidate.objectApiName === objectApiName);
      if (!item) throw new Error(`missing golden import ${objectApiName}`);
      return item.sampleRows;
    };
    const columnFor = (objectApiName: string, sourceApiName: string): string => {
      const object = snapshot.objects.find((candidate) => candidate.apiName === objectApiName);
      const field = snapshot.fields.find(
        (candidate) => candidate.objectId === object?.id && candidate.sourceApiName === sourceApiName,
      );
      if (!field) throw new Error(`missing golden field ${objectApiName}.${sourceApiName}`);
      return String(field.columnName);
    };
    const opportunityRows = sampleRows("opportunity");
    const taskRows = sampleRows("task");
    const eventRows = sampleRows("event");
    const userRows = sampleRows("user_nk");
    let watchNumber = 0;
    const evaluationQueries: string[] = [];
    const graphjin: RecordsGraphjinTransport = {
      async execute<T>(input): Promise<T> {
        if (input.operationName === "FindRecordsNativeWatch") {
          return { gj_watch: [] } as T;
        }
        if (input.operationName === "UpsertRecordsNativeWatch") {
          watchNumber += 1;
          return {
            gj_watch: [
              {
                id: `golden-watch-${watchNumber}`,
                status: "paused",
                approval: "pending",
                enabled: false,
                action_hash: watchNumber.toString(16).padStart(64, "0"),
                action_approval: "pending",
              },
            ],
          } as T;
        }
        if (input.operationName === "ReviewRecordsNativeWatchAction") {
          return {
            gj_watch: [
              {
                id: `golden-watch-${watchNumber}`,
                status: "active",
                approval: "approved",
                enabled: true,
                action_hash: watchNumber.toString(16).padStart(64, "0"),
                action_approval: "approved",
              },
            ],
          } as T;
        }
        evaluationQueries.push(input.query);
        if (input.operationName === "EvaluateRecordsStaleOpportunities") {
          return {
            opportunities: opportunityRows.map((row) => ({
              id: row.Id,
              owner_user_id: row.OwnerId,
              data: {
                [columnFor("opportunity", "Name")]: row.Name,
                [columnFor("opportunity", "StageName")]: row.StageName,
                [columnFor("opportunity", "CloseDate")]: row.CloseDate,
              },
            })),
            activity_0: eventRows.map((row) => ({
              id: row.Id,
              data: {
                [columnFor("event", "WhatId")]: row.WhatId,
                [columnFor("event", "StartDateTime")]: row.StartDateTime,
              },
            })),
            activity_1: taskRows.map((row) => ({
              id: row.Id,
              data: {
                [columnFor("task", "WhatId")]: row.WhatId,
                [columnFor("task", "LastModifiedDate")]: row.LastModifiedDate,
              },
            })),
          } as T;
        }
        if (input.operationName === "EvaluateRecordsOwnerMappings") {
          return {
            identities: userRows.map((row) => ({
              source_instance_id: "salesforce-golden-production",
              source_user_id: row.Id,
              source_email: row.Email,
              source_is_active: true,
              app_user_id: null,
              status: "conflict",
              updated_at: "2026-08-02T12:00:00.000Z",
            })),
          } as T;
        }
        if (input.operationName === "EvaluateRecordsClosingDeals") {
          return {
            opportunities: opportunityRows.map((row) => ({
              id: row.Id,
              owner_user_id: row.OwnerId,
              data: {
                [columnFor("opportunity", "Name")]: row.Name,
                [columnFor("opportunity", "StageName")]: row.StageName,
                [columnFor("opportunity", "Amount")]: row.Amount,
                [columnFor("opportunity", "CloseDate")]: row.CloseDate,
              },
            })),
          } as T;
        }
        throw new Error(`unexpected operation ${input.operationName}`);
      },
    };
    const seed = createRecordsStarterWatchSeeder({
      graphjin,
      token: () => "watch-service-token",
      webhookUrl: "http://172.19.0.5:4100/admin/events/records-watch",
    });
    await expect(
      seed({
        orgId,
        appId: "crm",
        actionPayload: {
          preview_hash: "b".repeat(64),
          starter_workflows: [
            { key: "opportunities_without_activity", enabled: true },
            { key: "unlinked_or_departed_owners", enabled: true },
            { key: "deals_closing_this_month", enabled: true },
          ],
        },
      }),
    ).resolves.toEqual({
      seeded: [
        "opportunities_without_activity",
        "unlinked_or_departed_owners",
        "deals_closing_this_month",
      ],
    });

    const bindings = await db()
      .select()
      .from(records_watch_binding)
      .where(eq(records_watch_binding.org_id, orgId));
    const publish = vi.fn(async () => undefined);
    const evaluate = createRecordsWatchEvaluator({
      graphjin,
      token: () => "watch-service-token",
      registry: { loadApp: async () => snapshot },
      now: () => new Date("2026-08-02T12:00:00.000Z"),
      publish,
    });
    for (const binding of bindings) {
      await expect(
        evaluate({
          orgId,
          bindingId: binding.id,
          triggerKind: "gj_watch",
          eventId: `golden-${binding.watch_key}`,
        }),
      ).resolves.toEqual({ findings: 1 });
      await expect(
        evaluate({ orgId, bindingId: binding.id, triggerKind: "schedule" }),
      ).resolves.toEqual({ findings: 1 });
    }

    expect(publish).toHaveBeenCalledTimes(6);
    expect(publish.mock.calls.map((call) => call[0].finding.topic).sort()).toEqual([
      "deals_closing_this_month",
      "deals_closing_this_month",
      "opportunities_without_activity",
      "opportunities_without_activity",
      "unlinked_or_departed_owners",
      "unlinked_or_departed_owners",
    ]);
    expect(evaluationQueries.join("\n")).toContain("crm__opportunity(limit: 500) { id owner_user_id data }");
    expect(evaluationQueries.join("\n")).toContain("crm__task");
    expect(evaluationQueries.join("\n")).toContain("crm__event");
  });
});
