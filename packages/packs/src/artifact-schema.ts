import { z } from "zod";
import type { PackArtifactKind } from "./bundle.js";

const artifactKey = z.string().regex(/^[a-z][a-z0-9_]*\.[a-z][a-z0-9_.-]*$/);
const targetRef = z.string().regex(/^[a-z][a-z0-9_.-]*$/);
const base = {
  key: artifactKey,
  targetRef,
};

const sourceSchema = z
  .object({
    key: artifactKey.optional(),
    name: targetRef,
    kind: z.enum(["database", "api"]),
  })
  .loose();

const sourceFileSchema = z
  .object({
    sources: z.array(sourceSchema).min(1),
  })
  .strict();

const metricSchema = z
  .object({
    ...base,
    title: z.string().min(1),
    description: z.string().min(1),
    role: z.string().min(1),
    chartHint: z.string().min(1),
    unit: z.string().min(1),
    directionGood: z.enum(["up", "down", "neutral"]),
    cadence: z.string().min(1),
    calculationNote: z.string().min(1),
    execution: z
      .object({
        mode: z.literal("saved_query"),
        source: z.string().min(1),
        query: z.string().min(1),
        result: z.record(z.string(), z.unknown()),
        freshnessSeconds: z.number().int().positive(),
        variables: z.record(z.string(), z.unknown()).optional(),
      })
      .strict(),
  })
  .strict();

const workflowSchema = z
  .object({
    ...base,
    name: z.string().min(1),
    description: z.string(),
    enabled: z.boolean(),
    status: z.literal("active"),
    goal: z.string().min(1),
    schedule: z
      .object({
        cron: z.string().min(1),
        timezoneInput: z.string().min(1),
        enabled: z.boolean(),
      })
      .strict()
      .nullable(),
    networkHosts: z.array(z.string().min(1)).optional(),
    activation: z.string().optional(),
    outputContract: z
      .object({
        kind: z.string().min(1),
        required: z.array(z.string()).min(1),
      })
      .strict(),
  })
  .strict();

const watcherSchema = z
  .object({
    ...base,
    name: z.string().min(1),
    description: z.string(),
    workflow: artifactKey,
    enabled: z.boolean(),
    activation: z.string().min(1),
    query: z.string().min(1),
    valuePath: z.string().min(1),
    operator: z.enum(["gt", "gte", "lt", "lte", "eq", "ne"]),
    threshold: z.union([z.number(), z.string(), z.boolean(), z.null()]),
    cadenceSeconds: z.number().int().positive(),
    debounceSeconds: z.number().int().nonnegative(),
    cooldownSeconds: z.number().int().nonnegative(),
    severity: z.enum(["low", "medium", "high", "critical"]),
    dedupeKey: z.string().min(1),
    readinessSignals: z.array(z.string()).optional(),
    variables: z.record(z.string(), z.unknown()).optional(),
  })
  .strict();

const mappingLeafSchema: z.ZodType<unknown> = z.union([
  z.object({ input: z.string().min(1) }).strict(),
  z.object({ packInput: z.string().min(1) }).strict(),
  z.object({ const: z.union([z.string(), z.number(), z.boolean(), z.null()]) }).strict(),
]);

const mappingSchema: z.ZodType<unknown> = z.lazy(() =>
  z.union([mappingLeafSchema, z.record(z.string(), mappingSchema)]),
);

const legacyActionSchema = z
  .object({
    ...base,
    kind: targetRef,
    description: z.string().min(1),
    readiness: z
      .object({
        capability: z.string().min(1),
        unavailableReasons: z.array(z.string()).min(1),
      })
      .strict(),
    inputSchema: z.record(z.string(), z.unknown()),
    adapter: z
      .object({
        kind: z.literal("graphjin_api_operation"),
        source: z.string().min(1),
        operationId: z.string().min(1),
        mutationRoot: z.string().min(1),
        preconditionQuery: z.string().min(1),
        mutationQuery: z.string().min(1),
        reconciliationQuery: z.string().min(1),
        request: z
          .object({
            path: z.record(z.string(), mappingSchema),
            body: z.record(z.string(), mappingSchema),
          })
          .strict(),
        receipt: z
          .object({
            fields: z.array(z.string()),
            redact: z.array(z.string()),
          })
          .strict(),
        ambiguousOutcome: z.literal("reconcile_required"),
        retry: z.literal("never"),
      })
      .strict(),
  })
  .strict();

const graphjinApiOperationSchema = z
  .object({
    operationId: z.string().min(1),
    mutationRoot: z.string().regex(/^[_A-Za-z][_0-9A-Za-z]*$/),
    reversible: z.boolean().default(false),
    // A header that the API spec declares for this operation, such as
    // Idempotency-Key. The runtime sends the action request id in it, so a
    // retried request cannot act twice.
    idempotencyHeader: z.string().regex(/^[A-Za-z0-9-]+$/).optional(),
  })
  .strict();

// Payload fields the policy engine reads as the action's targets. A field is a
// dot path into the payload, such as body.to. email_domain reads the domain of
// each address, so policies can allow or deny recipient domains.
const actionTargetsSchema = z
  .object({
    fields: z.array(z.string().regex(/^[A-Za-z0-9_]+(\.[A-Za-z0-9_]+)*$/)).min(1),
    as: z.enum(["value", "email_domain"]).optional(),
  })
  .strict();

const graphjinApiActionSchema = z
  .object({
    ...base,
    kind: targetRef,
    description: z.string().min(1),
    readiness: z
      .object({
        capability: z.literal("operator"),
        unavailableReasons: z.array(z.string()).min(1),
      })
      .strict(),
    inputSchema: z.record(z.string(), z.unknown()),
    example: z.record(z.string(), z.unknown()).optional(),
    targets: actionTargetsSchema.optional(),
    adapter: z
      .object({
        kind: z.literal("graphjin_api_operation"),
        source: z.string().min(1),
        spec: z.string().min(1),
        operations: z.record(z.string().min(1), graphjinApiOperationSchema),
        // Body fields the agent cannot choose. Each one is set from a pack
        // input, such as the sender address, and replaces any value in the
        // payload.
        lockedBody: z
          .record(z.string().regex(/^[A-Za-z0-9_]+$/), z.object({ input: z.string().min(1) }).strict())
          .optional(),
        ambiguousOutcome: z.literal("reconcile_required"),
        retry: z.literal("never"),
      })
      .strict(),
  })
  .strict();

const magentoOperationSchema = z
  .object({
    operationId: z.string().min(1),
    mutationRoot: z.string().regex(/^[_A-Za-z][_0-9A-Za-z]*$/),
    readRoot: z.string().regex(/^[_A-Za-z][_0-9A-Za-z]*$/).optional(),
    readOperationId: z.string().min(1).optional(),
    readPath: z.string().startsWith("/V1/").optional(),
    bodyKey: z.string().min(1).optional(),
    inverseOperation: z.string().min(1).optional(),
    entityIdField: z.string().min(1).optional(),
    defaultClass: z.union([z.literal(1), z.literal(2)]),
    entityType: z.string().min(1),
    reversible: z.boolean(),
    numericPathParams: z.array(z.string().min(1)).optional(),
    resultMode: z.enum(["sync", "async_bulk"]).default("sync"),
  })
  .strict();

const magentoGovernedAdapterSchema = z
  .object({
    kind: z.enum(["magento_governed_operation", "magento_changeset"]),
    source: z.literal("magento_operator"),
    spec: z.literal("magento-operator-v2"),
    operations: z.record(z.string().min(1), magentoOperationSchema),
    ambiguousOutcome: z.literal("reconcile_required"),
    retry: z.literal("never"),
  })
  .strict();

const magentoHandoffAdapterSchema = z
  .object({
    kind: z.literal("magento_financial_handoff"),
    handoffKinds: z
      .array(
        z.enum([
          "online_refund",
          "return_approval",
          "financial_configuration",
          "store_credit_over_cap",
        ]),
      )
      .min(1),
  })
  .strict();

const magentoV2ActionSchema = z
  .object({
    ...base,
    kind: targetRef,
    description: z.string().min(1),
    domain: z.enum([
      "catalog",
      "inventory",
      "orders",
      "promotions",
      "content",
      "customers",
    ]),
    readiness: z
      .object({
        capability: z.literal("operator"),
        domain: z.enum([
          "catalog",
          "inventory",
          "orders",
          "promotions",
          "content",
          "customers",
        ]),
        unavailableReasons: z.array(z.string()).min(1),
      })
      .strict(),
    inputSchema: z.record(z.string(), z.unknown()),
    adapter: z.union([
      magentoGovernedAdapterSchema,
      magentoHandoffAdapterSchema,
    ]),
  })
  .strict();

const actionSchema = z.union([legacyActionSchema, graphjinApiActionSchema, magentoV2ActionSchema]);

const policySchema = z
  .object({
    ...base,
    name: z.string().min(1),
    description: z.string(),
    appliesToKinds: z.array(targetRef).min(1),
    appliesToScopes: z.array(targetRef).min(1),
    mode: z.enum(["ask", "auto", "draft", "never"]),
    approverRole: z.literal("admin").optional(),
    priority: z.number().int(),
    enabled: z.boolean(),
    allowedTargets: z.record(z.string(), z.unknown()),
    limits: z.record(z.string(), z.unknown()),
  })
  .strict();

const relationshipsSchema = z
  .object({
    ...base,
    source: z.string().min(1),
    relationships: z
      .array(
        z
          .object({
            left: z.string().min(1),
            right: z.string().min(1),
            cardinality: z.enum(["one_to_one", "one_to_many", "many_to_one", "many_to_many"]),
          })
          .strict(),
      ),
  })
  .strict();

const schemas: Partial<Record<PackArtifactKind, z.ZodType>> = {
  source: sourceFileSchema,
  metric: metricSchema,
  workflow: workflowSchema,
  watcher: watcherSchema,
  action: actionSchema,
  policy: policySchema,
  relationships: relationshipsSchema,
};

export function validateArtifactContent(kind: PackArtifactKind, content: unknown, path: string): void {
  const schema = schemas[kind];
  if (schema) {
    const parsed = schema.safeParse(content);
    if (!parsed.success) {
      throw new Error(`invalid ${kind} artifact ${path}: ${z.prettifyError(parsed.error)}`);
    }
  }
  if (kind === "spec") {
    const spec = content as Record<string, unknown> | null;
    if (!spec || typeof spec !== "object" || typeof spec.openapi !== "string" || !spec.paths) {
      throw new Error(`invalid OpenAPI artifact ${path}`);
    }
  }
}
