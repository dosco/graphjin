import { readFile } from "node:fs/promises";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import {
  buildRunTimeline,
  splitProgressSections,
  type WorkEvent,
} from "../src/app/(work)/work/work-screen";

function toolItems(events: WorkEvent[]) {
  return buildRunTimeline(events, "run-1").items.filter(
    (item) => item.kind === "tools",
  );
}

describe("work run action timeline", () => {
  it("keeps shared Button-based tool headers left aligned", async () => {
    const css = await readFile(
      fileURLToPath(new URL("../src/app/styles/_work.css", import.meta.url)),
      "utf8",
    );
    const rule = (selector: string) => {
      const start = css.indexOf(`${selector} {`);
      return css.slice(start, css.indexOf("}", start) + 1);
    };

    expect(rule(".work-tool-group-head")).toContain(
      "justify-content: flex-start",
    );
    expect(rule(".work-tool-row-head")).toContain(
      "justify-content: flex-start",
    );
  });

  it("interleaves provider summaries without replacing assistant conversation", () => {
    const result = buildRunTimeline(
      [
        { type: "message", role: "assistant", content: "I’ll check the sales data." },
        {
          type: "progress",
          id: "gemini-summary-1",
          content: "Comparing won revenue by owner.",
          source: "provider_summary",
          provider: "google-gemini",
        },
        { type: "tool_start", id: "tool-1", name: "Inspect data" },
        { type: "tool_end", id: "tool-1", result: { ok: true } },
        {
          type: "surface",
          messages: [
            {
              version: "v1.0",
              createSurface: {
                surfaceId: "answer-run-1",
                catalogId: "urn:openneko:catalog:work:v2",
                components: [
                  { id: "root", component: "Answer", children: ["copy"] },
                  { id: "copy", component: "Markdown", text: "Sales ranking" },
                ],
              },
            },
          ],
        },
        { type: "message", role: "assistant", content: "Maya leads by a clear margin." },
        { type: "done" },
      ],
      "run-1",
    );

    expect(result.items.map((item) => item.kind)).toEqual([
      "text",
      "progress",
      "tools",
      "surface",
      "text",
    ]);
    expect(result.items.filter((item) => item.kind === "text")).toEqual([
      { kind: "text", content: "I’ll check the sales data." },
      { kind: "text", content: "Maya leads by a clear margin." },
    ]);
  });

  it("keeps provider progress detail behind its heading", async () => {
    const source = await readFile(
      fileURLToPath(new URL("../src/app/(work)/work/work-screen.tsx", import.meta.url)),
      "utf8",
    );
    expect(source).toContain('import { Disclosure } from "@/components/ui/disclosure";');
    expect(source).toContain('className="work-progress-disclosure"');
    expect(source).toContain('function splitProgressSections');
    expect(source).not.toContain('meta={live && index === sections.length - 1 ? "Live" : undefined}');
    expect(source).not.toContain('replace(/^#{1,6}\\s+/, "")');
  });

  it("turns plain provider headings into collapsed progress sections", () => {
    const content = [
      "Analyzing Sales Data",
      "",
      "I've begun analyzing the sales data.",
      "",
      "Comparing Bonus Structures",
      "",
      "The bonus structures differ across the top performers.",
      "",
      "Calculating Commission Impacts",
      "",
      "Commission has the larger impact.",
      "",
      "```json",
      '{"neko_value":{"type":"text"}}',
      "```",
    ].join("\n");

    expect(splitProgressSections(content)).toEqual([
      {
        heading: "Analyzing Sales Data",
        detail: "I've begun analyzing the sales data.",
      },
      {
        heading: "Comparing Bonus Structures",
        detail: "The bonus structures differ across the top performers.",
      },
      {
        heading: "Calculating Commission Impacts",
        detail: [
          "Commission has the larger impact.",
          "",
          "```json",
          '{"neko_value":{"type":"text"}}',
          "```",
        ].join("\n"),
      },
    ]);
  });

  it("uses standalone strong Markdown as the visible provider heading", () => {
    expect(
      splitProgressSections([
        "**Analyzing Successes and Needs**",
        "",
        "The render tool worked; now reviewing the closing blocks.",
      ].join("\n")),
    ).toEqual([
      {
        heading: "Analyzing Successes and Needs",
        detail: "The render tool worked; now reviewing the closing blocks.",
      },
    ]);
  });

  it("keeps unstructured provider summaries available for inline display", () => {
    expect(splitProgressSections("Checked the latest sales totals.")).toEqual([
      { heading: "Details", detail: "Checked the latest sales totals." },
    ]);

    // Every note, with or without headings, now renders inline as one note
    // (see work-reasoning.test.ts).
  });

  it("keeps plain completed answers as prose and adds vitals only as evidence", () => {
    const result = buildRunTimeline(
      [
        { type: "message", role: "assistant", content: "Maya is the top seller." },
        {
          type: "vitals",
          items: [{ label: "Won revenue", value: "$1.2M", basis: "calculated" }],
        },
        { type: "done" },
      ],
      "run-1",
    );

    expect(result.items[0]).toEqual({
      kind: "text",
      content: "Maya is the top seller.",
    });
    expect(result.items[1]?.kind).toBe("surface");
  });

  it("replaces an earlier canonical answer when the agent retries with a new surface id", () => {
    const result = buildRunTimeline(
      [
        {
          type: "surface",
          messages: [
            {
              version: "v1.0",
              createSurface: {
                surfaceId: "sales-report-01",
                catalogId: "urn:openneko:catalog:work:v2",
                components: [
                  { id: "root", component: "Answer", title: "Sales performance" },
                  { id: "table", component: "Table", rows: [] },
                ],
              },
            },
          ],
        },
        {
          type: "surface",
          messages: [
            {
              version: "v1.0",
              updateDataModel: {
                surfaceId: "sales-report-01",
                path: "/draft",
                value: true,
              },
            },
            {
              version: "v1.0",
              createSurface: {
                surfaceId: "sales-report-02",
                catalogId: "urn:openneko:catalog:work:v2",
                components: [
                  {
                    id: "root",
                    component: "Answer",
                    title: "Sales performance",
                    children: ["table"],
                  },
                  { id: "table", component: "Table", rows: [] },
                ],
              },
            },
            {
              version: "v1.0",
              updateDataModel: {
                surfaceId: "sales-report-01",
                path: "/late",
                value: true,
              },
            },
          ],
        },
      ],
      "run-1",
    );

    const surface = result.items.find((item) => item.kind === "surface");
    expect(surface?.kind).toBe("surface");
    if (surface?.kind !== "surface") throw new Error("Expected surface");
    expect(surface.messages).toHaveLength(1);
    expect(surface.messages[0]).toMatchObject({
      createSurface: { surfaceId: "sales-report-02" },
    });
  });

  it("keeps distinct non-answer surfaces in the same run", () => {
    const result = buildRunTimeline(
      ["source-form", "confirmation"].map((surfaceId) => ({
        type: "surface" as const,
        messages: [
          {
            version: "v1.0" as const,
            createSurface: {
              surfaceId,
              catalogId: "urn:openneko:catalog:work:v2",
              components: [
                { id: "root", component: "Answer", children: ["field"] },
                { id: "field", component: "TextField", label: surfaceId },
              ],
            },
          },
        ],
      })),
      "run-1",
    );

    const surface = result.items.find((item) => item.kind === "surface");
    expect(surface?.kind).toBe("surface");
    if (surface?.kind !== "surface") throw new Error("Expected surface");
    expect(surface.messages).toHaveLength(2);
  });

  it("keeps auto-approved actions as a single ordinary tool row", () => {
    const items = toolItems([
      { type: "tool_start", id: "tool-1", name: "web_search" },
      {
        type: "action_request_emit",
        action_request_id: "action-1",
        kind: "web_search",
        scope: "external",
        summary: "Search for the Igatpuri forecast",
        decision: "auto_approved",
      },
      {
        type: "action_request_result",
        action_request_id: "action-1",
        kind: "web_search",
        status: "succeeded",
      },
      { type: "tool_end", id: "tool-1", result: { ok: true } },
    ]);

    expect(items).toHaveLength(1);
    expect(items[0]?.kind).toBe("tools");
    if (items[0]?.kind !== "tools") throw new Error("Expected tool row");
    expect(items[0].tools).toHaveLength(1);
    expect(items[0].tools[0]?.approval).toBeUndefined();
  });

  it("attaches a pending approval to the originating tool row", () => {
    const items = toolItems([
      {
        type: "tool_start",
        id: "tool-1",
        name: "request_plugin_install",
      },
      {
        type: "action_request_emit",
        action_request_id: "action-1",
        kind: "plugin_install",
        scope: "external",
        intent: "Install the weather integration",
        decision: "pending_approval",
      },
      { type: "tool_end", id: "tool-1", result: { ok: true } },
      {
        type: "action_request_result",
        action_request_id: "action-1",
        kind: "plugin_install",
        status: "succeeded",
      },
    ]);

    expect(items).toHaveLength(1);
    if (items[0]?.kind !== "tools") throw new Error("Expected tool row");
    expect(items[0].tools).toHaveLength(1);
    expect(items[0].tools[0]?.approval).toMatchObject({
      actionRequestId: "action-1",
      actionKind: "plugin_install",
      intent: "Install the weather integration",
      result: { status: "succeeded" },
    });
  });

  it("uses the same tool row shape for historical approvals without tool events", () => {
    const items = toolItems([
      {
        type: "action_request_emit",
        action_request_id: "action-legacy",
        kind: "plugin_uninstall",
        scope: "external",
        summary: "Remove the unused integration",
        decision: "pending_approval",
      },
    ]);

    expect(items).toHaveLength(1);
    if (items[0]?.kind !== "tools") throw new Error("Expected tool row");
    expect(items[0].tools[0]).toMatchObject({
      id: "approval-action-legacy",
      name: "plugin_uninstall",
      approval: {
        actionRequestId: "action-legacy",
        actionKind: "plugin_uninstall",
      },
    });
  });

  it("keeps a modality-free clarification fallback only when no A2UI form was emitted", () => {
    const withoutSurface = buildRunTimeline(
      [
        {
          type: "needs_input",
          question: "Which destination?",
          options: ["Mumbai", "London"],
        },
        { type: "done", result: { status: "needs_input" } },
      ],
      "run-1",
    );
    expect(withoutSurface.items).toEqual([
      {
        kind: "needs_input",
        request: {
          type: "needs_input",
          question: "Which destination?",
          options: ["Mumbai", "London"],
        },
      },
    ]);

    const withSurface = buildRunTimeline(
      [
        {
          type: "surface",
          messages: [
            {
              version: "v1.0",
              createSurface: {
                surfaceId: "clarification-run-1",
                catalogId: "urn:openneko:catalog:work:v2",
                components: [{ id: "root", component: "Answer", children: [] }],
              },
            },
          ],
        },
        {
          type: "needs_input",
          question: "Which destination?",
          surfaceId: "clarification-run-1",
        },
      ],
      "run-1",
    );
    expect(withSurface.items.filter((item) => item.kind === "needs_input")).toEqual([]);
    expect(withSurface.items.filter((item) => item.kind === "surface")).toHaveLength(1);
  });
});

describe("work run stop", () => {
  it("shows a stopped run as stopped, and a failed run as failed", () => {
    const stopped = buildRunTimeline([
      { type: "error", message: "Cancelled by user." },
      { type: "done", result: { status: "cancelled" } },
    ] as WorkEvent[], "run-1").items.map((item) => item.kind);
    expect(stopped).toContain("stopped");
    expect(stopped).not.toContain("error");

    const failed = buildRunTimeline([
      { type: "error", message: "agent sandbox exited 1" },
      { type: "done", result: { status: "failed" } },
    ] as WorkEvent[], "run-1").items.map((item) => item.kind);
    expect(failed).toContain("error");
    expect(failed).not.toContain("stopped");
  });
});
