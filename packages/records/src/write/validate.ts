import type { RecordField } from "../types";

export type RecordReferenceCheck = {
  fieldApiName: string;
  value: string;
  targetObjectApiNames: string[];
};

export type ValidatedRecordFields = {
  fields: Record<string, unknown>;
  references: RecordReferenceCheck[];
};

export type RecordValidationIssue = {
  field: string;
  code: string;
  message: string;
};

export class RecordValidationError extends Error {
  readonly code = "records_validation_failed";

  constructor(readonly issues: RecordValidationIssue[]) {
    super(issues.map((issue) => `${issue.field}: ${issue.message}`).join("; "));
    this.name = "RecordValidationError";
  }
}

function issue(field: string, code: string, message: string): never {
  throw new RecordValidationError([{ field, code, message }]);
}

function validateString(value: unknown, field: RecordField): string {
  if (typeof value !== "string") issue(field.apiName, "type", "must be a string");
  if (field.length !== null && [...value].length > field.length) {
    issue(field.apiName, "length", `must contain at most ${field.length} characters`);
  }
  return value;
}

function validateDate(value: unknown, field: RecordField): string {
  const text = validateString(value, field);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(text)) {
    issue(field.apiName, "date", "must use YYYY-MM-DD");
  }
  const parsed = new Date(`${text}T00:00:00.000Z`);
  if (Number.isNaN(parsed.getTime()) || parsed.toISOString().slice(0, 10) !== text) {
    issue(field.apiName, "date", "must be a real calendar date");
  }
  return text;
}

function validateDatetime(value: unknown, field: RecordField): string {
  const text = validateString(value, field);
  const parsed = new Date(text);
  if (Number.isNaN(parsed.getTime())) {
    issue(field.apiName, "datetime", "must be an ISO-8601 timestamp");
  }
  return parsed.toISOString();
}

function decimalScale(value: string): number {
  return value.split(".")[1]?.length ?? 0;
}

function validateDecimal(value: unknown, field: RecordField): number {
  if (typeof value !== "number" && typeof value !== "string") {
    issue(field.apiName, "type", "must be a decimal number");
  }
  if (typeof value === "number" && !Number.isFinite(value)) {
    issue(field.apiName, "number", "must be finite");
  }
  const text = String(value);
  if (!/^-?\d+(?:\.\d+)?$/.test(text)) {
    issue(field.apiName, "number", "must use plain decimal notation");
  }
  if (field.scale !== null && decimalScale(text) > field.scale) {
    issue(field.apiName, "scale", `must use at most ${field.scale} decimal places`);
  }
  const digits = text.replace(/[-.]/g, "").replace(/^0+/, "").length || 1;
  if (field.length !== null && digits > field.length) {
    issue(field.apiName, "precision", `must use at most ${field.length} digits`);
  }
  // Documents keep decimals as JSON numbers so they compare numerically.
  const number = Number(text);
  if (!Number.isFinite(number) || Math.abs(number) > Number.MAX_SAFE_INTEGER) {
    issue(field.apiName, "precision", "is too large to store exactly");
  }
  return number;
}

function picklistCandidateMatches(candidate: unknown, value: unknown): boolean {
  if (Object.is(candidate, value)) return true;
  if (candidate && typeof candidate === "object" && !Array.isArray(candidate)) {
    return Object.is((candidate as Record<string, unknown>).value, value);
  }
  return false;
}

function validatePicklist(value: unknown, field: RecordField): unknown {
  const allowed = field.picklistValues ?? [];
  if (!allowed.some((candidate) => picklistCandidateMatches(candidate, value))) {
    issue(field.apiName, "picklist", "must be one of the configured values");
  }
  return value;
}

function validateMultipicklist(value: unknown, field: RecordField): unknown[] {
  if (!Array.isArray(value)) issue(field.apiName, "type", "must be an array");
  if (value.length > 100) issue(field.apiName, "cardinality", "cannot contain more than 100 values");
  const allowed = field.picklistValues ?? [];
  const seen = new Set<string>();
  for (const entry of value) {
    if (!allowed.some((candidate) => picklistCandidateMatches(candidate, entry))) {
      issue(field.apiName, "picklist", "contains a value outside the configured set");
    }
    const key = JSON.stringify(entry);
    if (seen.has(key)) issue(field.apiName, "duplicate", "cannot contain duplicate values");
    seen.add(key);
  }
  return value;
}

function validateFieldValue(
  value: unknown,
  field: RecordField,
): { value: unknown; reference?: RecordReferenceCheck } {
  if (value === null) {
    if (field.required) issue(field.apiName, "required", "cannot be null");
    return { value: null };
  }
  switch (field.kind) {
    case "id":
    case "text":
    case "textarea":
    case "phone":
    case "readonly_formula":
      return { value: validateString(value, field) };
    case "email": {
      const email = validateString(value, field);
      if (email.length > 320 || !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) {
        issue(field.apiName, "email", "must be a valid email address");
      }
      return { value: email };
    }
    case "url": {
      const text = validateString(value, field);
      let url: URL;
      try {
        url = new URL(text);
      } catch {
        issue(field.apiName, "url", "must be an absolute URL");
      }
      if (url.protocol !== "http:" && url.protocol !== "https:") {
        issue(field.apiName, "url", "must use HTTP or HTTPS");
      }
      return { value: text };
    }
    case "boolean":
      if (typeof value !== "boolean") issue(field.apiName, "type", "must be a boolean");
      return { value };
    case "integer":
      if (typeof value !== "number" || !Number.isSafeInteger(value)) {
        issue(field.apiName, "type", "must be a safe integer");
      }
      return { value };
    case "decimal":
    case "currency":
    case "percent":
      return { value: validateDecimal(value, field) };
    case "date":
      return { value: validateDate(value, field) };
    case "datetime":
      return { value: validateDatetime(value, field) };
    case "picklist":
      return { value: validatePicklist(value, field) };
    case "multipicklist":
      return { value: validateMultipicklist(value, field) };
    case "reference": {
      const referenceValue = validateString(value, field);
      if (!referenceValue) issue(field.apiName, "reference", "cannot be empty");
      const targets = field.referenceTargets ?? [];
      if (targets.length === 0) {
        issue(field.apiName, "reference", "has no configured target object");
      }
      return {
        value: referenceValue,
        reference: {
          fieldApiName: field.apiName,
          value: referenceValue,
          targetObjectApiNames: targets,
        },
      };
    }
  }
}

/** Validate only registry-declared business fields; system stamps never enter here. */
export function validateRecordFields(input: {
  values: Record<string, unknown>;
  registryFields: RecordField[];
  operation: "create" | "update" | "expected";
  /** Connector imports may materialize source formula/rollup values. */
  allowReadOnly?: boolean;
}): ValidatedRecordFields {
  if (!input.values || Array.isArray(input.values)) {
    throw new RecordValidationError([
      { field: "$", code: "type", message: "fields must be an object" },
    ]);
  }
  const liveFields = input.registryFields.filter((field) => field.archivedAt === null);
  const fieldsByApiName = new Map(liveFields.map((field) => [field.apiName, field]));
  const output: Record<string, unknown> = {};
  const references: RecordReferenceCheck[] = [];
  const issues: RecordValidationIssue[] = [];

  for (const [apiName, value] of Object.entries(input.values)) {
    if (apiName === "id" && input.operation === "create") {
      if (typeof value !== "string" || !value.trim() || value.length > 512) {
        issues.push({ field: "id", code: "id", message: "must be a non-empty string" });
      } else {
        output.id = value;
      }
      continue;
    }
    const field = fieldsByApiName.get(apiName as RecordField["apiName"]);
    if (!field) {
      issues.push({ field: apiName, code: "unknown", message: "is unknown or archived" });
      continue;
    }
    if (field.kind === "id") {
      issues.push({ field: apiName, code: "id", message: "must use the top-level id field" });
      continue;
    }
    if (field.readOnly && input.allowReadOnly !== true) {
      issues.push({ field: apiName, code: "read_only", message: "is read-only" });
      continue;
    }
    try {
      const validated = validateFieldValue(value, field);
      output[field.columnName] = validated.value;
      if (validated.reference) references.push(validated.reference);
    } catch (error) {
      if (error instanceof RecordValidationError) issues.push(...error.issues);
      else throw error;
    }
  }

  if (input.operation === "create") {
    for (const field of liveFields) {
      if (
        field.required &&
        !field.readOnly &&
        field.kind !== "id" &&
        !Object.prototype.hasOwnProperty.call(input.values, field.apiName)
      ) {
        issues.push({ field: field.apiName, code: "required", message: "is required" });
      }
    }
  }
  if (input.operation !== "create" && Object.keys(input.values).length === 0) {
    issues.push({ field: "$", code: "empty", message: "must include at least one field" });
  }
  if (issues.length > 0) throw new RecordValidationError(issues);
  return { fields: output, references };
}
