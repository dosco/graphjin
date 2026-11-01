"use client";

import React, { useEffect, useRef, useState } from "react";
import { registerComponent } from "./renderer";
import type { RenderContext } from "./renderer";
import type { A2UIComponent } from "./types";
import type { DiagramEdge, DiagramNode, DiagramProps } from "./catalog";
import { AskCard } from "./ask";
import { drillPrompt } from "./drill";

const MAX_NODES = 30;
const NODE_GAP = 18;
const LAYER_GAP_LR = 64;
const LAYER_GAP_TB = 54;
const LINE_HEIGHT = 16;
const CHAR_WIDTH = 7.1;
const NARROW = 560;

export type Direction = "lr" | "tb";

export type LaidNode = DiagramNode & { layer: number; x: number; y: number; w: number; h: number; lines: string[] };
export type LaidEdge = DiagramEdge & { path: string; labelX: number; labelY: number; labelShown: boolean };
export type DiagramLayout = { width: number; height: number; nodes: LaidNode[]; edges: LaidEdge[] };

/** Nodes in input order plus any edge endpoint the agent did not list, capped at 30. */
export function diagramGraph(nodes: unknown, edges: unknown): { nodes: DiagramNode[]; edges: DiagramEdge[] } {
  const byId = new Map<string, DiagramNode>();
  for (const node of Array.isArray(nodes) ? nodes : []) {
    if (node && typeof node.id === "string" && node.id.trim()) {
      byId.set(node.id, { id: node.id, label: typeof node.label === "string" && node.label.trim() ? node.label : node.id });
    }
  }
  const valid: DiagramEdge[] = [];
  for (const edge of Array.isArray(edges) ? edges : []) {
    if (!edge || typeof edge.from !== "string" || typeof edge.to !== "string" || !edge.from || !edge.to) continue;
    for (const id of [edge.from, edge.to]) if (!byId.has(id)) byId.set(id, { id, label: id });
    valid.push({ from: edge.from, to: edge.to, ...(typeof edge.label === "string" && edge.label.trim() ? { label: edge.label } : {}) });
  }
  const kept = [...byId.values()].slice(0, MAX_NODES);
  const ids = new Set(kept.map((node) => node.id));
  return { nodes: kept, edges: valid.filter((edge) => ids.has(edge.from) && ids.has(edge.to) && edge.from !== edge.to) };
}

/** Longest-path layers after reversing the edges that close a cycle. */
export function assignLayers(nodes: DiagramNode[], edges: DiagramEdge[]): Map<string, number> {
  const out = new Map(nodes.map((node) => [node.id, [] as string[]]));
  for (const edge of edges) out.get(edge.from)!.push(edge.to);
  const state = new Map<string, 1 | 2>();
  const forward: Array<[string, string]> = [];
  const visit = (id: string) => {
    state.set(id, 1);
    for (const next of out.get(id)!) {
      if (state.get(next) === 1) forward.push([next, id]);
      else {
        forward.push([id, next]);
        if (!state.has(next)) visit(next);
      }
    }
    state.set(id, 2);
  };
  for (const node of nodes) if (!state.has(node.id)) visit(node.id);
  const incoming = new Map(nodes.map((node) => [node.id, [] as string[]]));
  for (const [from, to] of forward) incoming.get(to)!.push(from);
  const layer = new Map<string, number>();
  const depth = (id: string, guard: Set<string>): number => {
    const known = layer.get(id);
    if (known !== undefined) return known;
    if (guard.has(id)) return 0;
    guard.add(id);
    const value = Math.max(-1, ...incoming.get(id)!.map((from) => depth(from, guard))) + 1;
    guard.delete(id);
    layer.set(id, value);
    return value;
  };
  for (const node of nodes) depth(node.id, new Set());
  return layer;
}

function orderLayers(ids: string[], links: Array<[string, string]>, layer: Map<string, number>): string[][] {
  const count = Math.max(0, ...layer.values()) + 1;
  const layers: string[][] = Array.from({ length: count }, () => []);
  for (const id of ids) layers[layer.get(id)!]!.push(id);
  const neighbours = new Map(ids.map((id) => [id, [] as string[]]));
  for (const [from, to] of links) {
    neighbours.get(from)!.push(to);
    neighbours.get(to)!.push(from);
  }
  const position = new Map<string, number>();
  const index = () => layers.forEach((row) => row.forEach((id, i) => position.set(id, i)));
  index();
  for (let sweep = 0; sweep < 6; sweep += 1) {
    const order = sweep % 2 === 0 ? [...layers.keys()] : [...layers.keys()].reverse();
    for (const current of order) {
      const reference = sweep % 2 === 0 ? current - 1 : current + 1;
      if (reference < 0 || reference >= count) continue;
      const score = (id: string) => {
        const linked = neighbours.get(id)!.filter((other) => layer.get(other) === reference);
        return linked.length ? linked.reduce((sum, other) => sum + position.get(other)!, 0) / linked.length : position.get(id)!;
      };
      layers[current]!.sort((a, b) => score(a) - score(b));
      index();
    }
  }
  return layers;
}

/** Split a word longer than a line at dots and underscores, then by length. */
function pieces(word: string, maxChars: number): string[] {
  if (word.length <= maxChars) return [word];
  const parts = word.split(/(?<=[._/])/);
  const out: string[] = [];
  for (const part of parts) {
    const last = out.at(-1);
    if (last !== undefined && last.length + part.length <= maxChars) out[out.length - 1] = last + part;
    else for (let start = 0; start < part.length; start += maxChars) out.push(part.slice(start, start + maxChars));
  }
  return out;
}

function wrap(label: string, maxChars: number): string[] {
  const words = label.split(/\s+/).flatMap((word) => pieces(word, maxChars));
  const lines: string[] = [];
  for (const word of words) {
    const last = lines.at(-1);
    if (last !== undefined && last.length + word.length + 1 <= maxChars) lines[lines.length - 1] = `${last} ${word}`;
    else lines.push(word);
  }
  if (lines.length <= 2) return lines;
  const second = lines.slice(1).join(" ");
  return [lines[0]!, second.length > maxChars ? `${second.slice(0, maxChars - 1)}…` : second];
}

type Box = { x: number; y: number; w: number; h: number };
type Point = [number, number];

function segment(direction: Direction, [x1, y1]: Point, [x2, y2]: Point): string {
  if (direction === "lr") {
    const bend = Math.max(20, (x2 - x1) / 2);
    return `C${x1 + bend} ${y1} ${x2 - bend} ${y2} ${x2} ${y2}`;
  }
  const bend = Math.max(16, (y2 - y1) / 2);
  return `C${x1} ${y1 + bend} ${x2} ${y2 - bend} ${x2} ${y2}`;
}

function route(direction: Direction, from: Box, to: Box, via: Box[]): { path: string; x: number; y: number } {
  const lr = direction === "lr";
  if (lr ? to.x < from.x + from.w : to.y < from.y + from.h) {
    if (lr) {
      const [x1, y1, x2, y2] = [from.x + from.w / 2, from.y + from.h, to.x + to.w / 2, to.y + to.h];
      const drop = Math.max(y1, y2) + 34;
      return { path: `M${x1} ${y1}C${x1} ${drop} ${x2} ${drop} ${x2} ${y2}`, x: (x1 + x2) / 2, y: drop - 8 };
    }
    const [x1, y1, x2, y2] = [from.x + from.w, from.y + from.h / 2, to.x + to.w, to.y + to.h / 2];
    const reach = Math.max(x1, x2) + 30;
    return { path: `M${x1} ${y1}C${reach} ${y1} ${reach} ${y2} ${x2} ${y2}`, x: reach - 6, y: (y1 + y2) / 2 };
  }
  const points: Point[] = [
    lr ? [from.x + from.w, from.y + from.h / 2] : [from.x + from.w / 2, from.y + from.h],
    ...via.map((box): Point => [box.x + box.w / 2, box.y + box.h / 2]),
    lr ? [to.x, to.y + to.h / 2] : [to.x + to.w / 2, to.y],
  ];
  const path = `M${points[0]![0]} ${points[0]![1]}` + points.slice(1).map((point, index) => segment(direction, points[index]!, point)).join("");
  const [a, b] = [points[0]!, points[1]!];
  return { path, x: (a[0] + b[0]) / 2, y: (a[1] + b[1]) / 2 };
}

export function layoutDiagram(nodesIn: DiagramNode[], edges: DiagramEdge[], direction: Direction, available: number): DiagramLayout {
  const layer = assignLayers(nodesIn, edges);
  const ids = nodesIn.map((node) => node.id);
  const links: Array<[string, string]> = [];
  const waypoints = edges.map((edge, index) => {
    const start = layer.get(edge.from)!;
    const end = layer.get(edge.to)!;
    if (end <= start) {
      links.push([edge.from, edge.to]);
      return [] as string[];
    }
    const chain: string[] = [];
    for (let step = start + 1; step < end; step += 1) {
      const id = `\u0000${index}:${step}`;
      layer.set(id, step);
      ids.push(id);
      chain.push(id);
    }
    [edge.from, ...chain, edge.to].reduce((previous, current) => {
      links.push([previous, current]);
      return current;
    });
    return chain;
  });
  const layers = orderLayers(ids, links, layer);
  const widest = Math.max(1, ...layers.map((row) => row.filter((id) => !id.startsWith("\u0000")).length));
  const maxChars = direction === "tb"
    ? Math.max(9, Math.min(22, Math.floor(((available - NODE_GAP * (widest - 1)) / widest - 24) / CHAR_WIDTH)))
    : 22;
  const boxes = new Map<string, Box>();
  const nodes: LaidNode[] = [];
  for (const node of nodesIn) {
    const lines = wrap(node.label, maxChars);
    const w = Math.round(Math.min(maxChars, Math.max(6, ...lines.map((line) => line.length))) * CHAR_WIDTH + 28);
    const laid: LaidNode = { ...node, layer: layer.get(node.id)!, x: 0, y: 0, w, h: 22 + lines.length * LINE_HEIGHT, lines };
    nodes.push(laid);
    boxes.set(node.id, laid);
  }
  for (const id of ids) if (!boxes.has(id)) boxes.set(id, { x: 0, y: 0, w: direction === "lr" ? 0 : 14, h: direction === "lr" ? 14 : 0 });
  const extent = (row: string[], axis: "w" | "h") =>
    row.reduce((sum, id) => sum + boxes.get(id)![axis], 0) + NODE_GAP * Math.max(0, row.length - 1);
  let cursor = 0;
  if (direction === "lr") {
    const height = Math.max(...layers.map((row) => extent(row, "h")));
    for (const row of layers) {
      const width = Math.max(...row.map((id) => boxes.get(id)!.w));
      let y = (height - extent(row, "h")) / 2;
      for (const id of row) {
        const box = boxes.get(id)!;
        box.x = cursor + (width - box.w) / 2;
        box.y = y;
        y += box.h + NODE_GAP;
      }
      cursor += width + LAYER_GAP_LR;
    }
  } else {
    const width = Math.max(...layers.map((row) => extent(row, "w")));
    for (const row of layers) {
      const height = Math.max(...row.map((id) => boxes.get(id)!.h));
      let x = (width - extent(row, "w")) / 2;
      for (const id of row) {
        const box = boxes.get(id)!;
        box.x = x;
        box.y = cursor + (height - box.h) / 2;
        x += box.w + NODE_GAP;
      }
      cursor += height + LAYER_GAP_TB;
    }
  }
  const routed = () => edges.map((edge, index) => {
    const { path, x, y } = route(direction, boxes.get(edge.from)!, boxes.get(edge.to)!, waypoints[index]!.map((id) => boxes.get(id)!));
    return { ...edge, path, labelX: x, labelY: y };
  });
  const first = routed();
  const minX = Math.min(...[...boxes.values()].map((box) => box.x), ...first.map((edge) => edge.labelX - 40));
  const minY = Math.min(...[...boxes.values()].map((box) => box.y), ...first.map((edge) => edge.labelY - 10));
  for (const box of boxes.values()) { box.x -= minX - 4; box.y -= minY - 4; }
  const placed: Box[] = [...nodes];
  const laidEdges = routed().map((edge) => {
    if (!edge.label) return { ...edge, labelShown: false };
    const w = Math.min(edge.label.length, 26) * CHAR_WIDTH * 0.86 + 8;
    const box = { x: edge.labelX - w / 2, y: edge.labelY - 15, w, h: 15 };
    const clear = placed.every((other) => box.x + box.w < other.x || other.x + other.w < box.x || box.y + box.h < other.y || other.y + other.h < box.y);
    if (clear) placed.push(box);
    return { ...edge, label: edge.label.length > 26 ? `${edge.label.slice(0, 25)}…` : edge.label, fullLabel: edge.label, labelShown: clear };
  });
  const width = Math.max(...nodes.map((node) => node.x + node.w), ...laidEdges.map((edge) => edge.labelX + 40)) + 4;
  const height = Math.max(...nodes.map((node) => node.y + node.h), ...laidEdges.map((edge) => edge.labelY + 14)) + 4;
  return { width: Math.ceil(width), height: Math.ceil(height), nodes, edges: laidEdges };
}

function useWidth(): [React.RefObject<HTMLDivElement | null>, number] {
  const ref = useRef<HTMLDivElement | null>(null);
  const [width, setWidth] = useState(720);
  useEffect(() => {
    const node = ref.current;
    if (!node) return;
    const measure = () => setWidth(Math.max(240, Math.round(node.clientWidth)));
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(node);
    return () => observer.disconnect();
  }, []);
  return [ref, width];
}

function Diagram({ props, ctx }: { props: DiagramProps & { id: string }; ctx: RenderContext }) {
  const [ref, available] = useWidth();
  const [picked, setPicked] = useState<string | null>(null);
  const graph = diagramGraph(props.nodes, props.edges);
  let direction: Direction = available < NARROW ? "tb" : "lr";
  let layout = layoutDiagram(graph.nodes, graph.edges, direction, available);
  if (direction === "lr" && layout.width > available) {
    direction = "tb";
    layout = layoutDiagram(graph.nodes, graph.edges, direction, available);
  }
  const pickedNode = graph.nodes.find((node) => node.id === picked);
  const scale = Math.max(0.78, Math.min(1, available / layout.width));
  const marker = `answer-diagram-arrow-${props.id.replace(/[^a-zA-Z0-9_-]/g, "")}`;
  return (
    <figure className="answer-diagram" aria-label={props.title}>
      <figcaption className="work-chart-title">{props.title}</figcaption>
      <div ref={ref} className="answer-diagram-canvas" data-direction={direction}>
        <svg
          width={Math.round(layout.width * scale)}
          height={Math.round(layout.height * scale)}
          viewBox={`0 0 ${layout.width} ${layout.height}`}
          role="img"
          aria-label={`${props.title}: ${graph.edges.map((edge) => `${edge.from} to ${edge.to}${edge.label ? `, ${edge.label}` : ""}`).join("; ")}`}
        >
          <defs>
            <marker id={marker} viewBox="0 0 10 10" refX="9" refY="5" markerWidth="7" markerHeight="7" orient="auto-start-reverse">
              <path className="answer-diagram-arrowhead" d="M0 1L9 5L0 9Z" />
            </marker>
          </defs>
          {layout.edges.map((edge, index) => (
            <path key={`edge-${index}`} className="answer-diagram-edge" d={edge.path} markerEnd={`url(#${marker})`}>
              {edge.label ? <title>{`${edge.from} → ${edge.to}: ${(edge as LaidEdge & { fullLabel?: string }).fullLabel ?? edge.label}`}</title> : null}
            </path>
          ))}
          {layout.edges.map((edge, index) =>
            edge.label && edge.labelShown ? (
              <text key={`edge-label-${index}`} className="answer-diagram-edge-label" x={edge.labelX} y={edge.labelY} textAnchor="middle" dy="-0.35em">
                {edge.label}
              </text>
            ) : null,
          )}
          {layout.nodes.map((node, index) => {
            const prompt = drillPrompt(props.drill, node.label);
            return (
              <g
                key={node.id}
                className={`answer-diagram-node${prompt ? " is-interactive" : ""}${picked === node.id ? " is-picked" : ""}`}
                transform={`translate(${node.x} ${node.y})`}
                style={{ animationDelay: `${Math.min(index, 12) * 18}ms` }}
                {...(prompt
                  ? {
                      role: "button",
                      tabIndex: 0,
                      "aria-label": node.label,
                      "aria-pressed": picked === node.id,
                      "data-ui-bespoke-reason": "diagram step is an SVG mark that picks the step and shows its question",
                      onClick: () => setPicked(node.id),
                      onMouseEnter: () => setPicked(node.id),
                      onKeyDown: (event: React.KeyboardEvent) => {
                        if (event.key === "Enter" || event.key === " ") {
                          event.preventDefault();
                          setPicked(node.id);
                        }
                      },
                    }
                  : {})}
              >
                <rect className="answer-diagram-box" width={node.w} height={node.h} rx="8" />
                <text className="answer-diagram-text" x={node.w / 2} y={11 + LINE_HEIGHT * 0.8} textAnchor="middle">
                  {node.lines.map((line, lineIndex) => (
                    <tspan key={lineIndex} x={node.w / 2} dy={lineIndex === 0 ? 0 : LINE_HEIGHT}>{line}</tspan>
                  ))}
                </text>
              </g>
            );
          })}
        </svg>
      </div>
      {pickedNode ? (
        <AskCard ctx={ctx} componentId={props.id} template={props.drill} label={pickedNode.label} value={stepLinks(graph, pickedNode.id)} />
      ) : null}
    </figure>
  );
}

function stepLinks(graph: { nodes: DiagramNode[]; edges: DiagramEdge[] }, id: string): string | undefined {
  const label = (nodeId: string) => graph.nodes.find((node) => node.id === nodeId)?.label ?? nodeId;
  const from = graph.edges.filter((edge) => edge.to === id).map((edge) => label(edge.from));
  const to = graph.edges.filter((edge) => edge.from === id).map((edge) => label(edge.to));
  const parts = [from.length ? `From ${from.join(", ")}` : "", to.length ? `to ${to.join(", ")}` : ""].filter(Boolean);
  return parts.length ? parts.join(" · ") : undefined;
}

registerComponent("Diagram", (comp: A2UIComponent, ctx: RenderContext) => {
  const props = comp as unknown as DiagramProps & { id: string };
  if (typeof props.title !== "string" || diagramGraph(props.nodes, props.edges).nodes.length === 0) return null;
  return <Diagram key={props.id} props={props} ctx={ctx} />;
});
