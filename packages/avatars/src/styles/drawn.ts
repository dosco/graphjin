import type { NekoFrame, NekoState } from '../types';
import { isDark, mix } from '../color';
import { circle, ellipse, g, matrix, mul, path, rotateAt, scaleAt, translate, type SceneDef, type SceneNode } from '../scene';
import { EAR, EAR_INNER, EYE_L, EYE_R, HEAD, bodyTransform, earTransforms, headShapes, markings, mirror, shadow, whiskers, type StyleInput, type StyleOutput } from './anatomy';

/* The flat and ink styles: one drawn face, painted two ways. */

type Eye = 'open' | 'wide' | 'lid' | 'focus' | 'happy' | 'sleep' | 'x';
type Mouth = 'w' | 'flat' | 'open' | 'smile' | 'wavy';

const FACE: Record<NekoState, [Eye, Mouth]> = {
  idle: ['open', 'w'],
  thinking: ['lid', 'flat'],
  searching: ['wide', 'w'],
  listening: ['wide', 'w'],
  speaking: ['open', 'open'],
  working: ['focus', 'flat'],
  happy: ['happy', 'smile'],
  error: ['x', 'wavy'],
  sleeping: ['sleep', 'w'],
};

const OUTLINE = '#141413';
const NOSE = 'M94 126 h12 l-6 6.5 z';
const MOUTH_W = 'M100 134 q-4 7 -10 4 M100 134 q4 7 10 4';
const MOUTH_FLAT = 'M95 141 h10';
const MOUTH_WAVY = 'M86 142 q3.5 -5 7 0 q3.5 5 7 0 q3.5 -5 7 0 q3.5 5 7 0';

const pulse = (t: number, i: number) => {
  const p = (t * 1.6 - i * 0.22) % 1;
  return p < 0.35 ? Math.sin((p / 0.35) * Math.PI) : 0;
};

function speakAmount(t: number) {
  const env = Math.max(0, Math.sin(t * 2.1) * 0.5 + Math.sin(t * 3.7 + 1) * 0.5);
  const a = (0.5 + 0.3 * Math.sin(t * 17) + 0.2 * Math.sin(t * 29 + 1)) * (0.35 + env);
  return Math.min(1, Math.max(0, a));
}

function extras(f: NekoFrame, ink: string): SceneNode[] {
  const t = f.time;
  switch (f.state) {
    case 'thinking':
      return [
        circle(160, 60, 3.5, { fill: ink, opacity: 0.3 + 0.6 * pulse(t, 0) }),
        circle(170, 46, 4.5, { fill: ink, opacity: 0.3 + 0.6 * pulse(t, 1) }),
        circle(180, 30, 6, { fill: ink, opacity: 0.3 + 0.6 * pulse(t, 2) }),
      ];
    case 'listening': {
      const p = (t * 1.4) % 1;
      return [
        path('M162 46 q9 9 0 20', { stroke: ink, strokeWidth: 3, linecap: 'round', fill: 'none', opacity: 0.25 + 0.6 * Math.sin(p * Math.PI) }),
        path('M171 38 q14 15 0 34', { stroke: ink, strokeWidth: 3, linecap: 'round', fill: 'none', opacity: 0.25 + 0.6 * Math.sin(((p + 0.5) % 1) * Math.PI) }),
      ];
    }
    case 'working':
      return [g([path('M178 37 A9 9 0 1 1 169 46', { stroke: ink, strokeWidth: 3.5, linecap: 'round', fill: 'none', opacity: 0.8 })], {
        transform: matrix(rotateAt((t * 300) % 360, 178, 46)),
      })];
    case 'sleeping':
      return [0, 1].map((k) => {
        const p = ((t + k * 1.2) / 2.4) % 1;
        const s = 0.7 + p * 0.5;
        return g([path('M-5 -5 h10 l-10 10 h10', { stroke: ink, strokeWidth: 2.6, linecap: 'round', linejoin: 'round', fill: 'none' })], {
          transform: matrix(mul(translate(156 + p * 14, 66 - p * 36), scaleAt(s, s, 0, 0))),
          opacity: Math.sin(p * Math.PI) * 0.8,
        });
      });
    default:
      return [];
  }
}

interface Painter {
  ink: string;
  eyeOpen(cx: number, cy: number, f: NekoFrame, wide: boolean): SceneNode[];
  stroke: number;
}

function eyes(kind: Eye, f: NekoFrame, p: Painter): SceneNode[] {
  const line = { stroke: p.ink, strokeWidth: p.stroke, linecap: 'round' as const, fill: 'none' };
  const out: SceneNode[] = [];
  for (const [cx, cy] of [EYE_L, EYE_R]) {
    const right = cx > 100;
    if (kind === 'happy') out.push(path(`M${cx - 9} ${cy + 3} Q${cx} ${cy - 8} ${cx + 9} ${cy + 3}`, line));
    else if (kind === 'sleep') out.push(path(`M${cx - 9} ${cy - 2} Q${cx} ${cy + 6} ${cx + 9} ${cy - 2}`, line));
    else if (kind === 'x') out.push(path(`M${cx - 7} ${cy - 7} l14 14 M${cx + 7} ${cy - 7} l-14 14`, line));
    else {
      const squint = kind === 'focus' ? 0.55 : 1;
      const open = Math.max(0.08, (1 - f.blink * 0.92) * squint);
      out.push(g(p.eyeOpen(cx, cy, f, kind === 'wide'), { transform: matrix(scaleAt(1, open, cx, cy + (kind === 'focus' ? 4 : 0))) }));
      if (kind === 'lid') {
        const d = right ? `M${cx - 10} ${cy - 10} Q${cx} ${cy - 16} ${cx + 10} ${cy - 12}` : `M${cx - 10} ${cy - 12} Q${cx} ${cy - 16} ${cx + 10} ${cy - 10}`;
        out.push(path(d, { ...line, strokeWidth: p.stroke * 0.8 }));
      }
    }
  }
  return out;
}

function mouth(kind: Mouth, f: NekoFrame, ink: string, width: number, tongue: string): SceneNode[] {
  const line = { stroke: ink, strokeWidth: width, linecap: 'round' as const, linejoin: 'round' as const, fill: 'none' };
  switch (kind) {
    case 'w': return [path(MOUTH_W, line)];
    case 'flat': return [path(MOUTH_FLAT, line)];
    case 'wavy': return [path(MOUTH_WAVY, line)];
    case 'smile': return [path('M88 135 Q100 152 112 135 Z', { fill: ink, stroke: ink, strokeWidth: 2, linejoin: 'round' }), ellipse(100, 143.5, 5, 3, { fill: tongue })];
    case 'open': {
      const a = speakAmount(f.time);
      if (a < 0.25) return [path(MOUTH_W, line)];
      return [ellipse(100, 139 + 3 * a, 5 + 2 * a, 2 + 6 * a, { fill: ink }), ellipse(100, 141 + 5 * a, 3 + a, 1 + 2 * a, { fill: tongue })];
    }
  }
}

export function flat({ colors: c, frame: f, floorShadow, id }: StyleInput, onDark = false): StyleOutput {
  const dark = isDark(c.coat);
  const ink = dark ? '#f4efe6' : '#2a211c';
  const innerEar = mix(c.coat, '#f4a6a6', 0.6);
  const [earL, earR] = earTransforms(f);
  const [eyeKind, mouthKind] = FACE[f.state];
  const painter: Painter = {
    ink,
    stroke: 4,
    eyeOpen: (cx, cy, fr, wide) => {
      const ex = cx + fr.lookX * 4, ey = cy + fr.lookY * 3;
      const rx = wide ? 9.5 : 8.5, ry = wide ? 11.5 : 10.5;
      if (dark) {
        return [
          ellipse(cx, cy, rx, ry, { fill: c.eye }),
          ellipse(ex, ey, 3.2, ry * 0.75, { fill: '#1b1b1f' }),
          circle(ex + 3, ey - 4, 2.6, { fill: '#ffffff' }),
        ];
      }
      return [ellipse(ex, ey, rx, ry, { fill: ink }), circle(ex + 3, ey - 4, wide ? 3.6 : 3.2, { fill: '#ffffff' })];
    },
  };
  const ear = (fill: string) => [
    path(EAR, { fill, stroke: fill, strokeWidth: 10, linejoin: 'round' }),
    path(EAR_INNER, { fill: innerEar, stroke: innerEar, strokeWidth: 4, linejoin: 'round' }),
  ];
  const rim = onDark && dark ? mix(c.coat, '#ffffff', 0.28) : null;

  const body: SceneNode[] = [];
  if (rim) {
    body.push(g([path(EAR, { fill: rim, stroke: rim, strokeWidth: 16, linejoin: 'round' })], { transform: earL }));
    body.push(g([path(EAR, { fill: rim, stroke: rim, strokeWidth: 16, linejoin: 'round' })], { transform: earR }));
    body.push(...headShapes({ fill: rim, stroke: rim, strokeWidth: 6 }));
  }
  body.push(
    g(ear(c.coat), { transform: earL }),
    g(ear(c.pattern === 'patch' ? c.marking : c.coat), { transform: earR }),
    ...headShapes({ fill: c.coat }),
    g(markings(c, () => ({ fill: c.marking })), { clip: `url(#${id}-head)` }),
    ellipse(100, 140, 36, 22, { fill: mix(c.coat, '#ffffff', dark ? 0.78 : 0.6) }),
    ellipse(62, 128, 10, 5.5, { fill: '#f2705f', opacity: f.state === 'happy' ? 0.5 : 0.3 }),
    ellipse(138, 128, 10, 5.5, { fill: '#f2705f', opacity: f.state === 'happy' ? 0.5 : 0.3 }),
    ...eyes(eyeKind, f, painter),
    ...mouth(mouthKind, f, dark ? '#2a211c' : ink, 3, '#f08a8f'),
    path(NOSE, { fill: '#e3787f', stroke: '#e3787f', strokeWidth: 3, linejoin: 'round' }),
    whiskers(f, 'M54 134 L28 128 M54 142 L26 145', { stroke: ink, strokeWidth: 2.5, linecap: 'round', opacity: 0.55, fill: 'none' }),
    ...extras(f, onDark ? '#f4efe6' : '#2a211c'),
  );
  return finish(body, f, floorShadow, id);
}

export function ink({ colors: c, frame: f, floorShadow, id }: StyleInput, onDark = false): StyleOutput {
  const dark = isDark(c.coat);
  const iris = mix(c.eye, '#cfd2cf', 0.45);
  const feature = dark ? '#f4efe6' : OUTLINE;
  const [earL, earR] = earTransforms(f);
  const [eyeKind, mouthKind] = FACE[f.state];
  const painter: Painter = {
    ink: feature,
    stroke: 3,
    eyeOpen: (cx, cy, fr, wide) => {
      const ex = cx + fr.lookX * 4, ey = cy + 1 + fr.lookY * 4;
      return [
        circle(cx, cy, 12, { fill: iris, stroke: OUTLINE, strokeWidth: 2.6 }),
        circle(ex, ey, wide ? 7.5 : 6.5, { fill: OUTLINE }),
        circle(ex + 3, ey - 4, 2.6, { fill: '#ffffff' }),
      ];
    },
  };
  const hatch = mix(c.coat, '#000000', 0.38);
  const markHatch = mix(c.marking, '#000000', 0.35);
  const fill = (colour: string) => ({ fill: colour, stroke: colour, strokeWidth: 2, linejoin: 'round' as const });
  const silhouette = (paint: { fill: string; stroke: string; strokeWidth: number; linejoin: 'round' }, rightEar = paint) => [
    g([path(EAR, paint)], { transform: earL }),
    g([path(EAR, rightEar)], { transform: earR }),
    ...headShapes(paint),
  ];

  const body: SceneNode[] = [];
  if (onDark) body.push(...silhouette({ fill: '#fbfaf6', stroke: '#fbfaf6', strokeWidth: 18, linejoin: 'round' }));
  body.push(
    ...silhouette({ fill: OUTLINE, stroke: OUTLINE, strokeWidth: 11, linejoin: 'round' }),
    ...silhouette(fill(c.coat), fill(c.pattern === 'patch' ? c.marking : c.coat)),
    g([path('M62 46 l4 14 M68 54 l3 10 M74 60 l2 8', { stroke: hatch, strokeWidth: 1.3, linecap: 'round', fill: 'none' })], { transform: earL }),
    g([path('M62 46 l4 14 M68 54 l3 10 M74 60 l2 8', { stroke: c.pattern === 'patch' ? markHatch : hatch, strokeWidth: 1.3, linecap: 'round', fill: 'none' })], { transform: earR }),
    g([
      ...markings(c, () => ({ fill: c.marking })),
      ...(c.pattern === 'patch'
        ? [path('M126 50 l6 10 M136 46 l5 11 M146 50 l4 11 M156 58 l3 10 M130 70 l5 9 M142 72 l4 9 M154 76 l3 9', { stroke: markHatch, strokeWidth: 1.4, linecap: 'round', fill: 'none' })]
        : []),
    ], { clip: `url(#${id}-head)` }),
    path('M92 62 l2 9 M100 60 l0 10 M108 62 l-2 9 M54 120 l-6 3 M56 128 l-7 2', { stroke: hatch, strokeWidth: 1.3, linecap: 'round', fill: 'none', opacity: 0.8 }),
    g([path('M54 120 l-6 3 M56 128 l-7 2', { stroke: hatch, strokeWidth: 1.3, linecap: 'round', fill: 'none', opacity: 0.8 })], { transform: mirror }),
    ...eyes(eyeKind, f, painter),
    ...mouth(mouthKind, f, feature, 2, '#d99a9a'),
    path(NOSE, { fill: '#c79f9c', stroke: OUTLINE, strokeWidth: 1.6, linejoin: 'round' }),
    whiskers(f, 'M60 134 Q36 128 12 130 M60 140 Q36 140 14 146 M62 146 Q40 150 20 160', {
      stroke: onDark ? '#bdbdb6' : OUTLINE, strokeWidth: 1.3, linecap: 'round', fill: 'none',
    }),
    ...extras(f, onDark ? '#f4efe6' : OUTLINE),
  );
  return finish(body, f, floorShadow, id);
}

function finish(body: SceneNode[], f: NekoFrame, floorShadow: boolean, id: string): StyleOutput {
  const defs: SceneDef[] = [{ type: 'clip', id: `${id}-head`, children: HEAD.map((h) => ellipse(h.cx, h.cy, h.rx, h.ry, {})) }];
  const nodes: SceneNode[] = [];
  if (floorShadow) nodes.push(shadow(f));
  nodes.push(g(body, { transform: bodyTransform(f) }));
  return { defs, nodes };
}
