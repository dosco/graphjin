import type { NekoColors, NekoFrame, NekoState } from '../types';
import { ellipse, g, matrix, mul, path, rotateAt, scaleAt, translate, type Paint, type SceneDef, type SceneNode } from '../scene';

export interface StyleInput {
  colors: NekoColors;
  frame: NekoFrame;
  /** Prefix for gradient and clip ids, unique per avatar on the page. */
  id: string;
  previous?: NekoState;
  switched?: number;
  floorShadow: boolean;
}

export interface StyleOutput {
  defs: SceneDef[];
  nodes: SceneNode[];
}

/* The shared cat: every style draws these shapes, in a 200 × 200 view. */

export const HEAD = [
  { cx: 100, cy: 106, rx: 70, ry: 58 },
  { cx: 67, cy: 132, rx: 39, ry: 30 },
  { cx: 133, cy: 132, rx: 39, ry: 30 },
];

export const EAR = 'M50 96 L54 25 L101 60 Z';
export const EAR_INNER = 'M62 74 L62 42 L86 60 Z';
const EAR_PIVOT: [number, number] = [75.5, 78];

export const EYE_L: [number, number] = [76, 108];
export const EYE_R: [number, number] = [124, 108];

export const PATCH = 'M116 40 C146 38 178 64 176 104 C154 106 130 94 118 78 Z';
export const TABBY_FOREHEAD = 'M100 50 L100 68 M87 54 L90 68 M113 54 L110 68';
export const TABBY_CHEEKS = 'M32 124 L50 128 M34 136 L51 137 M168 124 L150 128 M166 136 L149 137';

export const mirror = 'matrix(-1 0 0 1 200 0)';

export function headShapes(paint: Paint): SceneNode[] {
  return HEAD.map((h) => ellipse(h.cx, h.cy, h.rx, h.ry, paint));
}

/** Ear transforms: the left ear turns about its base, the right one is its mirror image. */
export function earTransforms(f: NekoFrame): [string, string] {
  const [px, py] = EAR_PIVOT;
  const left = rotateAt(-f.earL, px, py);
  const right = mul([-1, 0, 0, 1, 200, 0], rotateAt(-f.earR, px, py));
  return [matrix(left), matrix(right)];
}

/** Hop, bob, tilt and squash for the whole cat. */
export function bodyTransform(f: NekoFrame): string {
  const sx = 1 + f.squash * 0.5, sy = 1 - f.squash;
  return matrix(mul(mul(translate(0, f.bob - f.hop), rotateAt(f.tilt, 100, 150)), scaleAt(sx, sy, 100, 168)));
}

export function shadow(f: NekoFrame): SceneNode {
  const k = Math.max(0.3, 1 - f.hop / 40);
  return ellipse(100, 184, 56 * k, 7 * k, { fill: '#000000', opacity: 0.12 * k });
}

/** Whiskers droop about their roots; the right set is the mirror image. */
export function whiskers(f: NekoFrame, lines: string, paint: Paint): SceneNode {
  const side = () => g([path(lines, paint)], { transform: matrix(rotateAt(-f.whisker, 56, 138)) });
  return g([side(), g([side()], { transform: mirror })]);
}

/** Coat markings, to be drawn clipped to the head. */
export function markings(c: NekoColors, paint: (colour: string) => Paint): SceneNode[] {
  if (c.pattern === 'patch') return [path(PATCH, paint(c.marking))];
  if (c.pattern === 'tabby') {
    const p = { ...paint(c.marking), fill: 'none', stroke: c.marking, linecap: 'round' as const };
    return [path(TABBY_FOREHEAD, { ...p, strokeWidth: 5 }), path(TABBY_CHEEKS, { ...p, strokeWidth: 4 })];
  }
  return [];
}
