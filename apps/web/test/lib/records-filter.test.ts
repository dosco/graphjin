import { describe, expect, it } from "vitest";
import {
  buildFilter,
  describeLeaf,
  filterClauses,
  filterConditions,
  filterLeaf,
  hrefWithQuery,
  leafCondition,
  PAGING_RESET,
  type RecordFilterField,
} from "@/lib/records-filter";

const platform: RecordFilterField = {
  apiName: "platform",
  label: "Platform",
  kind: "picklist",
  picklistValues: ["linkedin", { value: "facebook", label: "Facebook" }],
};
const scheduled: RecordFilterField = { apiName: "scheduled_date", label: "Scheduled date", kind: "date", picklistValues: null };
const reach: RecordFilterField = { apiName: "reach", label: "Reach", kind: "integer", picklistValues: null };
const done: RecordFilterField = { apiName: "done", label: "Done", kind: "boolean", picklistValues: null };
const title: RecordFilterField = { apiName: "title", label: "Title", kind: "text", picklistValues: null };

describe("records filter model", () => {
  it("offers conditions that fit each field kind", () => {
    expect(filterConditions(platform).map((c) => c.value)).toEqual(["in", "not_in", "empty", "not_empty"]);
    expect(filterConditions(scheduled).map((c) => c.input)).toContain("date");
    expect(filterConditions(reach).every((c) => c.input === "number" || c.input === "none")).toBe(true);
    expect(filterConditions(done)[0]).toMatchObject({ value: "eq", input: "boolean" });
    expect(filterConditions(title)[0]).toMatchObject({ value: "contains", input: "text" });
  });

  it("only offers open and closed when the picklist defines them", () => {
    const stage = { ...platform, picklistValues: [{ value: "new", semantic: "open" }] };
    expect(filterConditions(stage).map((c) => c.value)).toContain("is_open");
    expect(filterConditions(stage).map((c) => c.value)).not.toContain("is_closed");
  });

  it("builds typed clauses and rejects incomplete input", () => {
    expect(filterLeaf(platform, "in", ["linkedin", "facebook"])).toEqual({ field: "platform", operator: "in", value: ["linkedin", "facebook"] });
    expect(filterLeaf(platform, "in", [])).toBeNull();
    expect(filterLeaf(reach, "gte", "10")).toEqual({ field: "reach", operator: "gte", value: 10 });
    expect(filterLeaf(reach, "gte", "ten")).toBeNull();
    expect(filterLeaf(done, "eq", "false")).toEqual({ field: "done", operator: "eq", value: false });
    expect(filterLeaf(scheduled, "last_n_days", "7")).toEqual({
      field: "scheduled_date",
      operator: "relative",
      value: { macro: "last_n_days", days: 7 },
    });
    expect(filterLeaf(title, "empty", "")).toEqual({ field: "title", operator: "is_null", value: true });
  });

  it("round-trips a clause through the editor state", () => {
    const leaf = filterLeaf(platform, "not_in", ["facebook"])!;
    const { condition, raw } = leafCondition(leaf);
    expect(filterLeaf(platform, condition, raw)).toEqual(leaf);
  });

  it("describes clauses with choice labels", () => {
    expect(describeLeaf({ field: "platform", operator: "in", value: ["linkedin", "facebook"] }, platform)).toBe("is any of linkedin, Facebook");
    expect(describeLeaf({ field: "title", operator: "is_null", value: false }, title)).toBe("is not empty");
    expect(describeLeaf({ field: "scheduled_date", operator: "eq", value: "2026-10-13" }, scheduled)).toBe("is on Oct 13, 2026");
  });

  it("flattens and rebuilds filters with the chosen match", () => {
    const a = { field: "title", operator: "contains" as const, value: "day" };
    const b = { field: "done", operator: "eq" as const, value: true };
    expect(filterClauses({ op: "or", clauses: [a, b] })).toEqual({ match: "or", clauses: [a, b] });
    expect(buildFilter("or", [a, b])).toEqual({ op: "or", clauses: [a, b] });
    expect(buildFilter("and", [a])).toEqual(a);
    expect(buildFilter("and", [])).toBeNull();
  });

  it("resets paging when the result set changes", () => {
    const href = hrefWithQuery("/a/x/post", { q: "day", after: "c2", page: "3", back: "c1" }, { ...PAGING_RESET, q: "night" });
    expect(href).toBe("/a/x/post?q=night");
  });
});
