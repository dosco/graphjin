import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { assignLayers, diagramGraph, layoutDiagram } from "@/a2ui/answer-diagram";
import { renderComponent } from "@/a2ui/renderer";
import type { SurfaceState } from "@/a2ui/types";

const EDGES = [
  { from: "Customer", to: "Sales order", label: "places" },
  { from: "Sales order", to: "Order lines" },
  { from: "Sales order", to: "Credit check" },
  { from: "Order lines", to: "Inventory" },
  { from: "Credit check", to: "Shipment" },
  { from: "Inventory", to: "Shipment" },
];

function render(component: Record<string, unknown>) {
  const surface = { surfaceId: "s", components: new Map(), dataModel: {} } as unknown as SurfaceState;
  return renderToStaticMarkup(createElement("div", null, renderComponent(component as never, { surface })));
}

function overlap(a: { x: number; y: number; w: number; h: number }, b: typeof a) {
  return a.x < b.x + b.w && b.x < a.x + a.w && a.y < b.y + b.h && b.y < a.y + a.h;
}

describe("answer diagram", () => {
  it("adds edge endpoints the agent did not list and drops self-loops", () => {
    const graph = diagramGraph([{ id: "Customer", label: "Customer" }], [...EDGES, { from: "Invoice", to: "Invoice" }]);
    expect(graph.nodes.map((node) => node.id)).toContain("Shipment");
    expect(graph.edges.some((edge) => edge.from === edge.to)).toBe(false);
  });

  it("layers by longest path", () => {
    const { nodes, edges } = diagramGraph([], EDGES);
    const layer = assignLayers(nodes, edges);
    expect(layer.get("Customer")).toBe(0);
    expect(layer.get("Shipment")).toBe(4);
  });

  it("terminates on cycles", () => {
    const { nodes, edges } = diagramGraph([], [{ from: "A", to: "B" }, { from: "B", to: "C" }, { from: "C", to: "A" }]);
    const layout = layoutDiagram(nodes, edges, "lr", 900);
    expect(layout.nodes).toHaveLength(3);
    expect(layout.edges.every((edge) => edge.path.startsWith("M"))).toBe(true);
  });

  it("places boxes without overlap in both directions", () => {
    const { nodes, edges } = diagramGraph([], EDGES);
    for (const direction of ["lr", "tb"] as const) {
      const layout = layoutDiagram(nodes, edges, direction, 360);
      for (const [index, a] of layout.nodes.entries()) {
        for (const b of layout.nodes.slice(index + 1)) expect(overlap(a, b)).toBe(false);
      }
    }
  });

  it("fits a phone width top to bottom", () => {
    const { nodes, edges } = diagramGraph([], EDGES);
    expect(layoutDiagram(nodes, edges, "tb", 358).width).toBeLessThanOrEqual(358);
  });

  it("caps the diagram at 30 steps", () => {
    const edges = Array.from({ length: 40 }, (_, index) => ({ from: `S${index}`, to: `S${index + 1}` }));
    expect(diagramGraph([], edges).nodes).toHaveLength(30);
  });

  it("renders drill-down steps as buttons that pick the step", () => {
    const html = render({ id: "d", component: "Diagram", title: "Order flow", nodes: [], edges: EDGES, drill: "Show records in {label}" });
    expect(html).toContain('aria-label="Inventory" aria-pressed="false"');
    expect(html).toContain("marker");
  });
});
