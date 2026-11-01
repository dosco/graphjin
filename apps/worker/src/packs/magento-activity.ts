type JsonRecord = Record<string, unknown>;

export function isMagentoTestRule(rule: {
  name: string;
  compiledPolicy: unknown;
}): boolean {
  const policy = rule.compiledPolicy;
  const source = policy && typeof policy === "object" && !Array.isArray(policy)
    ? (policy as JsonRecord).source
    : null;
  return source === "acceptance_test" || /^Local acceptance(?:\s|$)/i.test(rule.name);
}

export type MagentoActivityChangeset = {
  id: string;
  domain: string;
  operationId: string;
  executionMode: string;
  status: string;
  summary: string;
  bulkUuid: string | null;
  inverseOfId: string | null;
  scope: JsonRecord;
  capSnapshot: JsonRecord;
  createdAt: Date | string;
  reconciledAt: Date | string | null;
  rows: Array<{
    entityRef: string;
    beforeImage: unknown;
    afterImage: unknown;
  }>;
};

export type MagentoActivityHandoff = {
  id: string;
  kind: string;
  entityRef: string;
  status: string;
  draft: JsonRecord;
  evidence: JsonRecord;
  createdAt: Date | string;
  completedAt: Date | string | null;
};

export type MagentoActivityItem = {
  id: string;
  kind: "change" | "handoff";
  title: string;
  description: string;
  outcome:
    | "completed"
    | "reverted"
    | "awaiting_approval"
    | "in_progress"
    | "needs_attention"
    | "failed"
    | "cancelled";
  outcomeLabel: string;
  affectedCount: number;
  source: "requested_change" | "automatic_rule" | "test";
  sourceLabel: string;
  isTest: boolean;
  occurredAt: Date | string;
  currentState: string | null;
  technical: {
    reference: string;
    area: string;
    operation: string;
    execution: string;
    originalRequest: string;
    bulkReference: string | null;
    inverseOfReference: string | null;
  };
};

type ChangeDescriptor = {
  requestLabel: string;
  completedTitle: string;
  revertedTitle: string;
  completedDescription: string;
  revertedDescription: string;
  currentState: string;
};

const DOMAIN_LABELS: Record<string, string> = {
  catalog: "Catalog",
  content: "Content",
  customers: "Customers",
  inventory: "Inventory",
  orders: "Orders",
  promotions: "Promotions",
};

function plural(count: number, singular: string, pluralForm = `${singular}s`): string {
  return `${count} ${count === 1 ? singular : pluralForm}`;
}

function hasKey(value: unknown, expected: string): boolean {
  if (!value || typeof value !== "object") return false;
  if (Array.isArray(value)) return value.some((item) => hasKey(item, expected));
  return Object.entries(value as JsonRecord).some(
    ([key, child]) => key.toLowerCase() === expected || hasKey(child, expected),
  );
}

function describesPriceChange(changeset: MagentoActivityChangeset): boolean {
  return changeset.operationId.includes("price") || changeset.rows.some(
    (row) => hasKey(row.afterImage, "price") || hasKey(row.beforeImage, "price"),
  );
}

function descriptor(changeset: MagentoActivityChangeset): ChangeDescriptor {
  const count = changeset.rows.length;
  const operation = changeset.operationId;

  if (changeset.domain === "catalog" && describesPriceChange(changeset)) {
    return {
      requestLabel: "Price update",
      completedTitle: count === 1 ? "Price updated" : "Prices updated",
      revertedTitle: count === 1 ? "Price restored" : "Prices restored",
      completedDescription: `Updated prices for ${plural(count, "product")}.`,
      revertedDescription: `Restored the original prices for ${plural(count, "product")}.`,
      currentState: "No current price change",
    };
  }

  if (operation.startsWith("product_")) {
    return {
      requestLabel: "Product update",
      completedTitle: count === 1 ? "Product updated" : "Products updated",
      revertedTitle: count === 1 ? "Product restored" : "Products restored",
      completedDescription: `Updated ${plural(count, "product")}.`,
      revertedDescription: `Restored the original details for ${plural(count, "product")}.`,
      currentState: "No current product change",
    };
  }

  if (operation.includes("category")) {
    return {
      requestLabel: "Category update",
      completedTitle: count === 1 ? "Category updated" : "Categories updated",
      revertedTitle: count === 1 ? "Category restored" : "Categories restored",
      completedDescription: `Updated ${plural(count, "category", "categories")}.`,
      revertedDescription: `Restored the original category details for ${plural(count, "category", "categories")}.`,
      currentState: "No current category change",
    };
  }

  if (changeset.domain === "inventory") {
    return {
      requestLabel: "Inventory update",
      completedTitle: "Inventory updated",
      revertedTitle: "Inventory restored",
      completedDescription: `Updated ${plural(count, "inventory item")}.`,
      revertedDescription: `Restored the original values for ${plural(count, "inventory item")}.`,
      currentState: "No current inventory change",
    };
  }

  if (changeset.domain === "orders") {
    return {
      requestLabel: "Order update",
      completedTitle: count === 1 ? "Order updated" : "Orders updated",
      revertedTitle: count === 1 ? "Order restored" : "Orders restored",
      completedDescription: `Updated ${plural(count, "order")}.`,
      revertedDescription: `Restored the original state for ${plural(count, "order")}.`,
      currentState: "No current order change",
    };
  }

  if (changeset.domain === "promotions") {
    const target = operation.includes("coupon") ? "coupon" : "promotion";
    return {
      requestLabel: target === "coupon" ? "Coupon update" : "Promotion update",
      completedTitle: target === "coupon" ? "Coupons updated" : "Promotion updated",
      revertedTitle: target === "coupon" ? "Coupons restored" : "Promotion restored",
      completedDescription: `Updated ${plural(count, target)}.`,
      revertedDescription: `Restored the original details for ${plural(count, target)}.`,
      currentState: `No current ${target} change`,
    };
  }

  if (changeset.domain === "content") {
    const target = operation.includes("page") ? "content page" : "content block";
    return {
      requestLabel: "Content update",
      completedTitle: "Content updated",
      revertedTitle: "Content restored",
      completedDescription: `Updated ${plural(count, target)}.`,
      revertedDescription: `Restored the original content for ${plural(count, target)}.`,
      currentState: "No current content change",
    };
  }

  if (changeset.domain === "customers") {
    return {
      requestLabel: "Customer update",
      completedTitle: count === 1 ? "Customer updated" : "Customers updated",
      revertedTitle: count === 1 ? "Customer restored" : "Customers restored",
      completedDescription: `Updated ${plural(count, "customer")}.`,
      revertedDescription: `Restored the original details for ${plural(count, "customer")}.`,
      currentState: "No current customer change",
    };
  }

  const area = DOMAIN_LABELS[changeset.domain] ?? "Store";
  return {
    requestLabel: `${area} update`,
    completedTitle: `${area} updated`,
    revertedTitle: `${area} restored`,
    completedDescription: `Completed ${plural(count, "store change")}.`,
    revertedDescription: `Restored the original state for ${plural(count, "store change")}.`,
    currentState: `No current ${area.toLowerCase()} change`,
  };
}

function changesetSource(changeset: MagentoActivityChangeset): Pick<
  MagentoActivityItem,
  "source" | "sourceLabel" | "isTest"
> {
  const legacyAcceptanceSummary = /^(?:Undo:\s*)?Acceptance\b/i.test(changeset.summary);
  if (changeset.scope.activityOrigin === "acceptance_test" || legacyAcceptanceSummary) {
    return { source: "test", sourceLabel: "Local test", isTest: true };
  }
  if (typeof changeset.capSnapshot.autoRuleId === "string") {
    return { source: "automatic_rule", sourceLabel: "Automatic rule", isTest: false };
  }
  return { source: "requested_change", sourceLabel: "Requested change", isTest: false };
}

function statusPresentation(
  changeset: MagentoActivityChangeset,
  change: ChangeDescriptor,
): Pick<MagentoActivityItem, "title" | "description" | "outcome" | "outcomeLabel" | "currentState"> {
  const isCompletedUndo = Boolean(changeset.inverseOfId) && changeset.status === "applied";
  if (isCompletedUndo) {
    return {
      title: change.revertedTitle,
      description: change.revertedDescription,
      outcome: "reverted",
      outcomeLabel: "Reverted",
      currentState: change.currentState,
    };
  }
  if (changeset.status === "applied") {
    return {
      title: change.completedTitle,
      description: change.completedDescription,
      outcome: "completed",
      outcomeLabel: "Completed",
      currentState: null,
    };
  }
  if (changeset.status === "pending_approval" || changeset.status === "previewed") {
    return {
      title: `${change.requestLabel} awaiting approval`,
      description: "An administrator needs to review this change before anything is updated in Magento.",
      outcome: "awaiting_approval",
      outcomeLabel: "Awaiting approval",
      currentState: "Nothing changed yet",
    };
  }
  if (changeset.status === "approved" || changeset.status === "executing") {
    return {
      title: `${change.requestLabel} in progress`,
      description: "Magento is applying the approved change now.",
      outcome: "in_progress",
      outcomeLabel: "In progress",
      currentState: null,
    };
  }
  if (changeset.status === "partially_applied" || changeset.status === "reconcile_required") {
    return {
      title: `${change.requestLabel} needs attention`,
      description: "Magento could not confirm every requested update. Review the technical details before trying again.",
      outcome: "needs_attention",
      outcomeLabel: "Needs attention",
      currentState: "Current state needs review",
    };
  }
  if (changeset.status === "failed") {
    return {
      title: `${change.requestLabel} failed`,
      description: "The requested change could not be completed in Magento.",
      outcome: "failed",
      outcomeLabel: "Failed",
      currentState: "Current store data was not confirmed",
    };
  }
  return {
    title: `${change.requestLabel} cancelled`,
    description: "The requested change was cancelled before it completed.",
    outcome: "cancelled",
    outcomeLabel: "Cancelled",
    currentState: "Nothing changed",
  };
}

function changeActivity(changeset: MagentoActivityChangeset): MagentoActivityItem {
  const change = descriptor(changeset);
  return {
    id: changeset.id,
    kind: "change",
    ...statusPresentation(changeset, change),
    affectedCount: changeset.rows.length,
    ...changesetSource(changeset),
    occurredAt: changeset.reconciledAt ?? changeset.createdAt,
    technical: {
      reference: changeset.id,
      area: DOMAIN_LABELS[changeset.domain] ?? changeset.domain,
      operation: changeset.operationId,
      execution: changeset.executionMode,
      originalRequest: changeset.summary.replace(/^Undo:\s*/i, ""),
      bulkReference: changeset.bulkUuid,
      inverseOfReference: changeset.inverseOfId,
    },
  };
}

const HANDOFF_LABELS: Record<string, { title: string; description: string }> = {
  online_refund: {
    title: "Refund ready in Magento Admin",
    description: "Review the evidence and complete the refund in Magento Admin.",
  },
  return_approval: {
    title: "Return ready for review",
    description: "Review and approve the return in Magento Admin.",
  },
  financial_configuration: {
    title: "Financial setting ready for review",
    description: "Review and make this sensitive configuration change in Magento Admin.",
  },
  store_credit_over_cap: {
    title: "Store credit needs administrator review",
    description: "Review the evidence and complete the store credit change in Magento Admin.",
  },
};

function handoffActivity(handoff: MagentoActivityHandoff): MagentoActivityItem {
  const copy = HANDOFF_LABELS[handoff.kind] ?? {
    title: "Magento task ready for review",
    description: "Review the evidence and complete this task in Magento Admin.",
  };
  const completed = handoff.status === "completed_by_human";
  const cancelled = handoff.status === "cancelled";
  const isTest = handoff.draft.testOnly === true || String(handoff.evidence.reason ?? "").includes("acceptance");
  return {
    id: handoff.id,
    kind: "handoff",
    title: completed ? copy.title.replace("ready", "completed") : copy.title,
    description: copy.description,
    outcome: cancelled ? "cancelled" : completed ? "completed" : "awaiting_approval",
    outcomeLabel: cancelled ? "Cancelled" : completed ? "Completed" : "Action needed",
    affectedCount: 1,
    source: isTest ? "test" : "requested_change",
    sourceLabel: isTest ? "Local test" : "Requested change",
    isTest,
    occurredAt: handoff.completedAt ?? handoff.createdAt,
    currentState: completed ? null : "OpenNeko did not change Magento",
    technical: {
      reference: handoff.id,
      area: "Financial handoff",
      operation: handoff.kind,
      execution: "Complete in Magento Admin",
      originalRequest: handoff.entityRef,
      bulkReference: null,
      inverseOfReference: null,
    },
  };
}

function timestamp(value: Date | string): number {
  const parsed = value instanceof Date ? value.getTime() : Date.parse(value);
  return Number.isFinite(parsed) ? parsed : 0;
}

export function buildMagentoActivity(input: {
  changesets: MagentoActivityChangeset[];
  handoffs: MagentoActivityHandoff[];
}): MagentoActivityItem[] {
  const successfullyReverted = new Set(
    input.changesets.flatMap((changeset) =>
      changeset.inverseOfId && changeset.status === "applied" ? [changeset.inverseOfId] : [],
    ),
  );
  return [
    ...input.changesets
      .filter((changeset) => !successfullyReverted.has(changeset.id))
      .map(changeActivity),
    ...input.handoffs.map(handoffActivity),
  ].sort((left, right) => timestamp(right.occurredAt) - timestamp(left.occurredAt));
}
