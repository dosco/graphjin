import { describe, expect, it } from "vitest";
import {
  extractActionRequestFences,
  extractRuleSaveFence,
  extractVitalsFence,
  extractWorkflowOutputFences,
  extractWorkflowSaveFence,
} from "../src/workflows/fence-parsers";
import { WORKFLOW_SAVE_SCHEMA } from "../src/workflows/fence-schemas";

describe("extractWorkflowSaveFence", () => {
  it("documents executable GraphJin watch queries and dot-separated value paths", () => {
    const watch = WORKFLOW_SAVE_SCHEMA.shape.triggers
      .unwrap()
      .shape.watch.unwrap();
    expect(watch.shape.query.description).toContain("executed successfully");
    expect(watch.shape.value_path.description).toContain("numeric array");
    expect(watch.shape.value_path.description).toContain(
      "bracket notation is not supported",
    );
  });

  it("returns null payload when no fence is present", () => {
    const r = extractWorkflowSaveFence("just chatting with the operator");
    expect(r.payload).toBeNull();
    expect(r.errors).toEqual([]);
    expect(r.text).toBe("just chatting with the operator");
  });

  it("parses a valid save fence and strips it from text", () => {
    const raw = [
      "Saved 'APAC revenue dip check'. You can run it from the workflows list.",
      "",
      "```neko_workflow_save",
      JSON.stringify({
        name: "APAC revenue dip check",
        description: "Daily APAC revenue check.",
        goal: "Surface revenue dips.",
        systemPromptOverlay: "Show INR in lakhs.",
        steps: [
          { id: "pull", description: "Pull last 7 days of APAC revenue" },
          { id: "compare", description: "Compare with prior 7 days" },
        ],
        triggers: { cron: "0 9 * * *", timezone: "Asia/Kolkata", enabled: true },
      }),
      "```",
    ].join("\n");
    const r = extractWorkflowSaveFence(raw);
    expect(r.payload).not.toBeNull();
    expect(r.payload?.name).toBe("APAC revenue dip check");
    expect(r.payload?.steps).toHaveLength(2);
    expect(r.payload?.triggers?.cron).toBe("0 9 * * *");
    expect(r.text).toContain("Saved 'APAC revenue dip check'");
    expect(r.text).not.toContain("neko_workflow_save");
    expect(r.text).not.toContain("cron");
  });

  it("parses a triggers.when data-change trigger", () => {
    const raw = [
      "Saved 'low stock alert'.",
      "```neko_workflow_save",
      JSON.stringify({
        name: "low stock alert",
        steps: [{ id: "dm", description: "DM Amit on Slack with details" }],
        triggers: {
          when: {
            table: "productinventory",
            where: { quantity: { lt: { col: "product.reorderpoint" } } },
            primary_key: ["productid", "locationid"],
            version_column: "modifieddate",
          },
        },
      }),
      "```",
    ].join("\n");
    const r = extractWorkflowSaveFence(raw);
    expect(r.payload).not.toBeNull();
    expect(r.payload?.triggers?.when?.table).toBe("productinventory");
    expect(r.payload?.triggers?.when?.primary_key).toEqual([
      "productid",
      "locationid",
    ]);
    expect(r.text).not.toContain("neko_workflow_save");
  });

  it("rejects a triggers.when missing primary_key", () => {
    const raw = [
      "```neko_workflow_save",
      JSON.stringify({
        name: "bad trigger",
        steps: [{ id: "s", description: "do" }],
        triggers: { when: { table: "productinventory" } },
      }),
      "```",
    ].join("\n");
    const r = extractWorkflowSaveFence(raw);
    expect(r.payload).toBeNull();
    expect(r.errors).toHaveLength(1);
  });

  it("reports parse errors for invalid JSON without throwing", () => {
    const raw =
      "Prose.\n```neko_workflow_save\n{not valid json}\n```\nMore prose.";
    const r = extractWorkflowSaveFence(raw);
    expect(r.payload).toBeNull();
    expect(r.errors).toHaveLength(1);
    expect(r.text).toContain("Prose.");
    expect(r.text).toContain("More prose.");
  });

  it("rejects payloads missing required fields", () => {
    const raw = [
      "```neko_workflow_save",
      JSON.stringify({ name: "missing steps" }),
      "```",
    ].join("\n");
    const r = extractWorkflowSaveFence(raw);
    expect(r.payload).toBeNull();
    expect(r.errors).toHaveLength(1);
  });

  it("only consumes the first save fence when an agent emits two", () => {
    const valid = JSON.stringify({
      name: "wf one",
      steps: [{ id: "s", description: "step" }],
    });
    const raw = [
      "```neko_workflow_save",
      valid,
      "```",
      "```neko_workflow_save",
      valid.replace("wf one", "wf two"),
      "```",
    ].join("\n");
    const r = extractWorkflowSaveFence(raw);
    expect(r.payload?.name).toBe("wf one");
    expect(r.text).not.toContain("neko_workflow_save");
  });
});

describe("extractWorkflowOutputFences", () => {
  it("returns empty payloads when no fence is present", () => {
    const r = extractWorkflowOutputFences("nothing to emit");
    expect(r.payloads).toEqual([]);
    expect(r.errors).toEqual([]);
  });

  it("parses multiple output fences in one turn", () => {
    const first = JSON.stringify({
      kind: "observation",
      title: "APAC dipped",
      scope: "apac_revenue",
      mood: "watch",
    });
    const second = JSON.stringify({
      kind: "recommendation",
      title: "Ping growth lead",
      scope: "apac_revenue",
      mood: "act",
    });
    const raw = [
      "Done with the check.",
      "```neko_workflow_output",
      first,
      "```",
      "And a recommendation:",
      "```neko_workflow_output",
      second,
      "```",
    ].join("\n");
    const r = extractWorkflowOutputFences(raw);
    expect(r.payloads).toHaveLength(2);
    expect(r.payloads[0].kind).toBe("observation");
    expect(r.payloads[1].kind).toBe("recommendation");
    expect(r.text).toContain("Done with the check.");
    expect(r.text).toContain("And a recommendation:");
    expect(r.text).not.toContain("```neko_workflow_output");
  });

  it("rejects payloads with disallowed kinds", () => {
    const raw = [
      "```neko_workflow_output",
      JSON.stringify({ kind: "ridiculous", title: "n/a" }),
      "```",
    ].join("\n");
    const r = extractWorkflowOutputFences(raw);
    expect(r.payloads).toEqual([]);
    expect(r.errors).toHaveLength(1);
  });

  it("keeps valid fences when one in the batch is invalid", () => {
    const valid = JSON.stringify({
      kind: "finding",
      title: "ok",
      scope: "x",
      mood: "watch",
    });
    const raw = [
      "```neko_workflow_output",
      valid,
      "```",
      "```neko_workflow_output",
      "{ not json",
      "```",
    ].join("\n");
    const r = extractWorkflowOutputFences(raw);
    expect(r.payloads).toHaveLength(1);
    expect(r.errors).toHaveLength(1);
    expect(r.payloads[0].kind).toBe("finding");
  });
});

describe("extractActionRequestFences", () => {
  it("returns empty payloads when no fence is present", () => {
    const r = extractActionRequestFences("conversation prose only");
    expect(r.payloads).toEqual([]);
  });

  it("parses a valid external action request", () => {
    const payload = {
      scope: "external" as const,
      kind: "send_message",
      target: "slack:#growth",
      payload: { text: "APAC dipped 14% WoW" },
      risk_level: "low" as const,
      summary: "Post APAC dip alert to #growth so the GTM lead sees it.",
    };
    const raw = [
      "I propose this action:",
      "```neko_action_request",
      JSON.stringify(payload),
      "```",
    ].join("\n");
    const r = extractActionRequestFences(raw);
    expect(r.payloads).toHaveLength(1);
    expect(r.payloads[0].scope).toBe("external");
    expect(r.payloads[0].kind).toBe("send_message");
    expect(r.payloads[0].risk_level).toBe("low");
    expect(r.text).toContain("I propose this action");
    expect(r.text).not.toContain("neko_action_request");
  });

  it("rejects a request with an unknown scope", () => {
    const raw = [
      "```neko_action_request",
      JSON.stringify({
        scope: "wildcard",
        kind: "send_message",
        summary: "x",
      }),
      "```",
    ].join("\n");
    const r = extractActionRequestFences(raw);
    expect(r.payloads).toEqual([]);
    expect(r.errors).toHaveLength(1);
  });

  it("rejects a request missing the required summary", () => {
    const raw = [
      "```neko_action_request",
      JSON.stringify({ scope: "external", kind: "send_message" }),
      "```",
    ].join("\n");
    const r = extractActionRequestFences(raw);
    expect(r.payloads).toEqual([]);
    expect(r.errors).toHaveLength(1);
  });
});

describe("extractRuleSaveFence", () => {
  it("returns null payload when no fence is present", () => {
    const r = extractRuleSaveFence("just discussing policies with the operator");
    expect(r.payload).toBeNull();
    expect(r.errors).toEqual([]);
    expect(r.text).toBe("just discussing policies with the operator");
  });

  it("parses a valid policy fence and strips it from text", () => {
    const raw = [
      "Saved policy 'slack_low_risk_auto_approve'.",
      "",
      "```neko_rule_save",
      JSON.stringify({
        name: "slack_low_risk_auto_approve",
        description: "Auto-approve low-risk Slack alerts, capped at 20/day.",
        applies_to_kinds: ["send_message"],
        applies_to_scopes: ["external"],
        mode: "auto_approve",
        risk_threshold_auto_approve: "low",
        limits: { daily_cap: 20 },
      }),
      "```",
    ].join("\n");
    const r = extractRuleSaveFence(raw);
    expect(r.payload).not.toBeNull();
    expect(r.payload?.name).toBe("slack_low_risk_auto_approve");
    expect(r.payload?.mode).toBe("auto_approve");
    expect(r.payload?.applies_to_kinds).toEqual(["send_message"]);
    expect(r.payload?.limits).toEqual({ daily_cap: 20 });
    // Defaults from the schema are applied.
    expect(r.payload?.priority).toBe(100);
    expect(r.payload?.enabled).toBe(true);
    expect(r.text).toContain("Saved policy");
    expect(r.text).not.toContain("neko_rule_save");
  });

  it("defaults applies_to_scopes to ['external'] when omitted", () => {
    const raw = [
      "```neko_rule_save",
      JSON.stringify({
        name: "external_default",
        applies_to_kinds: [],
        mode: "approval_required",
      }),
      "```",
    ].join("\n");
    const r = extractRuleSaveFence(raw);
    expect(r.payload?.applies_to_scopes).toEqual(["external"]);
  });

  it("rejects payloads with an unknown mode", () => {
    const raw = [
      "```neko_rule_save",
      JSON.stringify({
        name: "weird",
        applies_to_kinds: ["send_message"],
        mode: "yolo",
      }),
      "```",
    ].join("\n");
    const r = extractRuleSaveFence(raw);
    expect(r.payload).toBeNull();
    expect(r.errors).toHaveLength(1);
  });

  it("rejects payloads missing required fields", () => {
    const raw = [
      "```neko_rule_save",
      JSON.stringify({ description: "no name or mode" }),
      "```",
    ].join("\n");
    const r = extractRuleSaveFence(raw);
    expect(r.payload).toBeNull();
    expect(r.errors).toHaveLength(1);
  });

  it("only consumes the first save fence when an agent emits two", () => {
    const valid = JSON.stringify({
      name: "first",
      applies_to_kinds: ["send_message"],
      mode: "auto_approve",
    });
    const raw = [
      "```neko_rule_save",
      valid,
      "```",
      "```neko_rule_save",
      valid.replace("first", "second"),
      "```",
    ].join("\n");
    const r = extractRuleSaveFence(raw);
    expect(r.payload?.name).toBe("first");
    expect(r.text).not.toContain("neko_rule_save");
  });
});

describe("extractVitalsFence", () => {
  it("returns null payload when no fence is present", () => {
    const r = extractVitalsFence("just the prose answer, no numbers fenced");
    expect(r.payload).toBeNull();
    expect(r.errors).toEqual([]);
    expect(r.text).toBe("just the prose answer, no numbers fenced");
  });

  it("parses headline vitals and strips the fence from text", () => {
    const raw = [
      "Your top 10 customers are 48% of YTD revenue.",
      "```neko_vitals",
      JSON.stringify({
        vitals: [
          { label: "Top-10 share", value: "48%", sub: "down from 53%" },
          { label: "#1 account", value: "$1.2M", sub: "Acme · 9.4%" },
          { label: "YoY revenue", value: "+14%" },
        ],
      }),
      "```",
    ].join("\n");
    const r = extractVitalsFence(raw);
    expect(r.payload?.vitals).toHaveLength(3);
    expect(r.payload?.vitals[0]).toEqual({
      label: "Top-10 share",
      value: "48%",
      sub: "down from 53%",
    });
    expect(r.payload?.vitals[2].sub).toBeUndefined();
    expect(r.text).toContain("Your top 10 customers");
    expect(r.text).not.toContain("neko_vitals");
  });

  it("rejects more than four vitals without throwing", () => {
    const raw = [
      "```neko_vitals",
      JSON.stringify({
        vitals: Array.from({ length: 5 }, (_, i) => ({
          label: `m${i}`,
          value: `${i}`,
        })),
      }),
      "```",
    ].join("\n");
    const r = extractVitalsFence(raw);
    expect(r.payload).toBeNull();
    expect(r.errors).toHaveLength(1);
  });

  it("reports parse errors for invalid JSON without throwing", () => {
    const raw = "Prose.\n```neko_vitals\n{not valid}\n```\nMore prose.";
    const r = extractVitalsFence(raw);
    expect(r.payload).toBeNull();
    expect(r.errors).toHaveLength(1);
    expect(r.text).toContain("Prose.");
    expect(r.text).toContain("More prose.");
  });

  it("lets the last valid fence win when an agent emits two", () => {
    const raw = [
      "```neko_vitals",
      JSON.stringify({ vitals: [{ label: "first", value: "1" }] }),
      "```",
      "```neko_vitals",
      JSON.stringify({ vitals: [{ label: "second", value: "2" }] }),
      "```",
    ].join("\n");
    const r = extractVitalsFence(raw);
    expect(r.payload?.vitals[0].label).toBe("second");
    expect(r.text).not.toContain("neko_vitals");
  });
});
