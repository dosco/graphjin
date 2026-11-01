import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { formatMapValue, placePins, validMapPoints } from "@/a2ui/answer-map";
import { renderComponent } from "@/a2ui/renderer";
import type { SurfaceState } from "@/a2ui/types";

const US = [
  { label: "Southwest", lat: 34.05, lon: -112.07, v: 1121473 },
  { label: "Northeast", lat: 42.36, lon: -73, v: 301552 },
  { label: "Central", lat: 41.6, lon: -93.6, v: 402118 },
];

function render(component: Record<string, unknown>) {
  const surface = {
    version: "v1.0",
    surfaceId: "s",
    catalogId: "urn:openneko:catalog:work:v2",
    components: new Map([["map", component as never]]),
    dataModel: {},
  } as unknown as SurfaceState;
  return renderToStaticMarkup(createElement("div", null, renderComponent(component as never, { surface })));
}

describe("answer map", () => {
  it("formats currency compactly and exactly", () => {
    expect(formatMapValue(1121473, "Sales (USD)", true)).toBe("$1.12M");
    expect(formatMapValue(1121473, "Sales (USD)", false)).toBe("$1,121,473");
    expect(formatMapValue(4210, "Orders", true)).toBe("4.21K");
  });

  it("drops points without valid coordinates or values", () => {
    expect(validMapPoints([...US, { label: "Bad", lat: 120, lon: 0, v: 1 }, { label: "", lat: 0, lon: 0, v: 1 }])).toHaveLength(3);
  });

  it("labels the largest places that fit without overlapping", () => {
    const pins = placePins([
      { label: "Big", v: 900, x: 200, y: 200 },
      { label: "Close", v: 800, x: 210, y: 205 },
      { label: "Far", v: 100, x: 600, y: 300 },
      { label: "Edge", v: 700, x: 2, y: 200 },
    ], 800, 400);
    expect(pins.map((pin) => pin.label)).toEqual(["Big", "Far"]);
  });

  it("ranks every place with its value and a drill-down button", () => {
    const html = render({
      id: "map", component: "AnswerMap", title: "Sales by territory", valueLabel: "Sales (USD)", points: US,
      drill: "Show the top customers in {label}",
    });
    expect(html).toContain('aria-label="Sales by territory, ranked by Sales (USD)"');
    expect(html.indexOf("Southwest")).toBeLessThan(html.indexOf("Central"));
    expect(html.indexOf("Central")).toBeLessThan(html.indexOf("Northeast"));
    expect(html).toContain("$1.12M");
    expect(html).toContain('title="Show the top customers in Southwest"');
    expect(html.match(/<button/g)).toHaveLength(3);
  });

  it("lists places as text without a drill-down", () => {
    const html = render({ id: "map", component: "AnswerMap", title: "Sales", valueLabel: "Sales (USD)", points: US });
    expect(html).not.toContain("<button");
    expect(html).toContain("Southwest");
  });

  it("renders nothing for an empty map", () => {
    expect(render({ id: "map", component: "AnswerMap", title: "Sales", valueLabel: "Sales", points: [] })).toBe("<div></div>");
  });
});
