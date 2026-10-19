import {
  createActionPolicy,
  listEnabledPolicies as defaultListEnabledPolicies,
  type ActionPolicyMode,
  type ActionPolicyRecord,
  type ActionScope,
  type RiskLevel,
} from "./action-store";

export type PolicyRequestSubject = {
  scope: ActionScope;
  kind: string;
  target?: string | null;
  /** Targets read from the payload. When set, every one must pass allowed_targets. */
  targets?: readonly string[];
  riskLevel?: RiskLevel | null;
};

export type PolicyDecision =
  | {
      decision: "allow";
      policy: ActionPolicyRecord;
      mode: "auto_approve";
      reason?: string;
    }
  | {
      decision: "needs_approval";
      policy: ActionPolicyRecord;
      mode: "approval_required" | "draft_only";
      reason?: string;
    }
  | {
      decision: "deny";
      policy: ActionPolicyRecord;
      mode: "observe_only" | "never";
      reason: string;
    }
  | {
      decision: "no_policy";
      reason: "no_matching_policy";
    };

const RISK_ORDER: Record<RiskLevel, number> = {
  low: 0,
  medium: 1,
  high: 2,
  critical: 3,
};

function riskExceedsThreshold(
  riskLevel: RiskLevel | null | undefined,
  threshold: RiskLevel | null,
): boolean {
  if (threshold === null || threshold === undefined) return false;
  if (!riskLevel) return false;
  return RISK_ORDER[riskLevel] > RISK_ORDER[threshold];
}

function targetPatterns(patterns: Record<string, unknown> | null): string[] | null {
  if (!patterns || !Array.isArray(patterns.patterns)) return null;
  return patterns.patterns.filter((p): p is string => typeof p === "string");
}

function patternMatches(target: string, pattern: string): boolean {
  if (pattern.startsWith("*.")) return target.endsWith(pattern.slice(1));
  if (pattern.endsWith("*")) return target.startsWith(pattern.slice(0, -1));
  return target === pattern;
}

function requestTargets(request: PolicyRequestSubject): readonly string[] {
  if (request.targets) return request.targets;
  return request.target ? [request.target] : [];
}

function anyTargetMatches(targets: readonly string[], patterns: Record<string, unknown> | null): boolean {
  const list = targetPatterns(patterns);
  return !!list && targets.some((t) => list.some((p) => patternMatches(t, p)));
}

function allTargetsMatch(targets: readonly string[], list: string[]): boolean {
  return targets.length > 0 && targets.every((t) => list.some((p) => patternMatches(t, p)));
}

function policyApplies(
  policy: ActionPolicyRecord,
  request: PolicyRequestSubject,
): boolean {
  if (
    policy.appliesToScopes.length > 0 &&
    !policy.appliesToScopes.includes(request.scope)
  ) {
    return false;
  }
  if (
    policy.appliesToKinds.length > 0 &&
    !policy.appliesToKinds.includes(request.kind)
  ) {
    return false;
  }
  return true;
}

/**
 * Evaluate a proposed action against the org's enabled policies. Returns
 * the first matching policy by priority (lower number = higher priority).
 *
 * Deterministic / pure given inputs; safe to unit-test without DB.
 */
export function evaluateActionPolicy(
  request: PolicyRequestSubject,
  policies: ActionPolicyRecord[],
): PolicyDecision {
  const ordered = [...policies].sort((a, b) => a.priority - b.priority);
  for (const policy of ordered) {
    if (!policyApplies(policy, request)) continue;
    const targets = requestTargets(request);
    const shown = targets.join(", ");
    if (anyTargetMatches(targets, policy.deniedTargets)) {
      return {
        decision: "deny",
        policy,
        mode: policy.mode === "never" ? "never" : "observe_only",
        reason: `target "${shown}" is in policy "${policy.name}" denied_targets`,
      };
    }
    const allowed = targetPatterns(policy.allowedTargets);
    if (allowed && !allTargetsMatch(targets, allowed)) {
      if (policy.allowedTargets?.on_miss === "next") continue;
      return {
        decision: "deny",
        policy,
        mode: "observe_only",
        reason: `target "${shown}" is not in policy "${policy.name}" allowed_targets`,
      };
    }
    return decisionFromMode(policy, request);
  }
  return { decision: "no_policy", reason: "no_matching_policy" };
}

function decisionFromMode(
  policy: ActionPolicyRecord,
  request: PolicyRequestSubject,
): PolicyDecision {
  switch (policy.mode) {
    case "never":
      return {
        decision: "deny",
        policy,
        mode: "never",
        reason: `policy "${policy.name}" mode=never`,
      };
    case "observe_only":
      return {
        decision: "deny",
        policy,
        mode: "observe_only",
        reason: `policy "${policy.name}" mode=observe_only`,
      };
    case "draft_only":
      return {
        decision: "needs_approval",
        policy,
        mode: "draft_only",
        reason: `policy "${policy.name}" mode=draft_only — operator approves before execute`,
      };
    case "approval_required":
      return {
        decision: "needs_approval",
        policy,
        mode: "approval_required",
        reason: `policy "${policy.name}" mode=approval_required`,
      };
    case "auto_approve":
      if (riskExceedsThreshold(request.riskLevel, policy.riskThresholdAutoApprove)) {
        return {
          decision: "needs_approval",
          policy,
          mode: "approval_required",
          reason: `risk=${request.riskLevel} exceeds policy "${policy.name}" auto-approve threshold (${policy.riskThresholdAutoApprove})`,
        };
      }
      return {
        decision: "allow",
        policy,
        mode: "auto_approve",
      };
    default:
      return {
        decision: "deny",
        policy,
        mode: "never",
        reason: `policy "${policy.name}" has unknown mode "${policy.mode}"`,
      };
  }
}

/**
 * Idempotent default-policy seeder. Seeds the baseline external_default
 * policy at priority 950 so org-specific policies (priority 100..900)
 * override it naturally.
 *
 * Internal product operations (memory writes, briefing creation,
 * workflow scheduling) are not policy-gated by design — they're product
 * features. The action stack and its policies only govern external
 * state changes (outbound webhooks, CRM updates, code commits, etc.).
 */
export async function seedDefaultActionPolicies(orgId: string): Promise<void> {
  const existing = await defaultListEnabledPolicies(orgId);
  const names = new Set(existing.map((p) => p.name));
  if (!names.has("external_default")) {
    await createActionPolicy({
      orgId,
      name: "external_default",
      description:
        "Operator must approve any external state-changing operation before it executes.",
      appliesToKinds: [],
      appliesToScopes: ["external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: null,
      priority: 950,
      enabled: true,
    });
  }
  // ADM1: user management from chat needs an ADMIN approval (K2 enforces).
  if (!names.has("user_management_default")) {
    await createActionPolicy({
      orgId,
      name: "user_management_default",
      description:
        "Inviting, role changes, and deactivation proposed from chat require an admin's approval.",
      appliesToKinds: ["user_admin"],
      appliesToScopes: ["internal", "external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: "admin",
      priority: 90,
      enabled: true,
    });
  }
  // Group, item grant and data access changes proposed from chat need an admin.
  if (!names.has("group_management_default")) {
    await createActionPolicy({
      orgId,
      name: "group_management_default",
      description:
        "Group, membership, item grant and data access changes proposed from chat require an admin's approval.",
      appliesToKinds: ["group_admin"],
      appliesToScopes: ["internal", "external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: "admin",
      priority: 90,
      enabled: true,
    });
  }
  // OL6: code actions require explicit approval unless an org policy
  // says otherwise — never autonomous.
  if (!names.has("code_action_default")) {
    await createActionPolicy({
      orgId,
      name: "code_action_default",
      description:
        "Filing issues and drafting patches always require operator approval; OpenNeko never applies code changes itself.",
      appliesToKinds: ["code_create_issue", "code_draft_patch"],
      appliesToScopes: ["internal", "external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: null,
      priority: 94,
      enabled: true,
    });
  }
  // C4: generated-app writes are internal state changes, but still require an
  // approval by default. Operators may add narrower auto-approve policies for
  // low-risk classes such as activity logging.
  if (!names.has("record_mutation_default")) {
    await createActionPolicy({
      orgId,
      name: "record_mutation_default",
      description:
        "Creating, updating, deleting, or restoring generated-app records requires operator approval by default.",
      appliesToKinds: [
        "record_create",
        "record_update",
        "record_delete",
        "record_restore",
      ],
      appliesToScopes: ["internal", "external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: null,
      priority: 92,
      enabled: true,
    });
  }
  // C3: app definitions and their additive physical schema are operator-owned
  // changes. Every schema kind requires an admin approval by default.
  if (!names.has("record_schema_default")) {
    await createActionPolicy({
      orgId,
      name: "record_schema_default",
      description:
        "Creating or changing a generated app schema, permissions, or layouts requires admin approval.",
      appliesToKinds: [
        "app_create",
        "app_object_create",
        "app_field_add",
        "app_field_modify",
        "app_object_archive",
        "app_field_archive",
        "app_permission_set",
        "app_layout_update",
      ],
      appliesToScopes: ["internal", "external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: "admin",
      priority: 91,
      enabled: true,
    });
  }
  if (!names.has("record_access_default")) {
    await createActionPolicy({
      orgId,
      name: "record_access_default",
      description:
        "Granting, changing, or revoking generated-app user/group access requires admin approval.",
      appliesToKinds: [
        "app_access_grant",
        "app_access_revoke",
        "app_object_permission_set",
        "app_field_permission_set",
      ],
      appliesToScopes: ["internal", "external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: "admin",
      priority: 93,
      enabled: true,
    });
  }
  // D7: additive field backfills can update many rows and are one step in a
  // reviewed schema-evolution sequence. Keep them admin-approved even though
  // the physical mutations use the ordinary audited records data plane.
  if (!names.has("record_backfill_default")) {
    await createActionPolicy({
      orgId,
      name: "record_backfill_default",
      description:
        "Populating a newly added generated-app field requires admin approval.",
      appliesToKinds: ["record_backfill"],
      appliesToScopes: ["internal", "external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: "admin",
      priority: 90,
      enabled: true,
    });
  }
  // C8: a reviewed mapping may create thousands of app rows, so starting or
  // cancelling an import stays admin-approved. Status is a read-only control
  // operation and can execute without manufacturing another approval card.
  if (!names.has("records_import_mutation_default")) {
    await createActionPolicy({
      orgId,
      name: "records_import_mutation_default",
      description:
        "Starting or cancelling a generated-app CSV import requires admin approval.",
      appliesToKinds: ["records_import_start", "records_import_cancel"],
      appliesToScopes: ["internal", "external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: "admin",
      priority: 90,
      enabled: true,
    });
  }
  if (!names.has("records_import_status_default")) {
    await createActionPolicy({
      orgId,
      name: "records_import_status_default",
      description: "Reading records import progress is safe to auto-approve.",
      appliesToKinds: ["records_import_status"],
      appliesToScopes: ["internal", "external"],
      mode: "auto_approve" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: null,
      priority: 90,
      enabled: true,
    });
  }
  if (!names.has("records_identity_backfill_default")) {
    await createActionPolicy({
      orgId,
      name: "records_identity_backfill_default",
      description:
        "Propagating linked source identities into generated-app ownership requires admin approval.",
      appliesToKinds: ["records_identity_backfill"],
      appliesToScopes: ["internal", "external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: "admin",
      priority: 90,
      enabled: true,
    });
  }
  if (!names.has("records_identity_backfill_lazy_default")) {
    await createActionPolicy({
      orgId,
      name: "records_identity_backfill_lazy_default",
      description:
        "A trusted post-SSO identity link may propagate that exact source user's ownership automatically.",
      appliesToKinds: ["records_identity_backfill_lazy"],
      appliesToScopes: ["internal"],
      mode: "auto_approve" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: null,
      priority: 89,
      enabled: true,
    });
  }
  if (!names.has("records_identity_backfill_import_default")) {
    await createActionPolicy({
      orgId,
      name: "records_identity_backfill_import_default",
      description:
        "A verified connector import may propagate only its unambiguous identity links automatically.",
      appliesToKinds: ["records_identity_backfill_import"],
      appliesToScopes: ["internal"],
      mode: "auto_approve" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: null,
      priority: 89,
      enabled: true,
    });
  }
  if (!names.has("records_salesforce_discovery_default")) {
    await createActionPolicy({
      orgId,
      name: "records_salesforce_discovery_default",
      description:
        "Salesforce metadata discovery is a read-only migration dry run and may execute automatically.",
      appliesToKinds: ["records_salesforce_discover"],
      appliesToScopes: ["internal", "external"],
      mode: "auto_approve" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: null,
      priority: 89,
      enabled: true,
    });
  }
  if (!names.has("records_salesforce_control_default")) {
    await createActionPolicy({
      orgId,
      name: "records_salesforce_control_default",
      description:
        "Starting or cancelling a Salesforce export requires admin approval.",
      appliesToKinds: [
        "records_salesforce_export_start",
        "records_salesforce_export_cancel",
      ],
      appliesToScopes: ["internal", "external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: "admin",
      priority: 90,
      enabled: true,
    });
  }
  if (!names.has("records_salesforce_sync_default")) {
    await createActionPolicy({
      orgId,
      name: "records_salesforce_sync_default",
      description:
        "Enabling a recurring Salesforce delta sync requires admin approval.",
      appliesToKinds: ["records_salesforce_sync_delta"],
      appliesToScopes: ["internal", "external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: "admin",
      priority: 90,
      enabled: true,
    });
  }
  if (!names.has("records_salesforce_cutover_default")) {
    await createActionPolicy({
      orgId,
      name: "records_salesforce_cutover_default",
      description:
        "Making an imported Salesforce mirror locally writable requires admin approval.",
      appliesToKinds: ["records_salesforce_cutover"],
      appliesToScopes: ["internal", "external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: "admin",
      priority: 90,
      enabled: true,
    });
  }
  if (!names.has("records_salesforce_status_default")) {
    await createActionPolicy({
      orgId,
      name: "records_salesforce_status_default",
      description: "Reading Salesforce export progress is safe to auto-approve.",
      appliesToKinds: ["records_salesforce_export_status"],
      appliesToScopes: ["internal", "external"],
      mode: "auto_approve" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: null,
      priority: 90,
      enabled: true,
    });
  }
  if (!names.has("records_artifact_import_default")) {
    await createActionPolicy({
      orgId,
      name: "records_artifact_import_default",
      description:
        "Creating an app and loading a reviewed connector artifact requires admin approval.",
      appliesToKinds: ["records_artifact_import_start"],
      appliesToScopes: ["internal", "external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: "admin",
      priority: 90,
      enabled: true,
    });
  }
  if (!names.has("records_artifact_import_from_export_default")) {
    await createActionPolicy({
      orgId,
      name: "records_artifact_import_from_export_default",
      description:
        "A trusted export worker may load the exact artifact covered by its approved Salesforce export.",
      appliesToKinds: ["records_artifact_import_from_export"],
      appliesToScopes: ["internal"],
      mode: "auto_approve" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: null,
      priority: 89,
      enabled: true,
    });
  }
  // OL5: configuring the customer GraphJin (sources/roles/access) from
  // chat needs an ADMIN. (The OpenNeko internal GraphJin is never a target.)
  if (!names.has("source_config_management_default")) {
    await createActionPolicy({
      orgId,
      name: "source_config_management_default",
      description:
        "Adding roles, changing source access, or registering a source on the customer data engine, proposed from chat, requires an admin's approval.",
      appliesToKinds: ["source_config_admin"],
      appliesToScopes: ["internal", "external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: "admin",
      priority: 93,
      enabled: true,
    });
  }
  // ADM2: data-source registry changes from chat need an ADMIN.
  if (!names.has("data_source_management_default")) {
    await createActionPolicy({
      orgId,
      name: "data_source_management_default",
      description:
        "Registering, enabling/disabling or removing a data source proposed from chat requires an admin's approval.",
      appliesToKinds: ["data_source_admin"],
      appliesToScopes: ["internal", "external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: "admin",
      priority: 96,
      enabled: true,
    });
  }
  // ADM5: linking/blocking channel identities from chat needs an ADMIN.
  if (!names.has("channel_management_default")) {
    await createActionPolicy({
      orgId,
      name: "channel_management_default",
      description:
        "Linking, unlinking or blocking a channel identity proposed from chat requires an admin's approval.",
      appliesToKinds: ["channel_admin"],
      appliesToScopes: ["internal", "external"],
      mode: "approval_required" as ActionPolicyMode,
      riskThresholdAutoApprove: null,
      allowedTargets: null,
      deniedTargets: null,
      limits: {},
      approverRole: "admin",
      priority: 95,
      enabled: true,
    });
  }
}
