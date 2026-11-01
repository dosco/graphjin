import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { loadSolutionPack, type SolutionPackBundle } from "@neko/packs";
import { deriveActionTargets, evaluateActionPolicy, type ActionPolicyRecord, type ActionTargetSpec } from "@neko/llm/workflows";
import { declarativeGraphjinUpdate } from "../src/packs/declarative.js";
import { lockBody } from "../src/packs/declarative-action-runtime.js";

const here = dirname(fileURLToPath(import.meta.url));
const load = () => loadSolutionPack(resolve(here, "../../../packs/resend"));

function content(bundle: SolutionPackBundle, key: string): Record<string, any> {
  const artifact = bundle.artifacts.find((value) => value.key === key);
  if (!artifact) throw new Error(`missing ${key}`);
  return artifact.content as Record<string, any>;
}

// The pack's policies as the installer stores them (PackService maps the
// authored modes), all enabled.
const MODES: Record<string, string> = { ask: "approval_required", auto: "auto_approve", draft: "draft_only" };
function policies(bundle: SolutionPackBundle, patterns: string[]): ActionPolicyRecord[] {
  return bundle.artifacts.filter((value) => value.kind === "policy").map((artifact) => {
    const value = artifact.content as Record<string, any>;
    const allowed = value.allowedTargets?.patterns ? { ...value.allowedTargets, patterns } : value.allowedTargets;
    return {
      id: artifact.key, orgId: "org", name: value.name, description: value.description,
      appliesToKinds: value.appliesToKinds, appliesToScopes: value.appliesToScopes, mode: MODES[value.mode] ?? value.mode,
      riskThresholdAutoApprove: null, allowedTargets: allowed, deniedTargets: null, limits: value.limits,
      approverGroupId: null, priority: value.priority, enabled: true,
      createdByThreadId: null, createdByRunId: null, createdAt: new Date(), updatedAt: new Date(),
    } as ActionPolicyRecord;
  });
}

describe("Resend pack", () => {
  it("reads every recipient domain from to, cc and bcc", async () => {
    const bundle = await load();
    const action = content(bundle, "action.resend_email");
    expect(action.targets).toEqual({ fields: ["body.to", "body.cc", "body.bcc"], as: "email_domain" });
    const targets = deriveActionTargets(action.targets as ActionTargetSpec, {
      operation: "send_email",
      body: {
        from: "Attacker <ceo@evil.test>",
        to: ["Ops@ACME.com", "Buyer <buyer@partner.com>"],
        cc: ["finance@acme.com"],
        bcc: ["audit@eu.acme.com."],
        subject: "Mail for intent@ignored.test",
      },
    });
    // from and the subject are not recipients.
    expect(targets).toEqual(["acme.com", "eu.acme.com", "partner.com"]);
  });

  it("sends without approval only when every recipient domain is approved", async () => {
    const bundle = await load();
    const spec = content(bundle, "action.resend_email").targets as ActionTargetSpec;
    const decide = (body: Record<string, unknown>) => {
      const targets = deriveActionTargets(spec, { operation: "send_email", body });
      return evaluateActionPolicy({ scope: "external", kind: "resend.email", target: "agent text", targets }, policies(bundle, ["acme.com", "*.acme.com"]));
    };
    expect(decide({ to: ["ops@acme.com"], cc: ["eu@eu.acme.com"] })).toMatchObject({ decision: "allow", policy: { name: "Resend approved recipient domains" } });
    expect(decide({ to: ["ops@acme.com"], bcc: ["someone@evil.test"] })).toMatchObject({ decision: "needs_approval", policy: { name: "Resend governed send" } });
    // An empty approved list sends nothing without approval.
    const targets = deriveActionTargets(spec, { operation: "send_email", body: { to: ["ops@acme.com"] } });
    expect(evaluateActionPolicy({ scope: "external", kind: "resend.email", targets }, policies(bundle, []))).toMatchObject({ decision: "needs_approval" });
  });

  it("locks the sender to the configured input", async () => {
    const bundle = await load();
    const adapter = content(bundle, "action.resend_email").adapter;
    expect(adapter.lockedBody).toEqual({ from: { input: "resend.from" } });
    expect(bundle.manifest.inputs.map((input) => input.key)).toContain("resend.from");
    expect(lockBody("resend.email", { from: "ceo@evil.test", to: ["a@acme.com"] }, adapter.lockedBody, { "resend.from": "Ops <ops@acme.com>" }))
      .toEqual({ from: "Ops <ops@acme.com>", to: ["a@acme.com"] });
    expect(() => lockBody("resend.email", { to: ["a@acme.com"] }, adapter.lockedBody, {})).toThrow("needs the pack input resend.from");
    expect(adapter.operations.send_email).toMatchObject({ operationId: "sendEmail", mutationRoot: "resend_send_email", idempotencyHeader: "Idempotency-Key" });
  });

  it("exposes only the send as a mutation, against the pinned Resend host", async () => {
    const bundle = await load();
    const update = declarativeGraphjinUpdate(bundle, { "resend.from": "Ops <ops@acme.com>" }, { "resend.api_key": "re_test" });
    const [source] = update.update_sources as Array<Record<string, any>>;
    expect(source.specs.resend.base_url).toBe("https://api.resend.com");
    expect(source.specs.resend.auth).toEqual({ scheme: "bearer", token: "re_test" });
    expect(source.specs.resend.operations).toEqual({
      getEmail: { expose_top_level: true },
      listTemplates: { expose_top_level: true },
      getTemplate: { expose_top_level: true },
      sendEmail: { expose_mutation: true, allowed_roles: ["pack_api_executor"], expose_as: "resend_send_email" },
    });
    expect(source.capabilities).toEqual({ "api.read": true, "api.write": true, "api.delete": false });
    expect(bundle.manifest.permissions.network).toEqual(["api.resend.com"]);
    const spec = content(bundle, "spec.resend");
    expect(spec.paths["/emails"].post.parameters).toEqual([expect.objectContaining({ name: "Idempotency-Key", in: "header" })]);
  });
});
