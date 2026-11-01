import { describe, expect, it } from "vitest";
import {
  buildSubscriptionQuery,
  parseSourceChangeFilter,
  parseSourceChangeMatch,
  parseWorkflowOutputMatch,
  type SourceChangeFilter,
} from "../src/workflows/subscription-query";

describe("buildSubscriptionQuery (workflow_output)", () => {
  it("scopes by org_id and translates scope/mood/topic/kinds", () => {
    const payload = buildSubscriptionQuery({
      sourceKind: "workflow_output",
      orgId: "org-abc",
      filter: {
        scope: "apac_churn",
        topic: "partner_renewal",
        mood: ["watch", "act"],
        kinds: ["finding", "recommendation"],
      },
    });
    expect(payload).not.toBeNull();
    expect(payload!.variables).toEqual({
      where: {
        org_id: { eq: "org-abc" },
        scope: { eq: "apac_churn" },
        topic: { eq: "partner_renewal" },
        mood: { in: ["watch", "act"] },
        kind: { in: ["finding", "recommendation"] },
      },
    });
    expect(payload!.query).toContain("subscription WorkflowOutputMatch");
    expect(payload!.query).toContain("workflow_output");
  });

  it("returns null for source_kinds not yet wired", () => {
    expect(
      buildSubscriptionQuery({
        sourceKind: "external_event",
        orgId: "org-abc",
        filter: {},
      }),
    ).toBeNull();
  });
});

describe("parseWorkflowOutputMatch", () => {
  it("extracts the first workflow_output row from a subscription payload", () => {
    const match = parseWorkflowOutputMatch({
      data: {
        workflow_output: [
          {
            id: "out-1",
            org_id: "org-abc",
            workflow_run_id: "wfr-1",
            kind: "finding",
            scope: "apac_churn",
            topic: null,
            mood: "watch",
            title: "Churn spike",
            created_at: "2026-05-13T12:00:00.000Z",
          },
        ],
      },
    });
    expect(match?.id).toBe("out-1");
    expect(match?.mood).toBe("watch");
    expect(match?.topic).toBeNull();
  });

  it("returns null on empty payload", () => {
    expect(parseWorkflowOutputMatch(null)).toBeNull();
    expect(
      parseWorkflowOutputMatch({ data: { workflow_output: [] } }),
    ).toBeNull();
  });
});

describe("buildSubscriptionQuery (source_change)", () => {
  it("inlines the where and projects pk + select + version", () => {
    const payload = buildSubscriptionQuery({
      sourceKind: "source_change",
      orgId: "org-abc",
      filter: {
        table: "productinventory",
        where: {
          quantity: { lt: { col: "product.reorderpoint" } },
        },
        select: ["quantity"],
        primary_key: ["productid", "locationid"],
        version_column: "modifieddate",
      },
    });
    expect(payload).not.toBeNull();
    // GraphJin rejects `where` passed as a variable, so it's inlined and no
    // GraphQL variables are emitted.
    expect(payload!.variables).toEqual({});
    expect(payload!.query).toContain("subscription SourceChangeMatch {");
    expect(payload!.query).not.toContain("$where");
    expect(payload!.query).toContain(
      "where: { quantity: { lt: { col: \"product.reorderpoint\" } } }",
    );
    expect(payload!.query).toContain("order_by: { modifieddate: desc }, limit: 1");
    // pk columns + select + version_column, deduped and ordered
    expect(payload!.query).toContain("productid");
    expect(payload!.query).toContain("locationid");
    expect(payload!.query).toContain("quantity");
    expect(payload!.query).toContain("modifieddate");
  });

  it("does NOT inject org_id (operator tables have no such column)", () => {
    const payload = buildSubscriptionQuery({
      sourceKind: "source_change",
      orgId: "org-abc",
      filter: {
        table: "productinventory",
        where: { quantity: { lt: 50 } },
        primary_key: ["productid", "locationid"],
      },
    });
    expect(payload!.query).toContain("where: { quantity: { lt: 50 } }");
    expect(payload!.query).not.toContain("org_id");
  });

  it("falls back to primary_key[0] for order_by when version_column omitted", () => {
    const payload = buildSubscriptionQuery({
      sourceKind: "source_change",
      orgId: "org-abc",
      filter: {
        table: "orders",
        where: {},
        primary_key: ["id"],
      },
    });
    expect(payload!.query).toContain("order_by: { id: desc }");
    // empty where → the argument is omitted entirely
    expect(payload!.query).not.toContain("where:");
  });

  it("returns null for invalid filter shape (missing table)", () => {
    expect(
      buildSubscriptionQuery({
        sourceKind: "source_change",
        orgId: "org-abc",
        filter: { primary_key: ["id"] },
      }),
    ).toBeNull();
  });

  it("returns null for invalid filter shape (missing primary_key)", () => {
    expect(
      buildSubscriptionQuery({
        sourceKind: "source_change",
        orgId: "org-abc",
        filter: { table: "productinventory" },
      }),
    ).toBeNull();
  });

  it("rejects non-identifier table/column names (defense in depth)", () => {
    expect(
      buildSubscriptionQuery({
        sourceKind: "source_change",
        orgId: "org-abc",
        filter: {
          table: "products; drop table users; --",
          primary_key: ["id"],
        },
      }),
    ).toBeNull();
    expect(
      buildSubscriptionQuery({
        sourceKind: "source_change",
        orgId: "org-abc",
        filter: {
          table: "productinventory",
          primary_key: ["id; --"],
        },
      }),
    ).toBeNull();
  });
});

describe("parseSourceChangeFilter", () => {
  it("returns the filter for a valid shape", () => {
    const parsed = parseSourceChangeFilter({
      table: "productinventory",
      where: { quantity: { lt: 100 } },
      primary_key: ["productid", "locationid"],
      version_column: "modifieddate",
    });
    expect(parsed).not.toBeNull();
    expect(parsed!.table).toBe("productinventory");
    expect(parsed!.primary_key).toEqual(["productid", "locationid"]);
  });

  it("rejects an empty primary_key array", () => {
    expect(
      parseSourceChangeFilter({
        table: "productinventory",
        primary_key: [],
      }),
    ).toBeNull();
  });
});

describe("parseSourceChangeMatch", () => {
  const filter: SourceChangeFilter = {
    table: "productinventory",
    where: {},
    primary_key: ["productid", "locationid"],
    version_column: "modifieddate",
  };

  it("extracts composite primary_key + snapshot + version_token", () => {
    const match = parseSourceChangeMatch(
      {
        data: {
          productinventory: [
            {
              productid: 680,
              locationid: 6,
              quantity: 12,
              modifieddate: "2026-05-23T10:00:00.000Z",
            },
          ],
        },
      },
      filter,
    );
    expect(match).not.toBeNull();
    expect(match!.table).toBe("productinventory");
    expect(match!.primary_key).toEqual({ productid: 680, locationid: 6 });
    expect(match!.snapshot.quantity).toBe(12);
    expect(match!.version_token).toBe("2026-05-23T10:00:00.000Z");
  });

  it("returns null version_token when version_column unset on filter", () => {
    const match = parseSourceChangeMatch(
      {
        data: {
          productinventory: [{ productid: 1, locationid: 1, quantity: 50 }],
        },
      },
      { ...filter, version_column: undefined },
    );
    expect(match?.version_token).toBeNull();
  });

  it("returns null on empty result", () => {
    expect(parseSourceChangeMatch(null, filter)).toBeNull();
    expect(
      parseSourceChangeMatch({ data: { productinventory: [] } }, filter),
    ).toBeNull();
  });

  it("returns null when a primary_key column is missing on the row", () => {
    const match = parseSourceChangeMatch(
      {
        data: {
          productinventory: [{ productid: 1, quantity: 5 }],
        },
      },
      filter,
    );
    expect(match).toBeNull();
  });
});
