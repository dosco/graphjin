import { describe, expect, it } from "vitest";
import {
  applyMessage,
  bodyChildIds,
  createSurface,
  getResolvedComponents,
  getRootComponent,
  resolveComponent,
  resolveDynamic,
} from "@/a2ui/surface";
import type { A2UIMessage, SurfaceState } from "@/a2ui/types";

describe("createSurface", () => {
  it("initializes empty components and dataModel", () => {
    const s = createSurface("s1", "cat:1");
    expect(s.surfaceId).toBe("s1");
    expect(s.catalogId).toBe("cat:1");
    expect(s.components.size).toBe(0);
    expect(s.dataModel).toEqual({});
    expect(s.theme).toBeUndefined();
  });

  it("preserves theme when provided", () => {
    const theme = { brand: "neko" };
    const s = createSurface("s1", "cat:1", theme);
    expect(s.theme).toEqual(theme);
  });

  it("creates a complete v1.0 surface from one message", () => {
    const surfaces = applyMessage(new Map(), {
      version: "v1.0",
      createSurface: {
        surfaceId: "form-1",
        catalogId: "urn:openneko:catalog:work:v2",
        surfaceProperties: { agentDisplayName: "OpenNeko" },
        dataModel: { form: { name: "warehouse" } },
        components: [
          { id: "root", component: "Column", children: ["name"] },
          { id: "name", component: "TextField", label: "Name", value: { path: "/form/name" } },
        ],
      },
    });
    const surface = surfaces.get("form-1")!;
    expect(surface.version).toBe("v1.0");
    expect(surface.surfaceProperties).toEqual({ agentDisplayName: "OpenNeko" });
    expect(surface.dataModel).toEqual({ form: { name: "warehouse" } });
    expect(surface.components.get("name")?.component).toBe("TextField");
  });
});

describe("resolveDynamic", () => {
  it("returns literal value when no path", () => {
    expect(resolveDynamic("hello", {})).toBe("hello");
    expect(resolveDynamic(42, {})).toBe(42);
  });

  it("returns null for null literal (regression: don't treat null as object)", () => {
    expect(resolveDynamic(null as never, { x: 1 })).toBeNull();
  });

  it("resolves a path reference against dataModel", () => {
    expect(resolveDynamic({ path: "/x" }, { x: 42 })).toBe(42);
  });

  it("resolves nested JSON Pointer", () => {
    expect(resolveDynamic({ path: "/a/b/c" }, { a: { b: { c: "deep" } } })).toBe("deep");
  });

  it("returns root with empty path or '/'", () => {
    const model = { x: 1 };
    expect(resolveDynamic({ path: "/" }, model)).toEqual(model);
  });

  it("returns undefined for missing path", () => {
    expect(resolveDynamic({ path: "/missing" }, { x: 1 })).toBeUndefined();
  });
});

describe("resolveComponent", () => {
  it("resolves dynamic-valued props but leaves id and component literal", () => {
    const c = {
      id: "card-1",
      component: "BriefingCard",
      mood: { path: "/insights/0/mood" },
      text: "literal-text",
    };
    const model = { insights: [{ mood: "good" }] };
    expect(resolveComponent(c, model)).toEqual({
      id: "card-1",
      component: "BriefingCard",
      mood: "good",
      text: "literal-text",
    });
  });

  it("resolves nested action context bindings", () => {
    const component = {
      id: "submit",
      component: "Button",
      action: {
        event: {
          name: "submit_config",
          context: { prompt: "Review this", values: { path: "/form" } },
        },
      },
    };
    expect(resolveComponent(component, { form: { name: "warehouse", port: 5432 } })).toMatchObject({
      action: {
        event: {
          context: { prompt: "Review this", values: { name: "warehouse", port: 5432 } },
        },
      },
    });
  });
});

describe("applyMessage", () => {
  it("createSurface adds an empty surface", () => {
    const next = applyMessage(new Map(), {
      version: "v0.9",
      createSurface: { surfaceId: "s1", catalogId: "cat:1" },
    });
    expect(next.has("s1")).toBe(true);
    expect(next.get("s1")?.components.size).toBe(0);
  });

  it("updateComponents adds components by id; same id replaces", () => {
    let s = applyMessage(new Map(), {
      version: "v0.9",
      createSurface: { surfaceId: "s1", catalogId: "cat:1" },
    });
    s = applyMessage(s, {
      version: "v0.9",
      updateComponents: {
        surfaceId: "s1",
        components: [
          { id: "a", component: "Briefing" },
          { id: "b", component: "BriefingCard" },
        ],
      },
    });
    expect(s.get("s1")?.components.size).toBe(2);

    s = applyMessage(s, {
      version: "v0.9",
      updateComponents: {
        surfaceId: "s1",
        components: [{ id: "a", component: "Briefing", title: "v2" }],
      },
    });
    expect(s.get("s1")?.components.size).toBe(2);
    expect(s.get("s1")?.components.get("a")?.title).toBe("v2");
  });

  it("updateDataModel without path replaces entire model", () => {
    let s = applyMessage(new Map(), {
      version: "v0.9",
      createSurface: { surfaceId: "s1", catalogId: "cat:1" },
    });
    s = applyMessage(s, {
      version: "v0.9",
      updateDataModel: {
        surfaceId: "s1",
        value: { greeting: "hi" },
      },
    });
    expect(s.get("s1")?.dataModel).toEqual({ greeting: "hi" });
  });

  it("updateDataModel with path sets a nested value (deep merge via JSON Pointer)", () => {
    let s = applyMessage(new Map(), {
      version: "v0.9",
      createSurface: { surfaceId: "s1", catalogId: "cat:1" },
    });
    s = applyMessage(s, {
      version: "v0.9",
      updateDataModel: { surfaceId: "s1", value: { existing: "yes" } },
    });
    s = applyMessage(s, {
      version: "v0.9",
      updateDataModel: { surfaceId: "s1", path: "/added", value: 1 },
    });
    expect(s.get("s1")?.dataModel).toEqual({ existing: "yes", added: 1 });
  });

  it("updateDataModel with value=undefined deletes the key at path", () => {
    let s = applyMessage(new Map(), {
      version: "v0.9",
      createSurface: { surfaceId: "s1", catalogId: "cat:1" },
    });
    s = applyMessage(s, {
      version: "v0.9",
      updateDataModel: { surfaceId: "s1", value: { a: 1, b: 2 } },
    });
    s = applyMessage(s, {
      version: "v0.9",
      updateDataModel: { surfaceId: "s1", path: "/a", value: undefined },
    });
    expect(s.get("s1")?.dataModel).toEqual({ b: 2 });
  });

  it("v1.0 updateDataModel deletes a key with null", () => {
    let s = applyMessage(new Map(), {
      version: "v1.0",
      createSurface: {
        surfaceId: "s1",
        catalogId: "cat:2",
        dataModel: { a: 1, b: 2 },
      },
    });
    s = applyMessage(s, {
      version: "v1.0",
      updateDataModel: { surfaceId: "s1", path: "/a", value: null },
    });
    expect(s.get("s1")?.dataModel).toEqual({ b: 2 });
  });

  it("deleteSurface removes the surface", () => {
    let s = applyMessage(new Map(), {
      version: "v0.9",
      createSurface: { surfaceId: "s1", catalogId: "cat:1" },
    });
    s = applyMessage(s, { version: "v0.9", deleteSurface: { surfaceId: "s1" } });
    expect(s.has("s1")).toBe(false);
  });

  it("updateComponents on missing surface is a no-op", () => {
    const s = applyMessage(new Map(), {
      version: "v0.9",
      updateComponents: { surfaceId: "missing", components: [] },
    });
    expect(s.size).toBe(0);
  });
});

describe("bodyChildIds", () => {
  const build = (rootExtra: Record<string, unknown>, bodyIds: string[]): SurfaceState => {
    let s = applyMessage(new Map(), {
      version: "v0.9",
      createSurface: { surfaceId: "s1", catalogId: "cat:1" },
    });
    s = applyMessage(s, {
      version: "v0.9",
      updateComponents: {
        surfaceId: "s1",
        components: [
          { id: "root", component: "Briefing", ...rootExtra },
          ...bodyIds.map((id) => ({ id, component: "Markdown", text: id })),
        ],
      },
    });
    return s.get("s1")!;
  };

  it("returns declared children verbatim (does not add orphans)", () => {
    const surface = build({ children: ["intro", "card1"] }, ["intro", "card1", "extra"]);
    expect(bodyChildIds(surface, getRootComponent(surface)!)).toEqual(["intro", "card1"]);
  });

  it("falls back to every non-root component in document order when children is missing", () => {
    const surface = build({}, ["intro", "card1", "details"]);
    expect(bodyChildIds(surface, getRootComponent(surface)!)).toEqual([
      "intro",
      "card1",
      "details",
    ]);
  });

  it("falls back when children is an empty array", () => {
    const surface = build({ children: [] }, ["intro", "details"]);
    expect(bodyChildIds(surface, getRootComponent(surface)!)).toEqual(["intro", "details"]);
  });

  it("falls back to top-level orphans without repeating nested form controls", () => {
    const surfaces = applyMessage(new Map(), {
      version: "v1.0",
      createSurface: {
        surfaceId: "source-form",
        catalogId: "cat:2",
        components: [
          { id: "root", component: "Answer", title: "Configure source" },
          { id: "note", component: "Callout", text: "Approval required" },
          { id: "fields", component: "Column", children: ["name", "submit"] },
          { id: "name", component: "TextField", label: "Source name" },
          { id: "submitLabel", component: "Text", text: "Review" },
          { id: "submit", component: "Button", child: "submitLabel" },
        ],
      },
    });
    const surface = surfaces.get("source-form")!;
    expect(bodyChildIds(surface, getRootComponent(surface)!)).toEqual([
      "note",
      "fields",
    ]);
  });

  it("does not absorb orphans for a non-root container", () => {
    const surface = build({}, ["intro", "extra"]);
    expect(bodyChildIds(surface, surface.components.get("intro")!)).toEqual([]);
  });
});

describe("end-to-end: building a briefing surface", () => {
  it("full message sequence produces a renderable surface", () => {
    const messages: A2UIMessage[] = [
      {
        version: "v0.9",
        createSurface: { surfaceId: "briefing-ceo", catalogId: "urn:cat:v1" },
      },
      {
        version: "v0.9",
        updateDataModel: {
          surfaceId: "briefing-ceo",
          value: {
            insights: { "card-1": { mood: "good", text: "All good" } },
          },
        },
      },
      {
        version: "v0.9",
        updateComponents: {
          surfaceId: "briefing-ceo",
          components: [
            { id: "root", component: "Briefing" },
            {
              id: "card-1",
              component: "BriefingCard",
              mood: { path: "/insights/card-1/mood" },
              text: { path: "/insights/card-1/text" },
            },
          ],
        },
      },
    ];

    let surfaces = new Map<string, SurfaceState>();
    for (const msg of messages) surfaces = applyMessage(surfaces, msg);

    const surface = surfaces.get("briefing-ceo");
    expect(surface).toBeDefined();
    expect(getRootComponent(surface!)?.component).toBe("Briefing");

    const resolved = getResolvedComponents(surface!);
    const card = resolved.find((c) => c.id === "card-1");
    expect(card).toMatchObject({ mood: "good", text: "All good" });
  });
});
