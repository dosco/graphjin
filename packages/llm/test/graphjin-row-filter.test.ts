import { describe, expect, it } from "vitest";
import { compileRowFilter, parseRowFilter, RowFilterError } from "../src/graphjin/row-filter";

describe("row filters", () => {
  it("compiles nested conditions, lists and trusted variables", () => {
    const filter = parseRowFilter({
      or: [
        { and: [{ column: "region", op: "eq", value: "emea" }, { column: "amount", op: "gte", value: 100 }] },
        { column: "team", op: "in", value: { var: "user_groups" } },
        { column: "owner_id", op: "eq", value: { var: "user_id" } },
        { column: "store_id", op: "nin", value: [1, 2] },
        { column: "deleted_at", op: "is_null", value: true },
      ],
    }, ["region", "amount", "team", "owner_id", "store_id", "deleted_at"]);
    expect(compileRowFilter(filter)).toBe(
      '{ or: [{ and: [{ region: { eq: "emea" } }, { amount: { gte: 100 } }] }, { team: { in: $user_groups } }, ' +
        '{ owner_id: { eq: $user_id } }, { store_id: { nin: [1, 2] } }, { deleted_at: { is_null: true } }] }',
    );
  });

  it("quotes string literals so they cannot change the filter", () => {
    const compiled = compileRowFilter(parseRowFilter({ column: "name", op: "eq", value: 'x" } } { id: { gt: 0' }));
    expect(compiled).toBe('{ name: { eq: "x\\" } } { id: { gt: 0" } }');
  });

  it.each([
    [{ and: [] }, "needs at least one condition"],
    [{ or: [{}] }, "column is not a valid column name"],
    [{ column: "a;drop", op: "eq", value: 1 }, "not a valid column name"],
    [{ column: "secret", op: "eq", value: 1 }, 'column "secret" is not a column'],
    [{ column: "a", op: "like", value: "x" }, "op must be one of"],
    [{ column: "a", op: "in", value: [] }, "non-empty list"],
    [{ column: "a", op: "eq", value: { var: "user_groups" } }, "needs in or nin"],
    [{ column: "a", op: "in", value: { var: "user_id" } }, "needs a single-value operator"],
    [{ column: "a", op: "eq", value: { var: "password" } }, "value.var must be one of"],
    [{ column: "a", op: "is_null", value: "yes" }, "true or false"],
    [{ column: "a", op: "eq", value: null }, "string, number or boolean"],
    [{ and: [{ column: "a", op: "eq", value: 1 }], or: [] }, "must be the only key"],
  ])("rejects %j", (input, message) => {
    expect(() => parseRowFilter(input, ["a"])).toThrow(RowFilterError);
    expect(() => parseRowFilter(input, ["a"])).toThrow(message);
  });

  it("limits depth and size", () => {
    let deep: unknown = { column: "a", op: "eq", value: 1 };
    for (let i = 0; i < 8; i++) deep = { and: [deep] };
    expect(() => parseRowFilter(deep)).toThrow("nested too deeply");
    expect(() => parseRowFilter({ or: Array.from({ length: 101 }, () => ({ column: "a", op: "eq", value: 1 })) })).toThrow("too many");
  });
});
