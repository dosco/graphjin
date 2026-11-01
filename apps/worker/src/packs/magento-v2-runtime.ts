import {
  action_changeset,
  action_changeset_row,
  and,
  data_source,
  db,
  desc,
  eq,
  magento_attribute_classification,
  magento_auto_rule,
  magento_financial_handoff,
  magento_store_control,
  pack_action_definition,
  pack_install,
  sql,
} from "@neko/db";
import { graphjinQuery, mintGraphjinToken, resolvePackSource, type PackSourceSelection } from "@neko/llm/graphjin";
import {
  autoApprovePreparedActionRequest,
  recordAuditEvent,
  registerActionAdapter,
  registerActionRequestCreatedHook,
  updateActionRequestPayload,
  type ActionAdapter,
  hasHumanActionApproval,
  type ActionRequestRecord,
} from "@neko/llm/workflows";
import {
  canonicalHash,
  classifyMagentoChange,
  DEFAULT_MAGENTO_ATTRIBUTE_CLASSIFICATIONS,
  evaluateMagentoCaps,
  magentoExecutionMode,
  type MagentoAttributeClassification,
  type MagentoDomain,
  type MagentoRiskClass,
} from "@neko/packs";
import { readSecretsStore } from "@neko/secrets";

const PACK_ID = "magento";
const PACK_SECRET_SECTION = "pack.magento";
const TOKEN_KEY = "MAGENTO_INTEGRATION_TOKEN";
const MAX_RESPONSE_BYTES = 2 * 1024 * 1024;

type MagentoOperation = {
  operationId: string;
  mutationRoot: string;
  readPath?: string;
  bodyKey?: string;
  inverseOperation?: string;
  entityIdField?: string;
  defaultClass: 1 | 2;
  entityType: string;
  reversible: boolean;
  numericPathParams?: string[];
  resultMode?: "sync" | "async_bulk";
};

type MagentoActionDefinition = {
  kind: string;
  domain: MagentoDomain;
  adapter: {
    kind: "magento_changeset" | "magento_governed_operation" | "magento_financial_handoff";
    operations?: Record<string, MagentoOperation>;
    handoffKinds?: string[];
  };
};

type MagentoChangeRowInput = {
  entity_ref: string;
  path?: Record<string, unknown>;
  /** Read-selection parameters only; never forwarded to a mutation. */
  query?: Record<string, unknown>;
  body?: Record<string, unknown> | Array<Record<string, unknown>>;
};

type MagentoChangePayload = {
  operation: string;
  scope: Record<string, unknown>;
  rows: MagentoChangeRowInput[];
  idempotency_key: string;
  projected_exposure?: number | null;
  auto_rule_id?: string;
  changeset_id?: string;
};

type MagentoRuntime = {
  baseUrl: string;
  storeCode: string;
  graphjinEndpoint: string;
  integrationToken: string;
};

type StoredChangesetRow = typeof action_changeset_row.$inferSelect;

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function asActionDefinition(value: unknown): MagentoActionDefinition | null {
  if (!isRecord(value) || typeof value.kind !== "string" || typeof value.domain !== "string") {
    return null;
  }
  if (!isRecord(value.adapter) || typeof value.adapter.kind !== "string") return null;
  return value as unknown as MagentoActionDefinition;
}

function parseChangePayload(value: Record<string, unknown>): MagentoChangePayload {
  const operation = typeof value.operation === "string" ? value.operation.trim() : "";
  const idempotencyKey = typeof value.idempotency_key === "string"
    ? value.idempotency_key.trim()
    : "";
  if (!operation) throw new Error("Magento change needs an operation");
  if (!isRecord(value.scope) || Object.keys(value.scope).length === 0) {
    throw new Error("Magento change needs a named store, website, or source scope");
  }
  if (!Array.isArray(value.rows) || value.rows.length === 0) {
    throw new Error("Magento change needs at least one row");
  }
  if (idempotencyKey.length < 8 || idempotencyKey.length > 200) {
    throw new Error("Magento change needs an idempotency_key between 8 and 200 characters");
  }
  const rows = value.rows.map((item, index) => {
    if (!isRecord(item)) throw new Error(`Magento row ${index + 1} must be an object`);
    const entityRef = typeof item.entity_ref === "string" ? item.entity_ref.trim() : "";
    if (!entityRef) throw new Error(`Magento row ${index + 1} needs entity_ref`);
    if (item.path !== undefined && !isRecord(item.path)) {
      throw new Error(`Magento row ${index + 1} path must be an object`);
    }
    if (item.query !== undefined && !isRecord(item.query)) {
      throw new Error(`Magento row ${index + 1} query must be an object`);
    }
    if (item.body !== undefined && !isRecord(item.body) && !Array.isArray(item.body)) {
      throw new Error(`Magento row ${index + 1} body must be an object or array`);
    }
    return {
      entity_ref: entityRef,
      path: (item.path as Record<string, unknown> | undefined) ?? {},
      query: (item.query as Record<string, unknown> | undefined) ?? {},
      body: (item.body as Record<string, unknown> | Array<Record<string, unknown>> | undefined) ?? {},
    };
  });
  return {
    operation,
    scope: value.scope,
    rows,
    idempotency_key: idempotencyKey,
    projected_exposure: typeof value.projected_exposure === "number"
      ? value.projected_exposure
      : null,
    auto_rule_id: typeof value.auto_rule_id === "string" ? value.auto_rule_id : undefined,
    changeset_id: typeof value.changeset_id === "string" ? value.changeset_id : undefined,
  };
}

async function loadActionDefinition(
  orgId: string,
  kind: string,
): Promise<MagentoActionDefinition | null> {
  const [row] = await db()
    .select({ definition: pack_action_definition.definition })
    .from(pack_action_definition)
    .where(
      and(
        eq(pack_action_definition.org_id, orgId),
        eq(pack_action_definition.kind, kind),
        eq(pack_action_definition.enabled, true),
      ),
    )
    .limit(1);
  return asActionDefinition(row?.definition);
}

async function loadAllActionDefinitions(
  orgId: string,
): Promise<MagentoActionDefinition[]> {
  const rows = await db()
    .select({ definition: pack_action_definition.definition })
    .from(pack_action_definition)
    .where(
      and(
        eq(pack_action_definition.org_id, orgId),
        eq(pack_action_definition.enabled, true),
      ),
    );
  return rows.flatMap((row) => {
    const definition = asActionDefinition(row.definition);
    return definition ? [definition] : [];
  });
}

async function operationAcrossDefinitions(
  orgId: string,
  operationName: string,
): Promise<{ definition: MagentoActionDefinition; operation: MagentoOperation }> {
  for (const definition of await loadAllActionDefinitions(orgId)) {
    const operation = definition.adapter.operations?.[operationName];
    if (operation) return { definition, operation };
  }
  throw new Error(`Magento operation ${operationName} is not installed`);
}

function graphjinEndpoint(url: string): string {
  const clean = url.replace(/\/+$/, "");
  return clean.endsWith("/api/v1/graphql") ? clean : `${clean}/api/v1/graphql`;
}

async function loadRuntime(orgId: string): Promise<MagentoRuntime> {
  const [installation] = await db()
    .select({ config: pack_install.config })
    .from(pack_install)
    .where(
      and(
        eq(pack_install.org_id, orgId),
        eq(pack_install.pack_id, PACK_ID),
        eq(pack_install.status, "installed"),
      ),
    )
    .orderBy(desc(pack_install.created_at))
    .limit(1);
  if (!installation) throw new Error("Magento pack is not installed");
  const selection = (installation.config?._runtime as { source?: PackSourceSelection } | undefined)?.source;
  const bound = selection ? await resolvePackSource(orgId, selection) : null;
  const [fallback] = bound ? [] : await db()
    .select({ graphqlUrl: data_source.graphql_url })
    .from(data_source)
    .where(and(eq(data_source.org_id, orgId), eq(data_source.enabled, true)))
    .orderBy(desc(data_source.is_default), data_source.created_at)
    .limit(1);
  const source = bound ?? fallback;
  if (!source?.graphqlUrl) throw new Error("Magento GraphJin source is unavailable");
  const secrets = await readSecretsStore();
  const token = secrets[PACK_SECRET_SECTION]?.[TOKEN_KEY];
  if (!token) throw new Error("Magento integration token is unavailable");
  const config = installation.config ?? {};
  return {
    baseUrl: String(config["magento.base_url"] ?? "").replace(/\/+$/, ""),
    storeCode: String(config["magento.store_code"] ?? "all"),
    graphjinEndpoint: graphjinEndpoint(source.graphqlUrl),
    integrationToken: token,
  };
}

function addSearchParams(url: URL, query: Record<string, unknown>) {
  for (const [key, value] of Object.entries(query)) {
    if (value === null || value === undefined) continue;
    if (Array.isArray(value)) {
      value.forEach((item) => url.searchParams.append(key, String(item)));
    } else {
      url.searchParams.set(key, String(value));
    }
  }
}

function resolvedReadPath(
  runtime: MagentoRuntime,
  template: string,
  path: Record<string, unknown>,
): string {
  const missing = new Set<string>();
  const resolved = template.replace(/\{([^}]+)\}/g, (_match, key: string) => {
    const value = path[key];
    if (value === null || value === undefined || String(value).trim() === "") {
      missing.add(key);
      return "";
    }
    return encodeURIComponent(String(value));
  });
  if (missing.size > 0) {
    throw new Error(`Magento read path is missing ${[...missing].join(", ")}`);
  }
  return `${runtime.baseUrl}/rest/${encodeURIComponent(runtime.storeCode)}${resolved}`;
}

async function boundedJson(response: Response): Promise<unknown> {
  const text = await response.text();
  if (Buffer.byteLength(text) > MAX_RESPONSE_BYTES) {
    throw new Error("Magento response exceeded 2 MiB");
  }
  if (!text) return null;
  try {
    return JSON.parse(text);
  } catch {
    return text;
  }
}

function assertOrderUpdateIdentity(path: Record<string, unknown>, body: unknown) {
  const entity = isRecord(body) ? body.entity : undefined;
  const id = Number(path.id);
  if (!Number.isSafeInteger(id) || id < 1 || !isRecord(entity) || entity.entity_id !== id) {
    throw new Error("Magento order update requires entity.entity_id to match the positive order path id");
  }
}

function errorWithCause(error: unknown): string {
  if (!(error instanceof Error)) return String(error);
  const cause = error.cause as { code?: unknown; message?: unknown } | undefined;
  const detail = typeof cause?.code === "string"
    ? cause.code
    : typeof cause?.message === "string"
      ? cause.message
      : null;
  return detail && !error.message.includes(detail) ? `${error.message} (${detail})` : error.message;
}

export async function readMagentoEntity(input: {
  runtime: MagentoRuntime;
  operation: MagentoOperation;
  path: Record<string, unknown>;
  query: Record<string, unknown>;
  body?: unknown;
}): Promise<Record<string, unknown> | null> {
  if (input.operation.operationId === "magentoUpdateOrder") {
    assertOrderUpdateIdentity(input.path, input.body);
  }
  if (!input.operation.readPath) return null;
  const url = new URL(
    resolvedReadPath(input.runtime, input.operation.readPath, input.path),
  );
  let query = input.query;
  let sourceItem: Record<string, unknown> | undefined;
  if (input.operation.entityType === "source_item") {
    const items = expectedAfterImage(input.operation, input.body);
    if (!Array.isArray(items) || items.length !== 1 || !isRecord(items[0]) ||
        typeof items[0].sku !== "string" || !items[0].sku.trim() ||
        typeof items[0].source_code !== "string" || !items[0].source_code.trim()) {
      throw new Error("Magento inventory rows require exactly one source item with sku and source_code");
    }
    sourceItem = items[0];
    // MSI identity is (sku, source_code). Caller-supplied search filters must
    // not select a different before-image from the item the body will mutate.
    query = {};
    for (const [index, field] of ["sku", "source_code"].entries()) {
      const prefix = `searchCriteria[filter_groups][${index}][filters][0]`;
      query[`${prefix}[field]`] = field;
      query[`${prefix}[value]`] = sourceItem[field];
      query[`${prefix}[condition_type]`] = "eq";
    }
  }
  addSearchParams(url, query);
  const response = await fetch(url, {
    headers: { authorization: `Bearer ${input.runtime.integrationToken}` },
    signal: AbortSignal.timeout(15_000),
  });
  if (response.status === 404) {
    if (input.operation.operationId === "magentoUpdateOrder") {
      throw new Error("Magento order update requires an existing order");
    }
    return null;
  }
  const body = await boundedJson(response);
  if (!response.ok) {
    throw new Error(`Magento precondition read failed: HTTP ${response.status}`);
  }
  if (sourceItem) {
    if (!isRecord(body) || !Array.isArray(body.items)) {
      throw new Error("Magento source-item read returned an invalid collection");
    }
    if (body.items.length > 1 || body.items.some((item) =>
      !isRecord(item) || item.sku !== sourceItem!.sku || item.source_code !== sourceItem!.source_code)) {
      throw new Error("Magento source-item read did not uniquely match the requested SKU and source");
    }
    return body.items[0] ?? null;
  }
  return isRecord(body) ? body : { value: body };
}

function classificationsFromRows(
  rows: Array<typeof magento_attribute_classification.$inferSelect>,
): MagentoAttributeClassification[] {
  return rows.map((row) => ({
    domain: row.domain as MagentoDomain,
    entityType: row.entity_type,
    attribute: row.attribute,
    riskClass: row.risk_class as MagentoRiskClass,
    category: row.category as MagentoAttributeClassification["category"],
    rationale: row.rationale,
  }));
}

async function domainControl(orgId: string, domain: MagentoDomain) {
  const [control] = await db()
    .select()
    .from(magento_store_control)
    .where(
      and(
        eq(magento_store_control.org_id, orgId),
        eq(magento_store_control.domain, domain),
      ),
    )
    .limit(1);
  if (!control || !control.enabled) {
    throw new Error(`Magento ${domain} changes are disabled by the administrator`);
  }
  return control;
}

export function magentoCooldownQuery(orgId: string, entityRefs: string[], cooldownSeconds: number) {
  return sql`
      SELECT DISTINCT r.entity_ref
        FROM action_changeset_row r
        JOIN action_changeset c ON c.id = r.changeset_id
       WHERE c.org_id = ${orgId}
         AND r.entity_ref IN (${sql.join(entityRefs.map((ref) => sql`${ref}`), sql`, `)})
         AND r.status = 'applied'
         AND r.finished_at >= now() - (${cooldownSeconds} * interval '1 second')
    `;
}

async function assertAutoRule(input: {
  request: ActionRequestRecord;
  payload: MagentoChangePayload;
  domain: MagentoDomain;
  entityRefs: string[];
}): Promise<{ autoExecution: boolean; actionsToday: number; cooldownBreaches: string[] }> {
  if (!input.payload.auto_rule_id) return { autoExecution: false, actionsToday: 0, cooldownBreaches: [] };
  const [rule] = await db()
    .select()
    .from(magento_auto_rule)
    .where(
      and(
        eq(magento_auto_rule.id, input.payload.auto_rule_id),
        eq(magento_auto_rule.org_id, input.request.orgId),
      ),
    )
    .limit(1);
  if (!rule || !rule.enabled || rule.domain !== input.domain || rule.action_kind !== input.request.kind) {
    throw new Error("Magento automatic rule is unavailable for this change");
  }
  const count = await db().execute<{ domain_count: string; rule_count: string }>(sql`
    SELECT
      count(*)::text AS domain_count,
      count(*) FILTER (WHERE cap_snapshot->>'autoRuleId' = ${rule.id})::text AS rule_count
      FROM action_changeset
     WHERE org_id = ${input.request.orgId}
       AND (cap_snapshot->>'autoExecution')::boolean IS TRUE
       AND created_at >= date_trunc('day', now())
  `);
  const actionsToday = Number(count.rows[0]?.domain_count ?? 0);
  const ruleActionsToday = Number(count.rows[0]?.rule_count ?? 0);
  const cooldownBreaches: string[] = [];
  if (rule.cooldown_seconds > 0 && input.entityRefs.length > 0) {
    const recent = await db().execute<{ entity_ref: string }>(
      magentoCooldownQuery(input.request.orgId, input.entityRefs, rule.cooldown_seconds),
    );
    cooldownBreaches.push(...recent.rows.map((row) => row.entity_ref));
  }
  if (ruleActionsToday >= rule.daily_cap) {
    await db().update(magento_auto_rule).set({
      enabled: false,
      suspended_reason: "daily_cap_exhausted",
      updated_at: new Date(),
    }).where(eq(magento_auto_rule.id, rule.id));
    throw new Error(`Magento automatic rule daily cap of ${rule.daily_cap} is exhausted`);
  }
  return { autoExecution: true, actionsToday, cooldownBreaches };
}

function compareCanonical(left: unknown, right: unknown): boolean {
  return canonicalHash(left) === canonicalHash(right);
}

function deepContains(actual: unknown, expected: unknown): boolean {
  if (Array.isArray(expected)) {
    return Array.isArray(actual) && expected.every((value, index) => deepContains(actual[index], value));
  }
  if (isRecord(expected)) {
    if (!isRecord(actual)) return false;
    return Object.entries(expected).every(([key, value]) => deepContains(actual[key], value));
  }
  return Object.is(actual, expected) || String(actual) === String(expected);
}

function stringField(value: unknown, field: string): string | null {
  if (Array.isArray(value)) {
    for (const child of value) {
      const found = stringField(child, field);
      if (found !== null) return found;
    }
    return null;
  }
  if (!isRecord(value)) return null;
  if (typeof value[field] === "string") return value[field];
  for (const child of Object.values(value)) {
    const found = stringField(child, field);
    if (found !== null) return found;
  }
  return null;
}

export function reconciliationConfirmed(
  operation: MagentoOperation,
  reconciled: Record<string, unknown> | null,
  expected: unknown,
): boolean {
  if (!operation.readPath) return true;
  if (isDeleteOperation(operation)) return reconciled === null;
  if (!reconciled) return false;
  if (operation.operationId === "magentoMoveCategory" && isRecord(expected)) {
    return reconciled.parent_id === expected.parentId && reconciled.position === expected.position;
  }
  if (operation.entityType === "sales_rule" && isRecord(expected)) {
    const couponTypes: Record<string, string> = { "1": "NO_COUPON", "2": "SPECIFIC_COUPON", "3": "AUTO" };
    expected = { ...expected, ...(expected.coupon_type !== undefined
      ? { coupon_type: couponTypes[String(expected.coupon_type)] ?? expected.coupon_type } : {}) };
  }
  if (operation.entityType === "customer" && isRecord(expected)) {
    const { updated_at: _updatedAt, ...writable } = expected;
    expected = writable;
  }
  if (["magentoCreateInvoice", "magentoCreateShipment"].includes(operation.operationId) && isRecord(expected)) {
    return reconciled.order_id === expected.order_id && Array.isArray(reconciled.items) && reconciled.items.length > 0 &&
      (expected.items === undefined || (Array.isArray(expected.items) && expected.items.length > 0 && expected.items.every((item) => isRecord(item) &&
        (reconciled.items as unknown[]).some((actual) => isRecord(actual) &&
          actual.order_item_id === item.order_item_id && actual.qty === item.qty))));
  }
  if (operation.operationId === "magentoAddOrderComment") {
    const comment = stringField(expected, "comment");
    const histories = Array.isArray(reconciled.status_histories)
      ? reconciled.status_histories
      : [];
    return Boolean(comment) && histories.some((history) => stringField(history, "comment") === comment);
  }
  if (operation.operationId === "magentoHoldOrder") return reconciled.state === "holded";
  if (operation.operationId === "magentoUnholdOrder") return reconciled.state !== "holded";
  if (operation.operationId === "magentoCancelOrder") {
    return reconciled.state === "canceled" || reconciled.status === "canceled";
  }
  if (operation.entityType === "source_item" && Array.isArray(expected)) {
    return expected.every((item) => deepContains(reconciled, item));
  }
  return deepContains(reconciled, expected);
}

function expectedAfterImage(operation: MagentoOperation, body: unknown): unknown {
  if (operation.bodyKey && isRecord(body) && body[operation.bodyKey] !== undefined) {
    return body[operation.bodyKey];
  }
  return body;
}

function isCreateOperation(operation: MagentoOperation): boolean {
  return /Create/.test(operation.operationId);
}

function isDeleteOperation(operation: MagentoOperation): boolean {
  return /Delete/.test(operation.operationId);
}

function responseEntityId(value: unknown, field: string): string | number | null {
  if (Array.isArray(value)) {
    for (const child of value) {
      const found = responseEntityId(child, field);
      if (found !== null) return found;
    }
    return null;
  }
  if (!isRecord(value)) return null;
  const direct = value[field];
  if (typeof direct === "string" || typeof direct === "number") return direct;
  for (const child of Object.values(value)) {
    const found = responseEntityId(child, field);
    if (found !== null) return found;
  }
  return null;
}

function reconciledPath(
  operation: MagentoOperation,
  path: Record<string, unknown>,
  response: unknown,
): Record<string, unknown> {
  if (!operation.readPath) return path;
  const next = { ...path };
  for (const match of operation.readPath.matchAll(/\{([^}]+)\}/g)) {
    const key = match[1]!;
    if (next[key] !== undefined && next[key] !== null && String(next[key]).trim()) continue;
    const value = responseEntityId(response, operation.entityIdField ?? key);
    if (value !== null) next[key] = value;
  }
  return next;
}

function normalizePath(operation: MagentoOperation, path: Record<string, unknown>, storeCode: string) {
  const numeric = new Set(operation.numericPathParams ?? []);
  return Object.fromEntries(
    Object.entries({ storeCode, ...path }).map(([key, value]) => {
      if (numeric.has(key)) {
        const number = Number(value);
        if (!Number.isSafeInteger(number) || number < 1) {
          throw new Error(`Magento path parameter ${key} must be a positive integer`);
        }
        return [key, number];
      }
      return [key, value];
    }),
  );
}

async function prepareChangeset(request: ActionRequestRecord, definition: MagentoActionDefinition) {
  const payload = parseChangePayload(request.payload);
  const operation = definition.adapter.operations?.[payload.operation];
  if (!operation) throw new Error(`Magento operation ${payload.operation} is not allowed for ${request.kind}`);
  const control = await domainControl(request.orgId, definition.domain);
  const runtime = await loadRuntime(request.orgId);
  const storedClassifications = await db()
    .select()
    .from(magento_attribute_classification)
    .where(
      and(
        eq(magento_attribute_classification.org_id, request.orgId),
        eq(magento_attribute_classification.domain, definition.domain),
        eq(magento_attribute_classification.entity_type, operation.entityType),
      ),
    );
  const classifications = storedClassifications.length > 0
    ? classificationsFromRows(storedClassifications)
    : DEFAULT_MAGENTO_ATTRIBUTE_CLASSIFICATIONS;
  const preparedRows = [] as Array<{
    input: MagentoChangeRowInput;
    beforeImage: Record<string, unknown>;
    classification: ReturnType<typeof classifyMagentoChange>;
  }>;
  for (const row of payload.rows) {
    const beforeImage = isCreateOperation(operation)
      ? {}
      : await readMagentoEntity({
          runtime,
          operation,
          path: row.path ?? {},
          query: row.query ?? {},
          body: row.body,
        }) ?? {};
    const classification = classifyMagentoChange({
      domain: definition.domain,
      entityType: operation.entityType,
      operationId: operation.operationId,
      defaultClass: operation.defaultClass,
      body: row.body ?? {},
      classifications,
    });
    if (classification.riskClass === 0) {
      throw new Error(
        `Magento ${operation.operationId} cannot be performed by OpenNeko; complete it in Magento Admin`,
      );
    }
    preparedRows.push({ input: row, beforeImage, classification });
  }
  const classifiedRisk = preparedRows.reduce<MagentoRiskClass>(
    (value, row) => Math.min(value, row.classification.riskClass) as MagentoRiskClass,
    operation.defaultClass,
  );
  const auto = await assertAutoRule({
    request,
    payload,
    domain: definition.domain,
    entityRefs: payload.rows.map((row) => row.entity_ref),
  });
  const caps = evaluateMagentoCaps({
    domain: definition.domain,
    operationId: operation.operationId,
    riskClass: classifiedRisk,
    rows: preparedRows.map((row) => ({
      beforeImage: row.beforeImage,
      afterImage: row.input.body ?? {},
    })),
    caps: control.caps as Record<string, number>,
    projectedExposure: payload.projected_exposure,
    autoExecution: auto.autoExecution,
    autoActionsToday: auto.actionsToday,
    cooldownBreaches: auto.cooldownBreaches,
  });
  if (!caps.allowed) throw new Error(`Magento caps rejected the change: ${caps.violations.join("; ")}`);
  if (request.status === "approved" && caps.riskClass === 1 && !(await hasHumanActionApproval(request))) {
    throw new Error("This Magento change requires approval from a human administrator");
  }

  const [existing] = await db()
    .select()
    .from(action_changeset)
    .where(
      and(
        eq(action_changeset.org_id, request.orgId),
        eq(action_changeset.idempotency_key, payload.idempotency_key),
      ),
    )
    .limit(1);
  if (existing) {
    if (existing.action_request_id !== request.id || existing.operation_id !== payload.operation) {
      throw new Error("Magento idempotency key is already bound to a different change-set");
    }
    return updateActionRequestPayload({
      id: request.id,
      orgId: request.orgId,
      payload: { ...request.payload, changeset_id: existing.id },
    });
  }

  const changeset = await db().transaction(async (tx) => {
    const [created] = await tx.insert(action_changeset).values({
      org_id: request.orgId,
      action_request_id: request.id,
      domain: definition.domain,
      scope: payload.scope,
      operation_id: payload.operation,
      risk_class: caps.riskClass as 1 | 2,
      status: request.status === "approved" ? "approved" : "pending_approval",
      summary: request.summary ?? `Magento ${payload.operation}`,
      idempotency_key: payload.idempotency_key,
      cap_snapshot: {
        ...caps.snapshot,
        autoRuleId: payload.auto_rule_id ?? null,
      },
      projected_exposure: caps.projectedExposure === null ? null : String(caps.projectedExposure),
      previewed_at: new Date(),
    }).returning();
    for (const [index, row] of preparedRows.entries()) {
      await tx.insert(action_changeset_row).values({
        changeset_id: created!.id,
        row_index: index,
        entity_ref: row.input.entity_ref,
        operation_id: payload.operation,
        path: row.input.path ?? {},
        query: row.input.query ?? {},
        before_image: row.beforeImage,
        expected_current: row.beforeImage,
        after_image: isRecord(row.input.body) ? row.input.body : { items: row.input.body ?? [] },
        status: "draft",
      });
    }
    return created!;
  });
  await recordAuditEvent({
    orgId: request.orgId,
    entityKind: "action_changeset",
    entityId: changeset.id,
    event: "previewed",
    payload: {
      actionRequestId: request.id,
      domain: definition.domain,
      operation: payload.operation,
      executionMode: magentoExecutionMode(caps.riskClass),
      rows: preparedRows.length,
      violations: caps.violations,
      escalations: caps.escalations,
    },
  });
  const prepared = await updateActionRequestPayload({
    id: request.id,
    orgId: request.orgId,
    payload: {
      ...request.payload,
      changeset_id: changeset.id,
      preview: {
        execution_mode: magentoExecutionMode(caps.riskClass),
        row_count: preparedRows.length,
        projected_exposure: caps.projectedExposure,
        escalations: caps.escalations,
        caps: caps.snapshot,
        rows: preparedRows.map((row) => {
          const after = expectedAfterImage(operation, row.input.body ?? {});
          const before = projectBeforeImage(row.beforeImage, after);
          return definition.domain === "customers"
            ? {
                entity_ref: row.input.entity_ref,
                attributes: row.classification.attributes.map((attribute) => attribute.attribute),
                before_hash: canonicalHash(before),
                after_hash: canonicalHash(after),
                data_minimized: true,
              }
            : {
                entity_ref: row.input.entity_ref,
                before,
                after,
                approval_requirements: row.classification.attributes.map(({ riskClass, ...attribute }) => ({
                  ...attribute,
                  execution_mode: magentoExecutionMode(riskClass),
                })),
              };
        }),
      },
    },
  });
  if (auto.autoExecution && caps.riskClass === 2) {
    if (!control.auto_execute) {
      throw new Error(`Magento ${definition.domain} automatic execution is disabled`);
    }
    return autoApprovePreparedActionRequest({
      id: prepared.id,
      orgId: prepared.orgId,
      reason: `Magento automatic rule ${payload.auto_rule_id} passed its stored limits`,
    });
  }
  return prepared;
}

function projectBeforeImage(before: unknown, requested: unknown): unknown {
  if (Array.isArray(requested)) return before;
  if (!isRecord(requested) || !isRecord(before)) return before;
  const projected: Record<string, unknown> = {};
  for (const [key, value] of Object.entries(requested)) {
    if (Object.hasOwn(before, key)) projected[key] = projectBeforeImage(before[key], value);
  }
  return projected;
}

function inverseBody(input: {
  inverseOperation: MagentoOperation;
  originalOperation: MagentoOperation;
  beforeImage: Record<string, unknown>;
  originalAfterImage: Record<string, unknown>;
}) {
  if (/Delete/.test(input.inverseOperation.operationId)) return {};
  const requested = expectedAfterImage(input.originalOperation, input.originalAfterImage);
  const projected = Array.isArray(requested)
    ? [input.beforeImage]
    : isRecord(requested) && Object.keys(requested).length > 0
      ? projectBeforeImage(input.beforeImage, requested)
      : input.beforeImage;
  return input.inverseOperation.bodyKey
    ? { [input.inverseOperation.bodyKey]: projected }
    : isRecord(projected)
      ? projected
      : input.beforeImage;
}

async function prepareUndo(request: ActionRequestRecord) {
  const originalId = typeof request.payload.changeset_id === "string"
    ? request.payload.changeset_id
    : "";
  const idempotencyKey = typeof request.payload.idempotency_key === "string"
    ? request.payload.idempotency_key.trim()
    : "";
  if (!originalId || idempotencyKey.length < 8) {
    throw new Error("Magento undo needs changeset_id and idempotency_key");
  }
  const [original] = await db()
    .select()
    .from(action_changeset)
    .where(and(eq(action_changeset.id, originalId), eq(action_changeset.org_id, request.orgId)))
    .limit(1);
  if (!original || !["applied", "partially_applied"].includes(original.status)) {
    throw new Error("Only an applied or partially applied Magento change-set can be undone");
  }
  const rows = await db()
    .select()
    .from(action_changeset_row)
    .where(eq(action_changeset_row.changeset_id, original.id))
    .orderBy(action_changeset_row.row_index);
  const runtime = await loadRuntime(request.orgId);
  const inverseRows: Array<{
    original: StoredChangesetRow;
    operationName: string;
    operation: MagentoOperation;
    current: Record<string, unknown>;
    body: Record<string, unknown>;
  }> = [];
  for (const row of rows.filter((value) => value.status === "applied")) {
    const currentOperation = await operationAcrossDefinitions(request.orgId, row.operation_id);
    if (!currentOperation.operation.reversible || !currentOperation.operation.inverseOperation) {
      throw new Error(`Magento operation ${row.operation_id} does not have a safe inverse`);
    }
    const inverse = await operationAcrossDefinitions(
      request.orgId,
      currentOperation.operation.inverseOperation,
    );
    const current = await readMagentoEntity({
      runtime,
      operation: currentOperation.operation,
      path: row.path,
      query: row.query,
      body: row.after_image,
    }) ?? {};
    const expected = row.reconciled_image ?? expectedAfterImage(currentOperation.operation, row.after_image);
    if (Object.keys(current).length > 0 && !deepContains(current, expected)) {
      await db().update(action_changeset_row).set({
        status: "drifted",
        reconciled_image: current,
        error: "current Magento value drifted after the original change",
        updated_at: new Date(),
      }).where(eq(action_changeset_row.id, row.id));
      throw new Error(`Magento entity ${row.entity_ref} drifted; inverse draft was not created`);
    }
    inverseRows.push({
      original: row,
      operationName: currentOperation.operation.inverseOperation,
      operation: inverse.operation,
      current,
      body: inverseBody({
        inverseOperation: inverse.operation,
        originalOperation: currentOperation.operation,
        beforeImage: row.before_image,
        originalAfterImage: row.after_image,
      }),
    });
  }
  if (inverseRows.length === 0) throw new Error("Magento change-set has no applied rows to undo");
  const operationNames = new Set(inverseRows.map((row) => row.operationName));
  if (operationNames.size !== 1) {
    throw new Error("Magento inverse rows span multiple operations; split them into separate undo drafts");
  }
  const operationName = inverseRows[0]!.operationName;
  const created = await db().transaction(async (tx) => {
    const [changeset] = await tx.insert(action_changeset).values({
      org_id: request.orgId,
      action_request_id: request.id,
      inverse_of_id: original.id,
      domain: original.domain,
      scope: original.scope,
      operation_id: operationName,
      risk_class: original.risk_class,
      status: request.status === "approved" ? "approved" : "pending_approval",
      summary: `Undo: ${original.summary}`,
      idempotency_key: idempotencyKey,
      cap_snapshot: { inverseOf: original.id, driftCheckedAt: new Date().toISOString() },
      previewed_at: new Date(),
    }).returning();
    for (const [index, row] of inverseRows.entries()) {
      await tx.insert(action_changeset_row).values({
        changeset_id: changeset!.id,
        row_index: index,
        entity_ref: row.original.entity_ref,
        operation_id: operationName,
        path: row.original.path,
        query: row.original.query,
        before_image: row.current,
        expected_current: row.current,
        after_image: row.body,
        status: "draft",
      });
    }
    return changeset!;
  });
  await recordAuditEvent({
    orgId: request.orgId,
    entityKind: "action_changeset",
    entityId: created.id,
    event: "inverse_previewed",
    payload: { inverseOf: original.id, rows: inverseRows.length },
  });
  return updateActionRequestPayload({
    id: request.id,
    orgId: request.orgId,
    payload: {
      ...request.payload,
      changeset_id: created.id,
      inverse_of: original.id,
      operation: operationName,
      preview: {
        execution_mode: magentoExecutionMode(original.risk_class as MagentoRiskClass),
        row_count: inverseRows.length,
      },
    },
  });
}

function graphjinResponseJson(value: unknown): unknown {
  if (typeof value !== "string") return value;
  try {
    return JSON.parse(value);
  } catch {
    return value;
  }
}

export function magentoGraphjinMutationField(mutationRoot: string): string {
  const field = mutationRoot.replace(/^magento_operator_v2_/, "");
  if (field === mutationRoot || !/^[_A-Za-z][_0-9A-Za-z]*$/.test(field)) {
    throw new Error("invalid Magento V2 mutation root");
  }
  return field;
}

export async function executeGraphjinMutation(input: {
  request: ActionRequestRecord;
  runtime: MagentoRuntime;
  operation: MagentoOperation;
  path: Record<string, unknown>;
  body: unknown;
  role: "magento_ops_executor" | "magento_sensitive_executor";
}) {
  // Pack definitions retain the stable source/spec namespace so installer
  // validation can prove where a write comes from. GraphJin's config assigns
  // the stripped value as expose_as, and that alias is the actual GraphQL root.
  const orderUpdate = input.operation.operationId === "magentoUpdateOrder";
  if (orderUpdate) assertOrderUpdateIdentity(input.path, input.body);
  const field = magentoGraphjinMutationField(input.operation.mutationRoot);
  const result = await graphjinQuery<Record<string, {
    ok?: boolean;
    status_code?: number;
    operation_id?: string;
    request_id?: string;
    response_json?: unknown;
  }>>({
    baseUrl: input.runtime.graphjinEndpoint,
    headers: {
      authorization: `Bearer ${mintGraphjinToken({
        orgId: input.request.orgId,
        userId: input.request.approvedByUserId ?? input.request.actorUserId ?? "magento-executor",
        role: input.role,
        ttlSeconds: 60,
      })}`,
    },
    role: input.role,
    query: `mutation ExecuteMagentoV2($call: JSON!) { ${field}(call: $call) { ok status_code operation_id request_id response_json } }`,
    variables: {
      call: {
        // Order save identifies its entity in the body; id belongs only to the read route.
        path: normalizePath(input.operation, orderUpdate ? {} : input.path, input.runtime.storeCode),
        ...(input.body !== undefined && (Array.isArray(input.body) || !isRecord(input.body) || Object.keys(input.body).length > 0)
          ? { body: input.body }
          : {}),
      },
    },
    signal: AbortSignal.timeout(30_000),
  });
  const response = result.data?.[field];
  if (result.errors?.length || !response) {
    throw new Error(result.errors?.map((error) => error.message).join("; ") || "Magento mutation returned no result");
  }
  if (!response.ok || Number(response.status_code ?? 500) >= 400) {
    throw new Error(`Magento mutation failed with HTTP ${response.status_code ?? "unknown"}`);
  }
  return {
    statusCode: Number(response.status_code ?? 200),
    operationId: response.operation_id ?? input.operation.operationId,
    requestId: response.request_id ?? null,
    response: graphjinResponseJson(response.response_json),
  };
}

function bulkUuid(value: unknown): string | null {
  if (isRecord(value)) {
    for (const key of ["bulk_uuid", "bulkUuid", "uuid"]) {
      if (typeof value[key] === "string" && value[key]) return value[key];
    }
  }
  return typeof value === "string" && value.length >= 8 ? value.replace(/^"|"$/g, "") : null;
}

function bulkOperations(value: unknown): Array<Record<string, unknown>> {
  if (!isRecord(value)) return [];
  const operations = Array.isArray(value.operations_list)
    ? value.operations_list
    : Array.isArray(value.operations)
      ? value.operations
      : [];
  return operations.filter(isRecord);
}

export function isTerminalMagentoBulkStatus(status: number): boolean {
  // Magento\Framework\Bulk\OperationInterface:
  // 1 complete, 2 retryably failed, 3 non-retryably failed, 4 open, 5 rejected.
  return [1, 2, 3, 5].includes(status);
}

async function waitForMagentoBulk(runtime: MagentoRuntime, uuid: string) {
  const deadline = Date.now() + 90_000;
  let last: unknown = null;
  while (Date.now() < deadline) {
    const url = `${runtime.baseUrl}/rest/${encodeURIComponent(runtime.storeCode)}/V1/bulk/${encodeURIComponent(uuid)}/status`;
    const response = await fetch(url, {
      headers: { authorization: `Bearer ${runtime.integrationToken}` },
      signal: AbortSignal.timeout(10_000),
    });
    last = await boundedJson(response);
    if (!response.ok) throw new Error(`Magento bulk status failed: HTTP ${response.status}`);
    const operations = bulkOperations(last);
    if (operations.length > 0) {
      const statuses = operations.map((item) => isRecord(item) ? Number(item.status ?? item.operation_status) : -1);
      if (statuses.every(isTerminalMagentoBulkStatus)) return last;
    }
    await new Promise((resolve) => setTimeout(resolve, 1_000));
  }
  throw new Error("Magento bulk operation did not reach a terminal state within 90 seconds");
}

async function indexerReceipt(runtime: MagentoRuntime, orgId: string) {
  const result = await graphjinQuery<{ indexer_state?: Array<{ indexer_id?: string; status?: string; updated?: string }> }>({
    baseUrl: runtime.graphjinEndpoint,
    headers: {
      authorization: `Bearer ${mintGraphjinToken({
        orgId,
        userId: "magento-reconciliation",
        role: "service",
        ttlSeconds: 60,
      })}`,
    },
    query: "query MagentoIndexerReceipt { indexer_state { indexer_id status updated } }",
    signal: AbortSignal.timeout(15_000),
  }).catch(() => null);
  return result?.data?.indexer_state ?? [];
}

async function executeChangeset(request: ActionRequestRecord) {
  const changesetId = typeof request.payload.changeset_id === "string" ? request.payload.changeset_id : "";
  if (!changesetId) throw new Error("Magento action has no prepared change-set");
  const [changeset] = await db()
    .select()
    .from(action_changeset)
    .where(and(eq(action_changeset.id, changesetId), eq(action_changeset.org_id, request.orgId)))
    .limit(1);
  if (!changeset || changeset.action_request_id !== request.id) {
    throw new Error("Magento change-set does not belong to this approved request");
  }
  if (changeset.risk_class === 1 && !(await hasHumanActionApproval(request))) {
    throw new Error("This Magento change requires approval from a human administrator");
  }
  if (!["approved", "pending_approval"].includes(changeset.status)) {
    throw new Error(`Magento change-set status=${changeset.status}; expected approved`);
  }
  const { definition, operation } = await operationAcrossDefinitions(
    request.orgId,
    changeset.operation_id,
  );
  await domainControl(request.orgId, definition.domain);
  const runtime = await loadRuntime(request.orgId);
  const rows = await db()
    .select()
    .from(action_changeset_row)
    .where(eq(action_changeset_row.changeset_id, changeset.id))
    .orderBy(action_changeset_row.row_index);
  if (operation.resultMode === "async_bulk" && operation.readPath) {
    let drifted = false;
    for (const row of rows) {
      const current = await readMagentoEntity({
        runtime,
        operation,
        path: row.path,
        query: row.query,
        body: row.after_image,
      });
      if (Object.keys(row.expected_current).length > 0 && !compareCanonical(current ?? {}, row.expected_current)) {
        drifted = true;
        await db().update(action_changeset_row).set({
          status: "drifted",
          reconciled_image: current,
          error: "Magento value changed after preview",
          finished_at: new Date(),
          updated_at: new Date(),
        }).where(eq(action_changeset_row.id, row.id));
      }
    }
    if (drifted) {
      await db().update(action_changeset).set({
        status: "reconcile_required",
        reconciled_at: new Date(),
        updated_at: new Date(),
      }).where(eq(action_changeset.id, changeset.id));
      throw new Error("Magento bulk change-set drifted after preview; nothing was submitted");
    }
  }
  const role = changeset.risk_class === 1
    ? "magento_sensitive_executor" as const
    : "magento_ops_executor" as const;
  await db().update(action_changeset).set({
    status: "executing",
    approved_by_user_id: request.approvedByUserId,
    approved_at: request.approvedAt ?? new Date(),
    updated_at: new Date(),
  }).where(eq(action_changeset.id, changeset.id));

  let sharedBulkUuid: string | null = null;
  let sharedBulkStatus: unknown = null;
  if (operation.resultMode === "async_bulk") {
    const result = await executeGraphjinMutation({
      request,
      runtime,
      operation,
      path: {},
      body: rows.map((row) => {
        const body = row.after_image;
        return Array.isArray(body.items) && Object.keys(body).length === 1 ? body.items : body;
      }).flat(),
      role,
    });
    sharedBulkUuid = bulkUuid(result.response);
    if (!sharedBulkUuid) throw new Error("Magento async bulk response did not include bulk_uuid");
    await db().update(action_changeset).set({
      bulk_uuid: sharedBulkUuid,
      updated_at: new Date(),
    }).where(eq(action_changeset.id, changeset.id));
    await db().update(action_changeset_row).set({
      status: "submitted",
      external_ref: sharedBulkUuid,
      started_at: new Date(),
      updated_at: new Date(),
    }).where(eq(action_changeset_row.changeset_id, changeset.id));
    sharedBulkStatus = await waitForMagentoBulk(runtime, sharedBulkUuid);
  }

  let applied = 0;
  let failed = 0;
  let reconcileRequired = 0;
  let firstRowError: string | null = null;
  const terminalBulkOperations = bulkOperations(sharedBulkStatus);
  for (const [rowIndex, row] of rows.entries()) {
    let writeAttempted = operation.resultMode === "async_bulk";
    try {
      if (operation.resultMode === "async_bulk") {
        const bulkOperation = terminalBulkOperations[rowIndex];
        const bulkStatus = Number(bulkOperation?.status ?? bulkOperation?.operation_status ?? -1);
        if (bulkStatus !== 1) {
          // The bulk request was submitted, so this is never a safe retry.
          reconcileRequired += 1;
          await db().update(action_changeset_row).set({
            status: "reconcile_required",
            error: `Magento bulk row failed with terminal status ${bulkStatus}`,
            finished_at: new Date(),
            updated_at: new Date(),
          }).where(eq(action_changeset_row.id, row.id));
          continue;
        }
      }
      const current = isCreateOperation(operation) ? null : await readMagentoEntity({
        runtime,
        operation,
        path: row.path,
        query: row.query,
        body: row.after_image,
      });
      if (
        operation.resultMode !== "async_bulk" &&
        operation.readPath &&
        Object.keys(row.expected_current).length > 0 &&
        !compareCanonical(current ?? {}, row.expected_current)
      ) {
        await db().update(action_changeset_row).set({
          status: "drifted",
          reconciled_image: current,
          error: "Magento value changed after preview",
          finished_at: new Date(),
          updated_at: new Date(),
        }).where(eq(action_changeset_row.id, row.id));
        reconcileRequired += 1;
        continue;
      }
      let response: Awaited<ReturnType<typeof executeGraphjinMutation>> | null = null;
      if (operation.resultMode !== "async_bulk") {
        await db().update(action_changeset_row).set({
          status: "submitted",
          started_at: new Date(),
          updated_at: new Date(),
        }).where(eq(action_changeset_row.id, row.id));
        writeAttempted = true;
        response = await executeGraphjinMutation({
          request,
          runtime,
          operation,
          path: row.path,
          body: row.after_image,
          role,
        });
      }
      const path = response
        ? reconciledPath(operation, row.path, response.response)
        : row.path;
      if (!compareCanonical(path, row.path)) {
        await db().update(action_changeset_row).set({
          path,
          updated_at: new Date(),
        }).where(eq(action_changeset_row.id, row.id));
      }
      const fulfillment = ["magentoCreateInvoice", "magentoCreateShipment"].includes(operation.operationId);
      const documentId = fulfillment ? Number(response?.response) : null;
      if (fulfillment && (!Number.isSafeInteger(documentId) || Number(documentId) < 1)) {
        throw new Error("Magento fulfillment response did not identify the created document");
      }
      const reconciled = await readMagentoEntity({
        runtime,
        operation: fulfillment ? { ...operation, readPath: `/V1/${operation.operationId === "magentoCreateInvoice" ? "invoices" : "shipment"}/${documentId}` } : operation,
        path,
        query: row.query,
        body: row.after_image,
      });
      let expected = expectedAfterImage(operation, row.after_image);
      if (fulfillment && isRecord(expected)) {
        expected = { ...expected, order_id: Number(path.orderId) };
      }
      if (operation.operationId === "magentoMoveCategory" && isRecord(expected)) {
        const sibling = expected.afterId ? await readMagentoEntity({ runtime,
          operation: { ...operation, readPath: "/V1/categories/{categoryId}" },
          path: { categoryId: expected.afterId }, query: {},
        }) : null;
        expected = { ...expected, position: expected.afterId ? Number(sibling?.position) + 1 : 1 };
      }
      const confirmed = reconciliationConfirmed(operation, reconciled, expected);
      await db().update(action_changeset_row).set({
        status: confirmed ? "applied" : "reconcile_required",
        external_ref: sharedBulkUuid ?? response?.requestId ?? null,
        reconciled_image: reconciled,
        error: confirmed ? null : "Magento write succeeded but reconciliation did not confirm the requested values",
        finished_at: new Date(),
        updated_at: new Date(),
      }).where(eq(action_changeset_row.id, row.id));
      if (confirmed) applied += 1;
      else reconcileRequired += 1;
    } catch (error) {
      if (writeAttempted) reconcileRequired += 1;
      else failed += 1;
      const message = errorWithCause(error);
      firstRowError ??= message;
      await db().update(action_changeset_row).set({
        status: writeAttempted ? "reconcile_required" : "failed",
        error: writeAttempted
          ? `Magento write outcome needs reconciliation: ${message}`
          : message,
        finished_at: new Date(),
        updated_at: new Date(),
      }).where(eq(action_changeset_row.id, row.id));
    }
  }
  const status: "applied" | "partially_applied" | "failed" | "reconcile_required" = applied > 0 && (failed > 0 || reconcileRequired > 0)
    ? "partially_applied"
    : reconcileRequired > 0
        ? "reconcile_required"
        : failed > 0
          ? "failed"
          : "applied";
  await db().update(action_changeset).set({
    status,
    executed_at: new Date(),
    reconciled_at: new Date(),
    updated_at: new Date(),
  }).where(eq(action_changeset.id, changeset.id));
  await recordAuditEvent({
    orgId: request.orgId,
    entityKind: "action_changeset",
    entityId: changeset.id,
    event: `execution:${status}`,
    payload: {
      operation: changeset.operation_id,
      applied,
      failed,
      reconcileRequired,
      bulkUuid: sharedBulkUuid,
    },
  });
  const autoRuleId = typeof request.payload.auto_rule_id === "string"
    ? request.payload.auto_rule_id
    : null;
  if (autoRuleId && status !== "failed") {
    await db().update(magento_auto_rule).set({
      last_fired_at: new Date(),
      updated_at: new Date(),
    }).where(
      and(
        eq(magento_auto_rule.id, autoRuleId),
        eq(magento_auto_rule.org_id, request.orgId),
      ),
    );
  }
  if (status === "failed") {
    throw new Error(
      firstRowError
        ? `Every Magento change-set row failed: ${firstRowError}`
        : "Every Magento change-set row failed",
    );
  }
  return {
    status: status === "applied" ? "succeeded" as const : status,
    commandOrOperation: changeset.operation_id,
    externalRef: sharedBulkUuid ?? changeset.id,
    changesetId: changeset.id,
    result: {
      changeset_id: changeset.id,
      status,
      row_count: rows.length,
      applied,
      failed,
      reconcile_required: reconcileRequired,
      bulk_uuid: sharedBulkUuid,
      bulk_status_recorded: sharedBulkStatus !== null,
      indexers: await indexerReceipt(runtime, request.orgId),
      retry: "never",
    },
  };
}

const financialHandoffAdapter: ActionAdapter = async ({ request }) => {
  const kind = typeof request.payload.handoff_kind === "string" ? request.payload.handoff_kind : "";
  const entityRef = typeof request.payload.entity_ref === "string" ? request.payload.entity_ref : "";
  const allowed = new Set([
    "online_refund",
    "return_approval",
    "financial_configuration",
    "store_credit_over_cap",
  ]);
  if (!allowed.has(kind) || !entityRef || !isRecord(request.payload.draft) || !isRecord(request.payload.evidence)) {
    throw new Error("Magento financial handoff payload is invalid");
  }
  const [handoff] = await db().insert(magento_financial_handoff).values({
    org_id: request.orgId,
    action_request_id: request.id,
    kind,
    entity_ref: entityRef,
    status: "ready_for_human",
    draft: request.payload.draft,
    evidence: request.payload.evidence,
  }).returning();
  await recordAuditEvent({
    orgId: request.orgId,
    entityKind: "magento_financial_handoff",
    entityId: handoff!.id,
    event: "ready_for_human",
    payload: { kind, entityRef, executePath: false },
  });
  return {
    commandOrOperation: "magento_admin_handoff",
    externalRef: handoff!.id,
    result: {
      handoff_id: handoff!.id,
      status: "ready_for_human",
      execute_path: false,
      next_step: "A human completes this operation in Magento admin.",
    },
  };
};

/** Register the V2 preview hook and trusted-host adapters. */
export function registerMagentoV2Runtime(): () => void {
  for (const kind of [
    "magento.manage_catalog",
    "magento.manage_inventory",
    "magento.manage_orders",
    "magento.manage_promotions",
    "magento.manage_content",
    "magento.manage_customers",
    "magento.undo_changeset",
  ]) {
    registerActionAdapter(kind, ({ request }) => executeChangeset(request));
  }
  registerActionAdapter("magento.financial_handoff", financialHandoffAdapter);
  return registerActionRequestCreatedHook(async (request) => {
    if (request.kind === "magento.financial_handoff") return;
    if (request.kind === "magento.undo_changeset") return prepareUndo(request);
    if (!request.kind.startsWith("magento.manage_")) return;
    const definition = await loadActionDefinition(request.orgId, request.kind);
    if (!definition || definition.adapter.kind !== "magento_changeset") {
      throw new Error(`Magento action definition ${request.kind} is unavailable`);
    }
    return prepareChangeset(request, definition);
  });
}
