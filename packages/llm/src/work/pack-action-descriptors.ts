import { and, db, eq, pack_action_definition } from "@neko/db";
import type { PackActionDescriptor } from "./tools";

type PackActionDefinition = {
  kind?: unknown;
  description?: unknown;
  inputSchema?: unknown;
  example?: unknown;
  adapter?: {
    operations?: Record<
      string,
      {
        readPath?: unknown;
        bodyKey?: unknown;
        reversible?: unknown;
      }
    >;
  };
};

function operationContract(definition: PackActionDefinition): string {
  const operations = definition.adapter?.operations;
  if (!operations || typeof operations !== "object") return "";
  const entries = Object.entries(operations).map(([name, operation]) => {
    const path =
      typeof operation.readPath === "string"
        ? [...operation.readPath.matchAll(/\{([^}]+)\}/g)].map((match) => match[1])
        : [];
    const details = [
      path.length > 0 ? `row.path keys: ${path.join(", ")}` : null,
      typeof operation.bodyKey === "string"
        ? `row.body key: ${operation.bodyKey}`
        : null,
      typeof operation.reversible === "boolean"
        ? `reversible: ${operation.reversible ? "yes" : "no"}`
        : null,
    ].filter(Boolean);
    return `${name}${details.length > 0 ? ` (${details.join("; ")})` : ""}`;
  });
  if (entries.length === 0) return "";
  return ` Each row uses {\"entity_ref\":\"...\",\"path\":{...},\"body\":{...}}. Supported operations: ${entries.join("; ")}.`;
}

/**
 * Discover ready pack-owned actions directly from installed pack artifacts.
 * Pack actions stay separate from the worker's built-in actions throughout discovery.
 */
export async function listPackActionDescriptors(
  orgId: string,
): Promise<PackActionDescriptor[]> {
  const rows = await db()
    .select({ definition: pack_action_definition.definition })
    .from(pack_action_definition)
    .where(
      and(
        eq(pack_action_definition.org_id, orgId),
        eq(pack_action_definition.enabled, true),
        eq(pack_action_definition.readiness, "ready"),
      ),
    );

  return rows.flatMap(({ definition }) => {
    const value = definition as PackActionDefinition;
    if (
      typeof value.kind !== "string" ||
      value.kind.length === 0 ||
      typeof value.description !== "string" ||
      value.description.length === 0
    ) {
      return [];
    }
    const schema =
      value.inputSchema && typeof value.inputSchema === "object"
        ? ` Payload schema: ${JSON.stringify(value.inputSchema)}.`
        : "";
    const operations = operationContract(value);
    const example =
      value.example &&
      typeof value.example === "object" &&
      !Array.isArray(value.example)
        ? (value.example as Record<string, unknown>)
        : undefined;
    return [
      {
        kind: value.kind,
        scope: "external" as const,
        default_mode: "ask" as const,
        description: `${value.description}${schema}${operations}`,
        ...(example ? { example } : {}),
      },
    ];
  });
}
