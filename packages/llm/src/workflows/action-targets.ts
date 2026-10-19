import { action_target_spec, and, db, eq, notInArray, pack_action_definition } from "@neko/db";
import type { PolicyRequestSubject } from "./policy-engine";

/**
 * Payload fields that hold an action's targets. A field may be a dot path into
 * the payload, such as body.to. Mirrors ActionTargetSpec in
 * @open-neko/plugin-types and the targets of a pack action.
 */
export type ActionTargetSpec = {
  fields: string[];
  as?: "value" | "email_domain";
};

const BUILTIN_TARGET_SPECS: Record<string, ActionTargetSpec> = {
  send_webhook: { fields: ["url"], as: "value" },
};

function parseSpec(value: unknown): ActionTargetSpec | null {
  if (!value || typeof value !== "object") return null;
  const { fields, as } = value as { fields?: unknown; as?: unknown };
  if (!Array.isArray(fields)) return null;
  const names = fields.filter((f): f is string => typeof f === "string" && f.length > 0);
  if (names.length === 0) return null;
  return { fields: names, as: as === "email_domain" ? "email_domain" : "value" };
}

function emailDomain(address: string): string {
  const bracketed = /<([^>]*)>/.exec(address);
  const email = (bracketed ? bracketed[1] : address).trim().toLowerCase();
  const at = email.lastIndexOf("@");
  return at > 0 && at < email.length - 1 ? email.slice(at + 1).replace(/\.$/, "") : `invalid:${email}`;
}

/** The action's real targets, read from its payload. Order and duplicates are removed. */
export function deriveActionTargets(
  spec: ActionTargetSpec,
  payload: Record<string, unknown> | null | undefined,
): string[] {
  const values: string[] = [];
  for (const field of spec.fields) {
    const raw = payloadField(payload, field);
    const list = Array.isArray(raw) ? raw : raw === undefined || raw === null ? [] : [raw];
    for (const item of list) {
      const text = typeof item === "string" ? item : JSON.stringify(item);
      if (spec.as === "email_domain") {
        values.push(...text.split(",").map((part) => part.trim()).filter(Boolean).map(emailDomain));
      } else if (text.trim()) {
        values.push(text.trim());
      }
    }
  }
  return [...new Set(values)].sort();
}

function payloadField(payload: Record<string, unknown> | null | undefined, field: string): unknown {
  let current: unknown = payload;
  for (const key of field.split(".")) {
    if (!current || typeof current !== "object" || Array.isArray(current) || !Object.hasOwn(current, key)) {
      return undefined;
    }
    current = (current as Record<string, unknown>)[key];
  }
  return current;
}

export async function getActionTargetSpec(orgId: string, kind: string): Promise<ActionTargetSpec | null> {
  const builtin = BUILTIN_TARGET_SPECS[kind];
  if (builtin) return builtin;
  const [pack] = await db()
    .select({ definition: pack_action_definition.definition })
    .from(pack_action_definition)
    .where(and(eq(pack_action_definition.org_id, orgId), eq(pack_action_definition.kind, kind)))
    .limit(1);
  if (pack) return parseSpec(pack.definition.targets);
  const [row] = await db()
    .select({ spec: action_target_spec.spec })
    .from(action_target_spec)
    .where(and(eq(action_target_spec.org_id, orgId), eq(action_target_spec.kind, kind)))
    .limit(1);
  return row ? parseSpec(row.spec) : null;
}

/** Replaces the org's plugin target specs with the ones the installed plugins declare. */
export async function syncPluginActionTargetSpecs(
  orgId: string,
  specs: ReadonlyArray<{ pluginName: string; kind: string; spec: unknown }>,
): Promise<void> {
  const valid = specs.flatMap((s) => {
    const spec = parseSpec(s.spec);
    return spec && !BUILTIN_TARGET_SPECS[s.kind] ? [{ ...s, spec }] : [];
  });
  await db().transaction(async (tx) => {
    const kinds = valid.map((s) => s.kind);
    await tx
      .delete(action_target_spec)
      .where(
        kinds.length > 0
          ? and(eq(action_target_spec.org_id, orgId), notInArray(action_target_spec.kind, kinds))
          : eq(action_target_spec.org_id, orgId),
      );
    for (const s of valid) {
      await tx
        .insert(action_target_spec)
        .values({ org_id: orgId, kind: s.kind, plugin_name: s.pluginName, spec: s.spec })
        .onConflictDoUpdate({
          target: [action_target_spec.org_id, action_target_spec.kind],
          set: { plugin_name: s.pluginName, spec: s.spec, updated_at: new Date() },
        });
    }
  });
}

/**
 * Replaces the agent's target with the targets read from the payload when the
 * kind declares a target spec. Kinds without a spec keep the agent's target.
 */
export async function withActionTargets<T extends PolicyRequestSubject>(
  orgId: string,
  subject: T,
  payload: Record<string, unknown> | null | undefined,
): Promise<T & { targets?: readonly string[] }> {
  const spec = await getActionTargetSpec(orgId, subject.kind);
  if (!spec) return subject;
  const targets = deriveActionTargets(spec, payload);
  return { ...subject, target: targets.length > 0 ? targets.join(", ") : null, targets };
}
