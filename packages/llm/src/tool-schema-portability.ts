/**
 * Model providers accept different subsets of JSON Schema for tool
 * parameters. Some strip unknown keys, some reject the tool. A tool schema
 * stays usable on every provider when it uses only explicit structure:
 * declared object properties, typed array items and string enums.
 */

const UNPORTABLE_KEYS = [
  "anyOf",
  "oneOf",
  "allOf",
  "not",
  "const",
  "$ref",
  "$defs",
  "definitions",
  "patternProperties",
  "propertyNames",
  "prefixItems",
  "dependentSchemas",
  "if",
] as const;

/** Return one issue per structural construct a provider may drop or reject. */
export function portableSchemaIssues(schema: unknown, path = "$"): string[] {
  if (!schema || typeof schema !== "object" || Array.isArray(schema)) {
    return [`${path}: schema is not an object`];
  }
  const node = schema as Record<string, unknown>;
  const issues: string[] = [];
  for (const key of UNPORTABLE_KEYS) {
    if (key in node) issues.push(`${path}: uses ${key}`);
  }
  if (Array.isArray(node.type)) issues.push(`${path}: uses a type list`);
  if (node.enum !== undefined &&
    (!Array.isArray(node.enum) || node.enum.some((value) => typeof value !== "string"))) {
    issues.push(`${path}: enum values are not all strings`);
  }
  if (node.type === undefined && !UNPORTABLE_KEYS.some((key) => key in node)) {
    issues.push(`${path}: has no type`);
  }
  if (node.type === "object") {
    const properties = node.properties;
    if (!properties || typeof properties !== "object" ||
      Object.keys(properties).length === 0) {
      if (path !== "$") issues.push(`${path}: object declares no properties`);
    } else {
      for (const [name, child] of Object.entries(properties)) {
        issues.push(...portableSchemaIssues(child, `${path}.${name}`));
      }
    }
    if (node.additionalProperties !== undefined && node.additionalProperties !== false) {
      issues.push(`${path}: allows undeclared properties`);
    }
  }
  if (node.type === "array") {
    if (node.items === undefined) issues.push(`${path}: array declares no items`);
    else issues.push(...portableSchemaIssues(node.items, `${path}[]`));
  }
  return issues;
}
