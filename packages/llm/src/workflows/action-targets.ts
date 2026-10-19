import type { PolicyRequestSubject } from "./policy-engine";

/** Which payload fields carry an action's real targets, and how to read them. */
export type ActionTargetSpec = {
  fields: string[];
  as?: "value" | "email_domain";
};

const BUILTIN_TARGET_SPECS: Record<string, ActionTargetSpec> = {
  send_webhook: { fields: ["url"], as: "value" },
};

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
    const raw = payload?.[field];
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

export async function getActionTargetSpec(_orgId: string, kind: string): Promise<ActionTargetSpec | null> {
  return BUILTIN_TARGET_SPECS[kind] ?? null;
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
