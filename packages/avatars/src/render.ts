import { n as round, type Scene, type SceneDef, type SceneNode, type Stop } from './scene';

/** The element types a renderer supplies: tag names on the web, react-native-svg components on mobile. */
export interface SvgComponents {
  Svg: unknown;
  G: unknown;
  Path: unknown;
  Circle: unknown;
  Ellipse: unknown;
  Defs: unknown;
  ClipPath: unknown;
  RadialGradient: unknown;
  LinearGradient: unknown;
  Stop: unknown;
}

/** React.createElement, or any function with its shape. */
export type CreateElement<E> = (type: any, props: Record<string, unknown> | null, ...children: E[]) => E;

const paint = (p: { fill?: string; stroke?: string; strokeWidth?: number; opacity?: number; linecap?: string; linejoin?: string }) => {
  const out: Record<string, unknown> = {};
  out.fill = p.fill ?? 'none';
  if (p.stroke) out.stroke = p.stroke;
  if (p.strokeWidth !== undefined) out.strokeWidth = p.strokeWidth;
  if (p.opacity !== undefined) out.opacity = round(p.opacity);
  if (p.linecap) out.strokeLinecap = p.linecap;
  if (p.linejoin) out.strokeLinejoin = p.linejoin;
  return out;
};

/** Turn a scene into elements with any createElement-style function. */
export function renderScene<E>(scene: Scene, h: CreateElement<E>, C: SvgComponents, rootProps: Record<string, unknown> = {}): E {
  const stops = (list: Stop[]) => list.map((s) => h(C.Stop, { offset: s.offset, stopColor: s.color, stopOpacity: s.opacity ?? 1 }));

  const node = (n: SceneNode): E => {
    switch (n.type) {
      case 'g': {
        const props: Record<string, unknown> = {};
        if (n.transform) props.transform = n.transform;
        if (n.clip) props.clipPath = n.clip;
        if (n.opacity !== undefined) props.opacity = round(n.opacity);
        return h(C.G, props, ...n.children.map(node));
      }
      case 'path':
        return h(C.Path, { d: n.d, ...paint(n) });
      case 'ellipse':
        return h(C.Ellipse, { cx: n.cx, cy: n.cy, rx: n.rx, ry: n.ry, ...(n.transform ? { transform: n.transform } : {}), ...paint(n) });
      case 'circle':
        return h(C.Circle, { cx: n.cx, cy: n.cy, r: n.r, ...paint(n) });
    }
  };

  const def = (d: SceneDef): E => {
    switch (d.type) {
      case 'radial':
        return h(C.RadialGradient, { id: d.id, cx: d.cx, cy: d.cy, r: d.r, fx: d.cx, fy: d.cy, gradientUnits: 'userSpaceOnUse' }, ...stops(d.stops));
      case 'linear':
        return h(C.LinearGradient, { id: d.id, x1: d.x1, y1: d.y1, x2: d.x2, y2: d.y2, gradientUnits: 'userSpaceOnUse' }, ...stops(d.stops));
      case 'clip':
        return h(C.ClipPath, { id: d.id }, ...d.children.map(node));
    }
  };

  return h(C.Svg, { viewBox: scene.viewBox, ...rootProps }, h(C.Defs, null, ...scene.defs.map(def)), ...scene.nodes.map(node));
}
