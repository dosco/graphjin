/* A renderer-neutral drawing: plain shapes that map one to one onto SVG
   elements in the browser and onto react-native-svg components. */

export interface Paint {
  fill?: string;
  stroke?: string;
  strokeWidth?: number;
  opacity?: number;
  linecap?: 'round' | 'butt';
  linejoin?: 'round' | 'miter';
}

export type SceneNode =
  | ({ type: 'g'; transform?: string; clip?: string; children: SceneNode[] } & Pick<Paint, 'opacity'>)
  | ({ type: 'path'; d: string } & Paint)
  | ({ type: 'ellipse'; cx: number; cy: number; rx: number; ry: number; transform?: string } & Paint)
  | ({ type: 'circle'; cx: number; cy: number; r: number } & Paint);

export interface Stop {
  offset: number;
  color: string;
  opacity?: number;
}

export type SceneDef =
  | { type: 'radial'; id: string; cx: number; cy: number; r: number; stops: Stop[] }
  | { type: 'linear'; id: string; x1: number; y1: number; x2: number; y2: number; stops: Stop[] }
  | { type: 'clip'; id: string; children: SceneNode[] };

export interface Scene {
  /** Every style draws in the same 200 × 200 space; the view crops it to the cat. */
  viewBox: string;
  defs: SceneDef[];
  nodes: SceneNode[];
}

/** The cat at rest fills this square, with room above for hops. */
export const VIEWBOX = '10 6 180 180';

export const n = (v: number) => Math.round(v * 100) / 100;

export const g = (children: SceneNode[], extra: { transform?: string; clip?: string; opacity?: number } = {}): SceneNode => ({
  type: 'g',
  children,
  ...extra,
});
export const path = (d: string, paint: Paint): SceneNode => ({ type: 'path', d, ...paint });
export const ellipse = (cx: number, cy: number, rx: number, ry: number, paint: Paint, transform?: string): SceneNode => ({
  type: 'ellipse', cx, cy, rx, ry, transform, ...paint,
});
export const circle = (cx: number, cy: number, r: number, paint: Paint): SceneNode => ({ type: 'circle', cx, cy, r, ...paint });

/** A path of many dots, so a whole LED layer is one element. */
export function dots(points: [number, number][], r: number): string {
  let d = '';
  for (const [x, y] of points) d += `M${n(x - r)} ${n(y)}a${r} ${r} 0 1 0 ${n(2 * r)} 0a${r} ${r} 0 1 0 ${n(-2 * r)} 0`;
  return d;
}

export type Mat = [number, number, number, number, number, number];
export const IDENTITY: Mat = [1, 0, 0, 1, 0, 0];

export function mul(m: Mat, k: Mat): Mat {
  return [
    m[0] * k[0] + m[2] * k[1],
    m[1] * k[0] + m[3] * k[1],
    m[0] * k[2] + m[2] * k[3],
    m[1] * k[2] + m[3] * k[3],
    m[0] * k[4] + m[2] * k[5] + m[4],
    m[1] * k[4] + m[3] * k[5] + m[5],
  ];
}

export const translate = (x: number, y: number): Mat => [1, 0, 0, 1, x, y];
export const scaleAt = (sx: number, sy: number, cx: number, cy: number): Mat => [sx, 0, 0, sy, cx - sx * cx, cy - sy * cy];
export function rotateAt(deg: number, cx: number, cy: number): Mat {
  const a = (deg * Math.PI) / 180, c = Math.cos(a), s = Math.sin(a);
  return [c, s, -s, c, cx - c * cx + s * cy, cy - s * cx - c * cy];
}

/** A single matrix() string: every SVG renderer reads it the same way. */
export const matrix = (m: Mat) => `matrix(${m.map(n).join(' ')})`;
