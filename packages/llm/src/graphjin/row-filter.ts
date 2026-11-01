/**
 * Structured row filters for group data access. Admins build these trees in
 * the UI; the compiler turns them into GraphJin filter strings. Free text
 * never reaches the GraphJin config.
 */

export const ROW_FILTER_OPS = ["eq", "neq", "gt", "gte", "lt", "lte", "in", "nin", "is_null"] as const;
export type RowFilterOp = (typeof ROW_FILTER_OPS)[number];

export const ROW_FILTER_VARIABLES = ["user_id", "user_groups", "account_id"] as const;
export type RowFilterVariable = (typeof ROW_FILTER_VARIABLES)[number];

export type RowFilterLiteral = string | number | boolean;
export type RowFilterValue = RowFilterLiteral | RowFilterLiteral[] | { var: RowFilterVariable };

export type RowFilter =
  | { and: RowFilter[] }
  | { or: RowFilter[] }
  | { column: string; op: RowFilterOp; value: RowFilterValue };

export class RowFilterError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "RowFilterError";
  }
}

const COLUMN = /^[A-Za-z_][A-Za-z0-9_$]*$/;
const MAX_DEPTH = 6;
const MAX_NODES = 100;

function isObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isLiteral(value: unknown): value is RowFilterLiteral {
  return typeof value === "string" || typeof value === "boolean" || (typeof value === "number" && Number.isFinite(value));
}

/** Parses untrusted JSON into a row filter, or throws RowFilterError. */
export function parseRowFilter(input: unknown, columns?: readonly string[]): RowFilter {
  let nodes = 0;
  const known = columns ? new Set(columns) : null;
  const visit = (value: unknown, depth: number, path: string): RowFilter => {
    if (++nodes > MAX_NODES) throw new RowFilterError("row filter has too many conditions");
    if (depth > MAX_DEPTH) throw new RowFilterError("row filter is nested too deeply");
    if (!isObject(value)) throw new RowFilterError(`${path} must be an object`);
    for (const group of ["and", "or"] as const) {
      if (group in value) {
        const children = value[group];
        if (Object.keys(value).length !== 1) throw new RowFilterError(`${path}.${group} must be the only key`);
        if (!Array.isArray(children) || children.length === 0) {
          throw new RowFilterError(`${path}.${group} needs at least one condition`);
        }
        const parsed = children.map((child, i) => visit(child, depth + 1, `${path}.${group}[${i}]`));
        return group === "and" ? { and: parsed } : { or: parsed };
      }
    }
    const { column, op, value: operand } = value;
    if (typeof column !== "string" || !COLUMN.test(column)) throw new RowFilterError(`${path}.column is not a valid column name`);
    if (known && !known.has(column)) throw new RowFilterError(`${path}.column "${column}" is not a column of the table`);
    if (typeof op !== "string" || !(ROW_FILTER_OPS as readonly string[]).includes(op)) {
      throw new RowFilterError(`${path}.op must be one of ${ROW_FILTER_OPS.join(", ")}`);
    }
    const typedOp = op as RowFilterOp;
    if (typedOp === "is_null") {
      if (typeof operand !== "boolean") throw new RowFilterError(`${path}.value must be true or false for is_null`);
      return { column, op: typedOp, value: operand };
    }
    if (isObject(operand)) {
      const variable = operand.var;
      if (Object.keys(operand).length !== 1 || typeof variable !== "string" || !(ROW_FILTER_VARIABLES as readonly string[]).includes(variable)) {
        throw new RowFilterError(`${path}.value.var must be one of ${ROW_FILTER_VARIABLES.join(", ")}`);
      }
      const listVariable = variable === "user_groups";
      if (listVariable !== (typedOp === "in" || typedOp === "nin")) {
        throw new RowFilterError(`${path}: $${variable} needs ${listVariable ? "in or nin" : "a single-value operator"}`);
      }
      return { column, op: typedOp, value: { var: variable as RowFilterVariable } };
    }
    if (typedOp === "in" || typedOp === "nin") {
      if (!Array.isArray(operand) || operand.length === 0 || !operand.every(isLiteral)) {
        throw new RowFilterError(`${path}.value must be a non-empty list of values for ${typedOp}`);
      }
      return { column, op: typedOp, value: operand };
    }
    if (!isLiteral(operand)) throw new RowFilterError(`${path}.value must be a string, number or boolean`);
    return { column, op: typedOp, value: operand };
  };
  return visit(input, 0, "filter");
}

function literal(value: RowFilterLiteral): string {
  return typeof value === "string" ? JSON.stringify(value) : String(value);
}

/** Compiles a parsed row filter to GraphJin filter syntax. */
export function compileRowFilter(filter: RowFilter): string {
  if ("and" in filter) return `{ and: [${filter.and.map(compileRowFilter).join(", ")}] }`;
  if ("or" in filter) return `{ or: [${filter.or.map(compileRowFilter).join(", ")}] }`;
  const { column, op, value } = filter;
  let operand: string;
  if (typeof value === "object" && value !== null && !Array.isArray(value)) operand = `$${value.var}`;
  else if (Array.isArray(value)) operand = `[${value.map(literal).join(", ")}]`;
  else operand = literal(value);
  return `{ ${column}: { ${op}: ${operand} } }`;
}
