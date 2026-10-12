import { isDark, mix } from '../color';
import { dots, ellipse, g, path, type SceneDef, type SceneNode } from '../scene';
import { EAR, EAR_INNER, HEAD, bodyTransform, earTransforms, headShapes, markings, shadow, whiskers, type StyleInput, type StyleOutput } from './anatomy';
import { COLS, EAR_LIGHT, ROWS, ledFace } from './led';

const WHISKERS = 'M50 146 L18 140 M50 152 L16 154';

export function robot({ colors: c, frame: f, id, previous, switched, floorShadow }: StyleInput): StyleOutput {
  const dark = isDark(c.coat);
  const shell = `${id}-shell`, mark = `${id}-mark`, glass = `${id}-glass`, spec = `${id}-spec`, clip = `${id}-head`;
  const defs: SceneDef[] = [
    {
      type: 'radial', id: shell, cx: 72, cy: 62, r: 150,
      stops: [
        { offset: 0, color: mix(c.coat, '#ffffff', dark ? 0.35 : 0.7) },
        { offset: 0.5, color: c.coat },
        { offset: 1, color: mix(c.coat, '#000000', 0.38) },
      ],
    },
    {
      type: 'radial', id: mark, cx: 140, cy: 50, r: 100,
      stops: [
        { offset: 0, color: mix(c.marking, '#ffffff', 0.25) },
        { offset: 1, color: mix(c.marking, '#000000', 0.3) },
      ],
    },
    {
      type: 'radial', id: spec, cx: 66, cy: 66, r: 26,
      stops: [
        { offset: 0, color: '#ffffff', opacity: 0.85 },
        { offset: 1, color: '#ffffff', opacity: 0 },
      ],
    },
    {
      type: 'linear', id: glass, x1: 0, y1: 84, x2: 0, y2: 140,
      stops: [
        { offset: 0, color: '#1d2523' },
        { offset: 1, color: '#070908' },
      ],
    },
    { type: 'clip', id: clip, children: HEAD.map((h) => ellipse(h.cx, h.cy, h.rx, h.ry, {})) },
  ];

  const light = EAR_LIGHT[f.state];
  const lightColour = light.colour ?? c.led;
  const pulse = light.pulse(f.time);
  const [earL, earR] = earTransforms(f);
  const ear = (fill: string) => [
    path(EAR, { fill, stroke: fill, strokeWidth: 10, linejoin: 'round' }),
    path(EAR_INNER, { fill: '#11160f', stroke: '#11160f', strokeWidth: 4, linejoin: 'round' }),
    path(EAR_INNER, { fill: lightColour, opacity: 0.15 + 0.8 * pulse, stroke: lightColour, strokeWidth: 2, linejoin: 'round' }),
  ];

  const led = f.state === 'error' ? '#ff5a4f' : c.led;
  const grid = ledFace(f, previous, switched);
  const lit: [[number, number][], [number, number][], [number, number][]] = [[], [], []];
  const glow: [number, number][] = [];
  for (let r = 0; r < ROWS; r++) {
    for (let col = 0; col < COLS; col++) {
      const v = grid[r * COLS + col] ?? 0;
      if (v < 0.05) continue;
      const p: [number, number] = [58 + col * 6, 93 + r * 6];
      lit[v > 0.7 ? 2 : v > 0.4 ? 1 : 0].push(p);
      if (v > 0.45) glow.push(p);
    }
  }
  const off: [number, number][] = [];
  for (let r = 0; r < ROWS; r++) for (let col = 0; col < COLS; col++) off.push([58 + col * 6, 93 + r * 6]);

  const body: SceneNode[] = [
    g(ear(`url(#${shell})`), { transform: earL }),
    g(ear(c.pattern === 'patch' ? `url(#${mark})` : `url(#${shell})`), { transform: earR }),
    ...headShapes({ fill: `url(#${shell})` }),
    g([...markings(c, () => ({ fill: c.pattern === 'patch' ? `url(#${mark})` : c.marking, opacity: 0.9 })),
      ellipse(66, 66, 24, 12, { fill: `url(#${spec})` }, 'rotate(-24 66 66)')], { clip: `url(#${clip})` }),
    whiskers(f, WHISKERS, { stroke: dark ? mix(c.coat, '#ffffff', 0.5) : mix(c.coat, '#000000', 0.42), strokeWidth: 2.4, linecap: 'round', fill: 'none' }),
    path('M74 84 H126 A24 24 0 0 1 150 108 V116 A24 24 0 0 1 126 140 H74 A24 24 0 0 1 50 116 V108 A24 24 0 0 1 74 84 Z', {
      fill: `url(#${glass})`, stroke: mix(c.coat, '#000000', 0.42), strokeWidth: 3,
    }),
    path(dots(off, 2.1), { fill: led, opacity: 0.07 }),
    path(dots(glow, 4.4), { fill: led, opacity: 0.2 }),
    ...lit.flatMap((pts, i) => (pts.length ? [path(dots(pts, 2.3), { fill: led, opacity: 0.35 + 0.325 * i })] : [])),
    path('M62 92 C74 88 104 88 120 89', { stroke: '#ffffff', strokeWidth: 3, linecap: 'round', opacity: 0.14, fill: 'none' }),
  ];

  const nodes: SceneNode[] = [];
  if (floorShadow) nodes.push(shadow(f));
  nodes.push(g(body, { transform: bodyTransform(f) }));
  return { defs, nodes };
}
