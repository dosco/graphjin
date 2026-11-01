export const MAGENTO_DOMAINS = [
  "catalog",
  "inventory",
  "orders",
  "promotions",
  "content",
  "customers",
] as const;

export type MagentoDomain = (typeof MAGENTO_DOMAINS)[number];
export type MagentoRiskClass = 0 | 1 | 2;
export type MagentoExecutionMode =
  | "handoff_only"
  | "approval_required"
  | "controlled_automation_eligible";
export type MagentoAttributeCategory =
  | "financial"
  | "pii"
  | "content"
  | "operational";

export type MagentoAttributeClassification = {
  domain: MagentoDomain;
  entityType: string;
  attribute: string;
  riskClass: MagentoRiskClass;
  category: MagentoAttributeCategory;
  rationale: string;
};

export type MagentoDomainControl = {
  domain: MagentoDomain;
  riskClass: 1 | 2;
  enabled: boolean;
  autoExecute: boolean;
  caps: MagentoCaps;
};

export type MagentoCaps = {
  maxRowsPerChangeset: number;
  maxPriceDeltaPercent: number;
  maxDiscountPercent: number;
  maxCouponCount: number;
  maxProjectedExposure: number;
  maxDailyAutoActions: number;
  maxStoreCredit: number;
  minPromotionDays: number;
  skuCooldownSeconds: number;
};

export const DEFAULT_MAGENTO_CAPS: MagentoCaps = {
  maxRowsPerChangeset: 500,
  maxPriceDeltaPercent: 10,
  maxDiscountPercent: 30,
  maxCouponCount: 500,
  maxProjectedExposure: 5_000,
  maxDailyAutoActions: 25,
  maxStoreCredit: 50,
  minPromotionDays: 1,
  skuCooldownSeconds: 3_600,
};

export const DEFAULT_MAGENTO_DOMAIN_CONTROLS: MagentoDomainControl[] = [
  { domain: "catalog", riskClass: 2, enabled: true, autoExecute: false, caps: DEFAULT_MAGENTO_CAPS },
  { domain: "inventory", riskClass: 2, enabled: true, autoExecute: false, caps: DEFAULT_MAGENTO_CAPS },
  { domain: "orders", riskClass: 2, enabled: true, autoExecute: false, caps: DEFAULT_MAGENTO_CAPS },
  { domain: "promotions", riskClass: 1, enabled: true, autoExecute: false, caps: DEFAULT_MAGENTO_CAPS },
  { domain: "content", riskClass: 2, enabled: true, autoExecute: false, caps: DEFAULT_MAGENTO_CAPS },
  { domain: "customers", riskClass: 1, enabled: false, autoExecute: false, caps: DEFAULT_MAGENTO_CAPS },
];

export function magentoExecutionMode(riskClass: MagentoRiskClass): MagentoExecutionMode {
  if (riskClass === 0) return "handoff_only";
  if (riskClass === 1) return "approval_required";
  return "controlled_automation_eligible";
}

const classification = (
  domain: MagentoDomain,
  entityType: string,
  attribute: string,
  riskClass: MagentoRiskClass,
  category: MagentoAttributeCategory,
  rationale: string,
): MagentoAttributeClassification => ({
  domain,
  entityType,
  attribute,
  riskClass,
  category,
  rationale,
});

/**
 * Reviewed writable attributes shipped by the Magento pack. A leaf that is
 * not represented here deliberately requires administrator approval in
 * classifyMagentoChange, so Magento upgrades cannot silently widen the
 * automatic-execution surface.
 */
export const DEFAULT_MAGENTO_ATTRIBUTE_CLASSIFICATIONS: MagentoAttributeClassification[] = [
  ...[
    "name",
    "sku",
    "attribute_set_id",
    "type_id",
    "status",
    "visibility",
    "weight",
    "description",
    "short_description",
    "meta_title",
    "meta_keyword",
    "meta_description",
    "url_key",
    "website_ids",
    "category_links",
    "product_links",
    "media_gallery_entries",
  ].map((attribute) =>
    classification("catalog", "product", attribute, 2, attribute.includes("description") || attribute.startsWith("meta_") ? "content" : "operational", "Reversible catalog metadata"),
  ),
  ...["price", "special_price", "tier_price", "value"].map((attribute) =>
    classification("catalog", "product", attribute, 2, "financial", "Price changes can run automatically only within the configured delta limit"),
  ),
  classification("catalog", "product", "cost", 1, "financial", "Changes margin-sensitive cost data"),
  ...[
    "name",
    "is_active",
    "include_in_menu",
    "parent_id",
    "position",
    "available_sort_by",
    "default_sort_by",
    "description",
    "meta_title",
    "meta_keywords",
    "meta_description",
  ].map((attribute) =>
    classification("catalog", "category", attribute, 2, attribute.includes("description") || attribute.startsWith("meta_") ? "content" : "operational", "Reversible category configuration"),
  ),
  ...["source_code", "sku", "quantity", "status", "notify_stock_qty", "use_config_notify_stock_qty"].map((attribute) =>
    classification("inventory", "source_item", attribute, 2, "operational", "Reversible inventory operation with reservation-aware preview"),
  ),
  ...["comment", "is_customer_notified", "is_visible_on_front", "hold_before_state", "hold_before_status"].map((attribute) =>
    classification("orders", "order", attribute, 2, "operational", "Routine order operations"),
  ),
  ...["firstname", "lastname", "street", "city", "region", "postcode", "country_id", "telephone", "email"].map((attribute) =>
    classification("orders", "order", attribute, 1, "pii", "Customer address or identity data"),
  ),
  ...["carrier_code", "title", "track_number", "items", "capture", "notify", "append_comment"].map((attribute) =>
    classification("orders", "fulfillment", attribute, 1, "operational", "Customer-visible fulfillment state"),
  ),
  ...["name", "description", "from_date", "to_date", "is_active", "uses_per_customer", "stop_rules_processing", "sort_order"].map((attribute) =>
    classification("promotions", "sales_rule", attribute, 1, attribute === "description" ? "content" : "financial", "Promotion configuration changes financial exposure"),
  ),
  ...["discount_amount", "discount_qty", "discount_step", "times_used", "uses_per_coupon", "usage_limit", "qty"].map((attribute) =>
    classification("promotions", "sales_rule", attribute, 1, "financial", "Promotion or coupon exposure"),
  ),
  ...["identifier", "title", "content", "page_layout", "meta_title", "meta_keywords", "meta_description", "is_active", "store_id", "stores"].map((attribute) =>
    classification("content", "cms", attribute, 2, "content", "Versioned CMS content"),
  ),
  ...["firstname", "lastname", "email", "group_id", "dob", "taxvat", "gender", "addresses", "default_billing", "default_shipping"].map((attribute) =>
    classification("customers", "customer", attribute, 1, "pii", "Customer personal data; domain is off by default"),
  ),
];

const HANDOFF_ONLY_OPERATION_PATTERNS = [
  /refund/i,
  /return.*approve|approve.*return|rma.*approve/i,
  /payment.*config|tax.*config|currency.*rate/i,
  /integration|admin.*user|authorization.*role/i,
];

export function isMagentoHandoffOnlyOperation(operationId: string): boolean {
  return HANDOFF_ONLY_OPERATION_PATTERNS.some((pattern) => pattern.test(operationId));
}

const STRUCTURAL_KEYS = new Set([
  "product",
  "category",
  "sourceitems",
  "source_items",
  "statushistory",
  "salesrule",
  "rule",
  "coupon",
  "page",
  "block",
  "customer",
  "entity",
  "extension_attributes",
  "custom_attributes",
  "items",
]);

export function magentoLeafAttributes(value: unknown): string[] {
  const attributes = new Set<string>();
  const visit = (item: unknown, key?: string) => {
    if (Array.isArray(item)) {
      for (const child of item) visit(child, key);
      return;
    }
    if (item && typeof item === "object") {
      const record = item as Record<string, unknown>;
      if (
        typeof record.attribute_code === "string" &&
        Object.hasOwn(record, "value")
      ) {
        attributes.add(record.attribute_code.toLowerCase());
      }
      for (const [childKey, child] of Object.entries(record)) {
        visit(child, childKey);
      }
      return;
    }
    if (key && !STRUCTURAL_KEYS.has(key.toLowerCase())) {
      attributes.add(key.toLowerCase());
    }
  };
  visit(value);
  return [...attributes].sort();
}

export type MagentoClassificationResult = {
  riskClass: MagentoRiskClass;
  attributes: Array<{
    attribute: string;
    riskClass: MagentoRiskClass;
    category: MagentoAttributeCategory;
    reviewed: boolean;
  }>;
  reasons: string[];
};

export function classifyMagentoChange(input: {
  domain: MagentoDomain;
  entityType: string;
  operationId: string;
  defaultClass: 1 | 2;
  body: unknown;
  classifications?: readonly MagentoAttributeClassification[];
}): MagentoClassificationResult {
  if (isMagentoHandoffOnlyOperation(input.operationId)) {
    return {
      riskClass: 0,
      attributes: [],
      reasons: ["This operation must be completed by a human in Magento Admin"],
    };
  }
  const entries = input.classifications ?? DEFAULT_MAGENTO_ATTRIBUTE_CLASSIFICATIONS;
  const byAttribute = new Map(
    entries
      .filter(
        (entry) =>
          entry.domain === input.domain &&
          (entry.entityType === input.entityType || entry.entityType === "*"),
      )
      .map((entry) => [entry.attribute.toLowerCase(), entry]),
  );
  const attributes = magentoLeafAttributes(input.body).map((attribute) => {
    const entry = byAttribute.get(attribute);
    return entry
      ? {
          attribute,
          riskClass: entry.riskClass,
          category: entry.category,
          reviewed: true,
        }
      : {
          attribute,
          riskClass: 1 as const,
          category: "operational" as const,
          reviewed: false,
        };
  });
  const riskClass = attributes.reduce<MagentoRiskClass>(
    (current, attribute) => Math.min(current, attribute.riskClass) as MagentoRiskClass,
    input.defaultClass,
  );
  const reasons = attributes
    .filter((attribute) => !attribute.reviewed)
    .map((attribute) => `Unreviewed attribute ${attribute.attribute} requires administrator approval`);
  return { riskClass, attributes, reasons };
}

function finiteNumber(value: unknown): number | null {
  if (value === null || value === undefined || value === "") return null;
  const number = typeof value === "number" ? value : Number(value);
  return Number.isFinite(number) ? number : null;
}

function valuesForKeys(value: unknown, keys: Set<string>): number[] {
  const values: number[] = [];
  const visit = (item: unknown) => {
    if (Array.isArray(item)) {
      item.forEach(visit);
      return;
    }
    if (!item || typeof item !== "object") return;
    for (const [key, child] of Object.entries(item as Record<string, unknown>)) {
      if (keys.has(key.toLowerCase())) {
        const number = finiteNumber(child);
        if (number !== null) values.push(number);
      }
      visit(child);
    }
  };
  visit(value);
  return values;
}

function stringForKeys(value: unknown, keys: Set<string>): string | null {
  if (Array.isArray(value)) {
    for (const child of value) {
      const found = stringForKeys(child, keys);
      if (found) return found;
    }
    return null;
  }
  if (!value || typeof value !== "object") return null;
  for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
    if (keys.has(key.toLowerCase()) && typeof child === "string" && child.trim()) {
      return child.trim();
    }
    const found = stringForKeys(child, keys);
    if (found) return found;
  }
  return null;
}

export type MagentoCapEvaluation = {
  allowed: boolean;
  riskClass: MagentoRiskClass;
  violations: string[];
  escalations: string[];
  projectedExposure: number | null;
  snapshot: Record<string, unknown>;
};

export function evaluateMagentoCaps(input: {
  domain: MagentoDomain;
  operationId?: string;
  riskClass: MagentoRiskClass;
  rows: Array<{ beforeImage?: unknown; afterImage: unknown }>;
  caps?: Partial<MagentoCaps>;
  projectedExposure?: number | null;
  autoExecution?: boolean;
  autoActionsToday?: number;
  cooldownBreaches?: string[];
  now?: Date;
}): MagentoCapEvaluation {
  const caps = { ...DEFAULT_MAGENTO_CAPS, ...(input.caps ?? {}) };
  const violations: string[] = [];
  const escalations: string[] = [];
  let riskClass = input.riskClass;
  if (input.rows.length > caps.maxRowsPerChangeset) {
    violations.push(`Change-set has ${input.rows.length} rows; maximum is ${caps.maxRowsPerChangeset}`);
  }

  const priceKeys = new Set(["price", "special_price", "value"]);
  for (const row of input.rows) {
    const before = valuesForKeys(row.beforeImage, priceKeys);
    const after = valuesForKeys(row.afterImage, priceKeys);
    for (let index = 0; index < Math.min(before.length, after.length); index++) {
      const prior = before[index]!;
      const next = after[index]!;
      if (prior <= 0) continue;
      const delta = Math.abs(((next - prior) / prior) * 100);
      if (delta > caps.maxPriceDeltaPercent) {
        riskClass = Math.min(riskClass, 1) as MagentoRiskClass;
        escalations.push(
          `Price delta ${delta.toFixed(2)}% exceeds the ${caps.maxPriceDeltaPercent}% automatic-execution limit and requires administrator approval`,
        );
      }
    }
  }

  if (input.domain === "promotions") {
    const discounts = input.rows.flatMap((row) =>
      valuesForKeys(row.afterImage, new Set(["discount_amount"])),
    );
    if (discounts.some((value) => value >= 90)) {
      violations.push("A promotion that can create a free or near-free cart is not executable");
      riskClass = 0;
    } else if (discounts.some((value) => value > caps.maxDiscountPercent)) {
      violations.push(`Promotion discount exceeds the ${caps.maxDiscountPercent}% ceiling`);
    }
    const couponCounts = input.rows.flatMap((row) =>
      valuesForKeys(row.afterImage, new Set(["quantity", "qty", "coupon_count"])),
    );
    if (couponCounts.some((value) => value > caps.maxCouponCount)) {
      violations.push(`Coupon count exceeds the ${caps.maxCouponCount} ceiling`);
    }
    for (const row of input.rows) {
      if (["magentoDeleteSalesRule", "magentoGenerateCoupons"].includes(input.operationId ?? "")) continue;
      const effectiveRule = { ...(row.beforeImage && typeof row.beforeImage === "object" ? row.beforeImage : {}),
        ...(row.afterImage && typeof row.afterImage === "object" && "rule" in row.afterImage
          ? row.afterImage.rule as Record<string, unknown> : row.afterImage as Record<string, unknown>) };
      const start = stringForKeys(effectiveRule, new Set(["from_date"]));
      const end = stringForKeys(effectiveRule, new Set(["to_date"]));
      if (!end) violations.push("Promotion expiry is required");
      if (start && end) {
        const duration = (Date.parse(end) - Date.parse(start)) / 86_400_000;
        if (Number.isFinite(duration) && duration < caps.minPromotionDays) {
          violations.push(`Promotion duration must be at least ${caps.minPromotionDays} day(s)`);
        }
      }
    }
  }

  const projectedExposure = finiteNumber(input.projectedExposure);
  if (
    projectedExposure !== null &&
    projectedExposure > caps.maxProjectedExposure
  ) {
    violations.push(
      `Projected exposure ${projectedExposure} exceeds the ${caps.maxProjectedExposure} ceiling`,
    );
  }

  if (input.autoExecution) {
    if (riskClass !== 2) violations.push("This change requires administrator approval and cannot execute automatically");
    if ((input.autoActionsToday ?? 0) >= caps.maxDailyAutoActions) {
      violations.push(`Daily automatic-action cap of ${caps.maxDailyAutoActions} is exhausted`);
    }
    for (const entity of input.cooldownBreaches ?? []) {
      violations.push(`Cooldown is still active for ${entity}`);
    }
  }

  return {
    allowed: violations.length === 0 && riskClass !== 0,
    riskClass,
    violations,
    escalations,
    projectedExposure,
    snapshot: {
      caps,
      evaluatedAt: (input.now ?? new Date()).toISOString(),
      rowCount: input.rows.length,
      autoExecution: input.autoExecution ?? false,
      autoActionsToday: input.autoActionsToday ?? 0,
    },
  };
}
