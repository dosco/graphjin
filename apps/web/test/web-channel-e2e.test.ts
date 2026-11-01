import { describe, expect, it, vi } from "vitest";
import type { ReactElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { applyMessage, getResolvedComponents } from "@/a2ui/surface";
import { renderComponent } from "@/a2ui/renderer";
import "@/a2ui/components"; // side-effect: registers the real Briefing/Markdown/BriefingCard renderers
import { CATALOG_ID, WORK_CATALOG_ID, ComponentTypes } from "@/a2ui/catalog";
import type { A2UIComponent, A2UIMessage, SurfaceState } from "@/a2ui/types";
import { webProjection } from "@neko/channels";
import { WEB_PROFILE, type IntentEvent } from "@neko/interaction";
import type { AgentEvent } from "@neko/llm";
import { toInteractionEvents } from "@neko/llm/interaction";

/**
 * End-to-end through the web channel, all real code, no plugins/server/DB:
 *   AgentEvent[]  →  toInteractionEvents (the waist mapper)
 *                 →  webProjection (the built-in web channel)
 *                 →  applyMessage (apps/web's actual A2UI surface reducer)
 *                 →  resolved components the renderer consumes.
 */

const agentStream: AgentEvent[] = [
  { type: "status", message: "Reading the sales data source" },
  {
    type: "surface",
    messages: [
      { version: "v0.9", createSurface: { surfaceId: "x", catalogId: CATALOG_ID } },
      {
        version: "v0.9",
        updateComponents: {
          surfaceId: "x",
          components: [
            {
              id: "card1",
              component: "BriefingCard",
              mood: "good",
              text: "Q3 revenue landed at $4.7M",
              metric: "$4.7M",
              label: "Revenue MTD",
              detail: "Up 12% MoM, driven by enterprise expansion.",
              chartType: "line",
              chartData: [{ d: "Jul", v: 3.9 }, { d: "Aug", v: 4.2 }, { d: "Sep", v: 4.7 }],
            },
          ],
        },
      },
    ],
  },
  { type: "message", role: "assistant", content: "Want me to post the Q3 summary to #exec?" },
  {
    type: "action_request_emit",
    action_request_id: "ar-501",
    kind: "send_slack_message",
    scope: "external",
    risk_level: "medium",
    intent: "Post the Q3 revenue summary to #exec",
    decision: "pending_approval",
  },
];

const buildSurface = () => {
  const events = toInteractionEvents(agentStream);
  const projection = webProjection(events, WEB_PROFILE);
  let surfaces = new Map<string, SurfaceState>();
  for (const message of projection.surfaces) {
    surfaces = applyMessage(surfaces, message as unknown as A2UIMessage);
  }
  return { events, projection, surfaces };
};

describe("web channel — end to end through the real A2UI surface pipeline", () => {
  it("targets the real briefing catalog", () => {
    const { projection } = buildSurface();
    const create = projection.surfaces[0] as unknown as { createSurface: { catalogId: string } };
    expect(create.createSurface.catalogId).toBe(CATALOG_ID);
  });

  it("renders a MetricCard the web reducer resolves, carrying the agent's numbers", () => {
    const { surfaces } = buildSurface();
    const surface = surfaces.get("s1");
    expect(surface).toBeDefined();

    const resolved = getResolvedComponents(surface!);
    const card = resolved.find((c) => c.component === ComponentTypes.MetricCard) as A2UIComponent;
    expect(card).toMatchObject({
      mood: "good",
      text: "Q3 revenue landed at $4.7M",
      metric: "$4.7M",
      label: "Revenue MTD",
      chartType: "line",
    });
    expect((card.chartData as unknown[]).length).toBe(3);
  });

  it("renders the assistant reply as Markdown", () => {
    const { surfaces } = buildSurface();
    const resolved = getResolvedComponents(surfaces.get("s1")!);
    const markdown = resolved.find((c) => c.component === ComponentTypes.Markdown) as A2UIComponent;
    expect(markdown.text).toBe("Want me to post the Q3 summary to #exec?");
  });

  it("surfaces the approval to the web's native affordance, not A2UI", () => {
    const { projection, surfaces } = buildSurface();
    const resolved = getResolvedComponents(surfaces.get("s1")!);
    // the ask is NOT an A2UI component — it's a web-native pending approval
    expect(resolved.some((c) => c.component === "ApprovalRequest")).toBe(false);
    expect(projection.pendingAsks).toHaveLength(1);
    expect(projection.pendingAsks[0]!.decisionRef).toBe("ar-501");
  });

  it("closes the loop: a web Approve click yields a decision for the same request", () => {
    const { projection } = buildSurface();
    const ask = projection.pendingAsks[0]!;
    // what the web Approve button emits → feeds approveActionRequest(orgId, decisionRef)
    const intent: IntentEvent = { kind: "decision", decisionRef: ask.decisionRef, choice: "approve" };
    expect(intent).toEqual({ kind: "decision", decisionRef: "ar-501", choice: "approve" });
  });
});

const renderHtml = (component: A2UIComponent, surface: SurfaceState): string =>
  renderToStaticMarkup(renderComponent(component, { surface, extras: {} }) as ReactElement);

describe("web channel — rendered HTML through the real React renderers", () => {
  it("the MetricCard renders the agent's metric and label", () => {
    const { surfaces } = buildSurface();
    const surface = surfaces.get("s1")!;
    const card = getResolvedComponents(surface).find((c) => c.component === ComponentTypes.MetricCard)!;
    const html = renderHtml(card, surface);
    expect(html).toContain("$4.7M");
    expect(html).toContain("Revenue MTD");
  });

  it("the assistant reply renders as markdown HTML", () => {
    const { surfaces } = buildSurface();
    const surface = surfaces.get("s1")!;
    const markdown = getResolvedComponents(surface).find((c) => c.component === ComponentTypes.Markdown)!;
    const html = renderHtml(markdown, surface);
    expect(html).toContain("Want me to post the Q3 summary to #exec?");
  });

  it("renders the briefing body even when the root omits children (regression: blank card, work c8073aae)", () => {
    let surfaces = new Map<string, SurfaceState>();
    const messages: A2UIMessage[] = [
      { version: "v0.9", createSurface: { surfaceId: "s1", catalogId: CATALOG_ID } },
      {
        version: "v0.9",
        updateComponents: {
          surfaceId: "s1",
          components: [
            // root WITHOUT `children` — the shape that rendered a blank body in prod
            { id: "root", component: "Briefing", greeting: "SLA Watch", subtitle: "Pending orders", role: "Operations Specialist" },
            { id: "intro", component: "Markdown", text: "The Slow-Ship workflow flags pending orders." },
            { id: "card1", component: "BriefingCard", mood: "act", text: "SLA breaches", metric: "10,878", label: "Breaching Orders", detail: "98% of the queue.", chartType: "kpi", chartData: [] },
            { id: "details", component: "Markdown", text: "How it works: runs daily at 8:30 AM UTC." },
          ],
        },
      },
    ];
    for (const m of messages) surfaces = applyMessage(surfaces, m);
    const surface = surfaces.get("s1")!;
    const html = renderHtml(surface.components.get("root")!, surface);
    expect(html).toContain("SLA Watch"); // header still renders
    expect(html).toContain("The Slow-Ship workflow flags pending orders."); // intro body
    expect(html).toContain("How it works"); // details body
    expect(html).toContain("Breaching Orders"); // the KPI card body
  });
});

describe("web channel — expanded A2UI catalog (Table / Callout / Section / Choice / Divider)", () => {
  const surfaceWith = (components: A2UIComponent[]): SurfaceState => {
    let surfaces = new Map<string, SurfaceState>();
    surfaces = applyMessage(surfaces, {
      version: "v0.9",
      createSurface: { surfaceId: "s1", catalogId: CATALOG_ID },
    });
    surfaces = applyMessage(surfaces, {
      version: "v0.9",
      updateComponents: { surfaceId: "s1", components },
    });
    return surfaces.get("s1")!;
  };

  it("Table renders columns, rows, and column alignment", () => {
    const surface = surfaceWith([
      {
        id: "t",
        component: "Table",
        columns: [
          { key: "region", label: "Region" },
          { key: "rev", label: "Revenue", align: "right" },
        ],
        rows: [
          { region: "Southwest", rev: "$4.7M" },
          { region: "Canada", rev: "$3.1M" },
        ],
      },
    ] as A2UIComponent[]);
    const html = renderHtml(surface.components.get("t")!, surface);
    expect(html).toContain("Region");
    expect(html).toContain("Revenue");
    expect(html).toContain("Southwest");
    expect(html).toContain("$3.1M");
    expect(html).toContain("text-align:right");
  });

  it("Callout renders its mood class, title, and markdown body", () => {
    const surface = surfaceWith([
      { id: "c", component: "Callout", mood: "act", title: "Takeaway", text: "Revenue is **down** 12%." },
    ] as A2UIComponent[]);
    const html = renderHtml(surface.components.get("c")!, surface);
    expect(html).toContain("work-callout-act");
    expect(html).toContain("Takeaway");
    expect(html).toContain("<strong>down</strong>");
  });

  it("Section (collapsible) renders a details/summary disclosure with its body", () => {
    const surface = surfaceWith([
      { id: "sec", component: "Section", title: "Supporting detail", collapsible: true, children: ["m"] },
      { id: "m", component: "Markdown", text: "Inner body line." },
    ] as A2UIComponent[]);
    const html = renderHtml(surface.components.get("sec")!, surface);
    expect(html).toContain("<details");
    expect(html).toContain("<summary");
    expect(html).toContain("Supporting detail");
    expect(html).toContain("Inner body line.");
  });

  it("Choice renders one button per option with its label", () => {
    const surface = surfaceWith([
      {
        id: "ch",
        component: "Choice",
        options: [
          { label: "Drill into Q3", prompt: "Break down Q3 revenue by product." },
          { label: "Show the trend", prompt: "Show the 12-month revenue trend." },
        ],
      },
    ] as A2UIComponent[]);
    const html = renderHtml(surface.components.get("ch")!, surface);
    expect(html).toContain("Drill into Q3");
    expect(html).toContain("Show the trend");
    expect((html.match(/<button/g) ?? []).length).toBe(2);
  });

  it("Choice fires an action carrying the option's prompt (the follow-up loop)", () => {
    const surface = surfaceWith([
      {
        id: "ch",
        component: "Choice",
        options: [{ label: "Drill in", prompt: "Break down Q3 revenue by product." }],
      },
    ] as A2UIComponent[]);
    const onAction = vi.fn();
    type ButtonEl = ReactElement<{ onClick: () => void }>;
    const el = renderComponent(surface.components.get("ch")!, { surface, onAction }) as ReactElement<{
      children: ButtonEl[];
    }>;
    el.props.children[0].props.onClick();
    expect(onAction).toHaveBeenCalledWith("ch", "select", {
      prompt: "Break down Q3 revenue by product.",
      value: "Drill in",
    });
  });
});

describe("web channel — Answer root (work/Ask vocabulary, not 'Briefing')", () => {
  const answerSurface = (root: Record<string, unknown>, body: A2UIComponent[]): SurfaceState => {
    let surfaces = new Map<string, SurfaceState>();
    surfaces = applyMessage(surfaces, {
      version: "v0.9",
      createSurface: { surfaceId: "s1", catalogId: CATALOG_ID },
    });
    surfaces = applyMessage(surfaces, {
      version: "v0.9",
      updateComponents: {
        surfaceId: "s1",
        components: [{ id: "root", component: "Answer", ...root }, ...body] as A2UIComponent[],
      },
    });
    return surfaces.get("s1")!;
  };

  it("renders the title and body, with no dashboard 'Briefing' eyebrow", () => {
    const surface = answerSurface({ title: "Revenue Summary", children: ["m"] }, [
      { id: "m", component: "Markdown", text: "Up 12% MoM." } as A2UIComponent,
    ]);
    const html = renderHtml(surface.components.get("root")!, surface);
    expect(html).toContain("Revenue Summary");
    expect(html).toContain("Up 12% MoM.");
    expect(html).not.toContain("Briefing");
  });

  it("renders an optional eyebrow kicker when provided", () => {
    const surface = answerSurface({ title: "Revenue Summary", eyebrow: "SALES", children: ["m"] }, [
      { id: "m", component: "Markdown", text: "Body." } as A2UIComponent,
    ]);
    const html = renderHtml(surface.components.get("root")!, surface);
    expect(html).toContain("SALES");
    expect(html).toContain("work-surface-eyebrow");
  });

  it("falls back to all body components when the Answer root omits children", () => {
    const surface = answerSurface({ title: "Revenue Summary" }, [
      { id: "m1", component: "Markdown", text: "First line." } as A2UIComponent,
      { id: "m2", component: "Markdown", text: "Second line." } as A2UIComponent,
    ]);
    const html = renderHtml(surface.components.get("root")!, surface);
    expect(html).toContain("First line.");
    expect(html).toContain("Second line.");
  });

  it("MetricCard renders the same KPI chrome as BriefingCard", () => {
    const surface = answerSurface({ title: "Q3", children: ["c"] }, [
      {
        id: "c",
        component: "MetricCard",
        metricId: "chat-1",
        source: "chat",
        mood: "good",
        text: "Revenue up",
        metric: "$4.7M",
        label: "Revenue MTD",
        detail: "Up 12% MoM.",
        chartType: "kpi",
        chartData: [],
      } as A2UIComponent,
    ]);
    const html = renderHtml(surface.components.get("root")!, surface);
    expect(html).toContain("$4.7M");
    expect(html).toContain("Revenue MTD");
  });

  it("renders an agent chosen chart with a title, source, and accessible values", () => {
    const surface = answerSurface({ title: "Orders", children: ["trend"] }, [
      {
        id: "trend",
        component: "Chart",
        title: "Orders by week",
        type: "line",
        valueLabel: "Orders",
        source: "Orders database",
        data: [{ d: "Sep 7", v: 42 }, { d: "Sep 14", v: 47 }],
      } as A2UIComponent,
    ]);
    const html = renderHtml(surface.components.get("root")!, surface);
    expect(html).toContain("work-chart");
    expect(html).toContain("Orders by week");
    expect(html).toContain("Source: Orders database");
    expect(html).toContain("<caption>Orders by week values</caption>");
    expect(html).toContain("<table class=\"sr-only\"");
    expect(html).toContain("Sep 14");
    expect(html).toContain("recharts-surface");
    expect(html).not.toContain("Card actions");
  });

  it("resolves chart points from the A2UI data model", () => {
    const surface = answerSurface({ title: "Orders", children: ["trend"] }, [
      {
        id: "trend", component: "Chart", title: "Orders by week", type: "bar",
        valueLabel: "Orders", data: { path: "/series" },
      } as A2UIComponent,
    ]);
    surface.dataModel = { series: [{ d: "Sep 7", v: 42 }, { d: "Sep 14", v: 47 }] };
    const html = renderHtml(surface.components.get("root")!, surface);
    expect(html).toContain("recharts-surface");
    expect(html).toContain("<td>47</td>");
  });
});

describe("web channel — A2UI v1.0 configuration form", () => {
  const buildForm = (): SurfaceState => {
    const surfaces = applyMessage(new Map(), {
      version: "v1.0",
      createSurface: {
        surfaceId: "source-form-1",
        catalogId: WORK_CATALOG_ID,
        dataModel: {
          form: { name: "warehouse", kind: ["database"], port: 5432 },
        },
        components: [
          { id: "root", component: "Column", children: ["name", "kind", "submit"] },
          { id: "name", component: "TextField", label: "Source name", value: { path: "/form/name" }, placeholder: "warehouse" },
          { id: "kind", component: "ChoicePicker", label: "Source kind", options: [{ label: "Database", value: "database" }], value: { path: "/form/kind" } },
          { id: "submitLabel", component: "Text", text: "Review proposal" },
          {
            id: "submit",
            component: "Button",
            child: "submitLabel",
            variant: "primary",
            action: {
              event: {
                name: "submit_source_config",
                context: {
                  prompt: "Review these values and file a proposal.",
                  values: { path: "/form" },
                },
              },
            },
          },
        ],
      },
    });
    return surfaces.get("source-form-1")!;
  };

  it("renders bound fields and the proposal action from a single createSurface", () => {
    const surface = buildForm();
    const html = renderHtml(surface.components.get("root")!, surface);
    expect(html).toContain("Source name");
    expect(html).toContain('value="warehouse"');
    expect(html).toContain("Source kind");
    expect(html).toContain("Review proposal");
  });

  it("resolves the live form object into the Button action context", () => {
    const surface = buildForm();
    const onAction = vi.fn();
    const button = renderComponent(surface.components.get("submit")!, {
      surface,
      onAction,
    }) as ReactElement<{ onClick: () => void }>;
    button.props.onClick();
    expect(onAction).toHaveBeenCalledWith("submit", "submit_source_config", {
      prompt: "Review these values and file a proposal.",
      values: { name: "warehouse", kind: ["database"], port: 5432 },
    });
  });

  it("renders only the field group matching the selected source kind", () => {
    const surfaces = applyMessage(new Map(), {
      version: "v1.0",
      createSurface: {
        surfaceId: "typed-source-form",
        catalogId: WORK_CATALOG_ID,
        dataModel: { form: { kind: ["api"] } },
        components: [
          // Gemini has emitted Answer roots without children. The renderer's
          // top-level fallback must keep nested controls from appearing twice.
          { id: "root", component: "Answer", title: "Add a source" },
          { id: "common", component: "Column", children: ["name", "kind"] },
          { id: "name", component: "TextField", label: "Source name" },
          { id: "kind", component: "ChoicePicker", label: "Source kind", options: [], value: { path: "/form/kind" }, variant: "mutuallyExclusive" },
          { id: "database", component: "Conditional", when: { path: "/form/kind" }, equals: "database", children: ["dbHost"] },
          { id: "dbHost", component: "TextField", label: "Database host" },
          { id: "api", component: "Conditional", when: { path: "/form/kind" }, equals: "api", children: ["openApiSpec"] },
          { id: "openApiSpec", component: "OpenApiSpecInput", label: "OpenAPI specification", value: { path: "/form/openApiSpec" } },
          { id: "file", component: "Conditional", when: { path: "/form/kind" }, equals: "file", children: ["bucket"] },
          { id: "bucket", component: "TextField", label: "Storage bucket" },
        ],
      },
    });
    const surface = surfaces.get("typed-source-form")!;
    const html = renderHtml(surface.components.get("root")!, surface);
    expect(html).toContain("OpenAPI specification");
    expect(html).toContain("Hosted URL");
    expect(html).not.toContain("Database host");
    expect(html).not.toContain("Storage bucket");
    expect(html.match(/Source name/g)).toHaveLength(1);
  });

  it("matches a Conditional oneOf branch against a one-item picker array", () => {
    const surfaces = applyMessage(new Map(), {
      version: "v1.0",
      createSurface: {
        surfaceId: "storage-form",
        catalogId: WORK_CATALOG_ID,
        dataModel: { form: { backend: ["gcs"] } },
        components: [
          { id: "root", component: "Column", children: ["object", "s3"] },
          { id: "object", component: "Conditional", when: { path: "/form/backend" }, oneOf: ["s3", "gcs"], children: ["bucket"] },
          { id: "bucket", component: "TextField", label: "Storage bucket" },
          { id: "s3", component: "Conditional", when: { path: "/form/backend" }, equals: "s3", children: ["region"] },
          { id: "region", component: "TextField", label: "S3 region" },
        ],
      },
    });
    const surface = surfaces.get("storage-form")!;
    const html = renderHtml(surface.components.get("root")!, surface);
    expect(html).toContain("Storage bucket");
    expect(html).not.toContain("S3 region");
  });
});
