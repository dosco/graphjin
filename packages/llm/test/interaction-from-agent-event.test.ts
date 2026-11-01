import { describe, expect, it } from "vitest";
import type { AgentEvent } from "../src/agent-backend";
import { toInteractionEvents } from "../src/interaction/from-agent-event";

describe("toInteractionEvents", () => {
  it("maps assistant prose to converse and drops user echoes", () => {
    const events: AgentEvent[] = [
      { type: "message", role: "assistant", content: "Looking into it." },
      { type: "message", role: "user", content: "thanks" },
    ];
    expect(toInteractionEvents(events)).toEqual([
      { kind: "converse", id: "ie-1", role: "assistant", text: "Looking into it." },
    ]);
  });

  it("maps tool, runtime status, and provider summary to progress", () => {
    const events: AgentEvent[] = [
      { type: "tool_start", id: "t1", name: "graphjin_query" },
      { type: "tool_end", id: "t1" },
      { type: "status", message: "Thinking" },
      {
        type: "progress",
        id: "summary-1",
        content: "Checking the sales totals.",
        source: "provider_summary",
        provider: "google-gemini",
      },
    ];
    expect(toInteractionEvents(events)).toEqual([
      { kind: "progress", id: "t1", label: "graphjin_query", phase: "start" },
      { kind: "progress", id: "t1", label: "t1", phase: "end" },
      { kind: "progress", id: "ie-1", label: "Thinking", phase: "start" },
      { kind: "progress", id: "summary-1", label: "Checking the sales totals.", phase: "start" },
    ]);
  });

  it("extracts a modality-free core from an A2UI surface and keeps it as enrichment", () => {
    const messages = [
      { version: "v0.9", createSurface: { surfaceId: "s1", catalogId: "urn:app:catalog:briefing:v1" } },
      {
        version: "v0.9",
        updateComponents: {
          surfaceId: "s1",
          components: [
            { id: "intro", component: "Markdown", text: "Morning." },
            {
              id: "card1",
              component: "BriefingCard",
              mood: "good",
              text: "Revenue up",
              metric: "$4.7M",
              label: "Revenue MTD",
              detail: "Up 12% MoM.",
              chartType: "line",
              chartData: [{ d: "Mon", v: 1 }, { d: "Tue", v: 2 }],
            },
          ],
        },
      },
    ];
    const [event, ...rest] = toInteractionEvents([{ type: "surface", messages }]);
    expect(rest).toHaveLength(0);
    expect(event).toMatchObject({
      kind: "inform",
      mood: "good",
      title: "Revenue up",
      body: "Up 12% MoM.",
      metric: { label: "Revenue MTD", value: "$4.7M" },
      series: { kind: "line", points: [{ d: "Mon", v: 1 }, { d: "Tue", v: 2 }] },
    });
    expect((event as { enrichment: { surfaces: unknown[] } }).enrichment.surfaces).toBe(messages);
  });

  it("maps a pending action request to an approval ask and skips auto-approved ones", () => {
    const pending: AgentEvent = {
      type: "action_request_emit",
      action_request_id: "ar-1",
      kind: "send_slack_message",
      scope: "external",
      risk_level: "medium",
      intent: "Post the Q3 numbers to #exec",
      decision: "pending_approval",
    };
    const auto: AgentEvent = {
      type: "action_request_emit",
      action_request_id: "ar-2",
      kind: "append_sheet_row",
      scope: "internal",
      decision: "auto_approved",
    };
    expect(toInteractionEvents([pending, auto])).toEqual([
      { kind: "ask", id: "ar-1", ask: "approval", prompt: "Post the Q3 numbers to #exec", decisionRef: "ar-1", risk: "medium" },
    ]);
  });

  it("maps an action result to a resolve", () => {
    const ok: AgentEvent = {
      type: "action_request_result",
      action_request_id: "ar-1",
      kind: "send_slack_message",
      status: "succeeded",
      outcome: { externalRef: "1718.42" },
    };
    const rejected: AgentEvent = {
      type: "action_request_result",
      action_request_id: "ar-3",
      kind: "send_gmail",
      status: "rejected",
      rejection_reason: "not now",
    };
    const uncertain: AgentEvent = {
      type: "action_request_result",
      action_request_id: "ar-4",
      kind: "magento.manage_catalog",
      status: "reconcile_required",
    };
    expect(toInteractionEvents([ok, rejected, uncertain])).toEqual([
      { kind: "resolve", id: "ar-1", ref: "ar-1", status: "succeeded", summary: "send_slack_message → 1718.42" },
      { kind: "resolve", id: "ar-3", ref: "ar-3", status: "rejected", summary: "not now" },
      { kind: "resolve", id: "ar-4", ref: "ar-4", status: "reconcile_required", summary: "magento.manage_catalog needs reconciliation before retry" },
    ]);
  });

  it("maps needs_input to a choice or freeform ask", () => {
    const withOptions: AgentEvent = { type: "needs_input", question: "Which region?", options: ["NA", "EU"] };
    const freeform: AgentEvent = { type: "needs_input", question: "What's the budget?" };
    const [choice, free] = toInteractionEvents([withOptions, freeform]);
    expect(choice).toMatchObject({
      kind: "ask",
      id: "ie-1",
      decisionRef: "ie-1",
      ask: "choice",
      prompt: "Which region?",
      options: [{ id: "opt-0", label: "NA" }, { id: "opt-1", label: "EU" }],
    });
    expect(free).toMatchObject({
      kind: "ask",
      id: "ie-2",
      decisionRef: "ie-2",
      ask: "freeform",
      prompt: "What's the budget?",
    });
  });

  it("maps error to an act-mood inform and drops done", () => {
    expect(toInteractionEvents([{ type: "error", message: "boom" }, { type: "done" }])).toEqual([
      { kind: "inform", id: "ie-1", mood: "act", title: "Something went wrong", body: "boom" },
    ]);
  });

  it("maps vitals to a modality-free highlight, dropping a sub when absent", () => {
    const vitals: AgentEvent = {
      type: "vitals",
      items: [
        { label: "Top-10 share", value: "48%", sub: "down from 53%" },
        { label: "YoY revenue", value: "+14%" },
      ],
    };
    expect(toInteractionEvents([vitals])).toEqual([
      {
        kind: "highlight",
        id: "ie-1",
        metrics: [
          { label: "Top-10 share", value: "48%", sub: "down from 53%" },
          { label: "YoY revenue", value: "+14%" },
        ],
      },
    ]);
  });

  it("drops an empty vitals event entirely", () => {
    expect(toInteractionEvents([{ type: "vitals", items: [] }])).toEqual([]);
  });

  it("maps a sandbox network denial to an honest recovery event", () => {
    expect(
      toInteractionEvents([
        {
          type: "capability_denied",
          capability: "network_egress",
          reason: "policy_denied",
          host: "wttr.in",
          port: 80,
          method: "GET",
          path: "/Igatpuri",
        },
      ]),
    ).toEqual([
      {
        kind: "inform",
        id: "ie-1",
        mood: "watch",
        title: "Network access blocked: wttr.in",
        body: "Live data was not retrieved. Use an approved integration or contact an administrator.",
        evidence: [
          {
            label: "Denied by sandbox policy",
            detail: "GET wttr.in:80/Igatpuri",
          },
        ],
      },
    ]);
  });
});
