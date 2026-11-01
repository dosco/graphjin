import type { NekoFrame, NekoState } from '../types';

/* The robot's LED face: a 15 × 7 matrix, one brightness per cell. */

export const COLS = 15;
export const ROWS = 7;

type Grid = Float32Array;

const put = (g: Grid, c: number, r: number, v: number) => {
  if (c < 0 || r < 0 || c >= COLS || r >= ROWS) return;
  const i = r * COLS + c;
  if (v > (g[i] ?? 0)) g[i] = v;
};

const sprite = (g: Grid, c: number, r: number, rows: string[], v: number) => {
  rows.forEach((row, y) => {
    for (let x = 0; x < row.length; x++) if (row[x] === '#') put(g, c + x, r + y, v);
  });
};

const noise = (x: number) => 0.5 + 0.25 * Math.sin(x * 1.7) + 0.15 * Math.sin(x * 3.1 + 1.3) + 0.1 * Math.sin(x * 5.3 + 4.1);

function eyes(g: Grid, f: NekoFrame, h = 3, dy = 0, v = 1) {
  const dx = Math.round(f.lookX * 1.4);
  const ly = Math.round(f.lookY) + dy;
  const open = Math.max(1, Math.round(h - (h - 1) * f.blink));
  const top = 1 + (3 - h) + ly + Math.floor((h - open) / 2);
  for (const c of [2, 10]) for (let y = 0; y < open; y++) for (let x = 0; x < 3; x++) put(g, c + dx + x, top + y, v);
}

const nose = (g: Grid, f: NekoFrame, v = 1) => put(g, 7 + Math.round(f.lookX * 0.6), 4, v);

const mouth = (g: Grid, f: NekoFrame, v = 1) => {
  const dx = Math.round(f.lookX * 0.6);
  sprite(g, 5 + dx, 5, ['#.#.#', '.#.#.'], v);
};

const FACES: Record<NekoState, (g: Grid, f: NekoFrame) => void> = {
  idle(g, f) {
    eyes(g, f);
    nose(g, f);
    mouth(g, f);
  },
  thinking(g, f) {
    eyes(g, f, 2);
    nose(g, f);
    for (let i = 0; i < 3; i++) {
      const p = (f.time * 1.6 - i * 0.22) % 1;
      put(g, 4 + i * 3, 6, 0.3 + 0.7 * (p < 0.35 ? Math.sin((p / 0.35) * Math.PI) : 0));
    }
  },
  searching(g, f) {
    eyes(g, f);
    nose(g, f);
    mouth(g, f, 0.5);
    const sweep = (Math.sin(f.time * 1.6 + 0.4) * 0.5 + 0.5) * (COLS + 3) - 2;
    const dir = Math.cos(f.time * 1.6 + 0.4) > 0 ? -1 : 1;
    for (let k = 0; k < 4; k++) for (let r = 0; r < ROWS; r++) put(g, Math.round(sweep + dir * k), r, 0.32 * (1 - k / 4));
  },
  listening(g, f) {
    eyes(g, f, 3, -1);
    nose(g, f);
    for (let i = 0; i < 9; i++) {
      const centre = 1 - Math.abs(i - 4) / 6;
      const h = Math.round(noise(f.time * 7 + i * 1.9 + f.seed * 10) * centre * 2.6);
      for (let k = 0; k < h; k++) put(g, 3 + i, 6 - k, 0.8 - k * 0.15);
    }
  },
  speaking(g, f) {
    eyes(g, f);
    nose(g, f);
    const env = Math.max(0, Math.sin(f.time * 2.1) * 0.5 + Math.sin(f.time * 3.7 + 1) * 0.5);
    const a = noise(f.time * 11) * (0.35 + env);
    if (a < 0.3) mouth(g, f);
    else {
      sprite(g, 5, 5, ['#####'], 1);
      if (a > 0.55) sprite(g, 6, 6, ['###'], 1);
    }
  },
  working(g, f) {
    eyes(g, f, 1);
    nose(g, f);
    const fill = Math.round(((f.time / 2.6) % 1) * 11);
    for (let i = 0; i < 11; i++) put(g, 2 + i, 6, i < fill ? 0.95 : 0.18);
  },
  happy(g, f) {
    const hop = Math.sin(f.time * 5.6) > 0 ? -1 : 0;
    sprite(g, 2, 2 + hop, ['.#.', '#.#'], 1);
    sprite(g, 10, 2 + hop, ['.#.', '#.#'], 1);
    nose(g, f);
    sprite(g, 4, 5 + hop, ['#.....#', '.#####.'], 1);
  },
  error(g, f) {
    const v = Math.sin(f.time * 7) > -0.6 ? 1 : 0.5;
    sprite(g, 2, 1, ['#.#', '.#.', '#.#'], v);
    sprite(g, 10, 1, ['#.#', '.#.', '#.#'], v);
    sprite(g, 4, 5, ['#.#.#.#', '.#.#.#.'], v * 0.85);
  },
  sleeping(g, f) {
    sprite(g, 2, 2, ['#.#', '.#.'], 0.45);
    sprite(g, 10, 2, ['#.#', '.#.'], 0.45);
    nose(g, f, 0.35);
    mouth(g, f, 0.3);
    const p = (f.time / 2.4) % 1;
    sprite(g, 12, Math.round(2 - p * 2), ['###', '.#.', '###'], Math.sin(p * Math.PI) * 0.7);
  },
};

/** The face for a frame. Right after a state change, the old face shakes into the new one. */
export function ledFace(f: NekoFrame, previous?: NekoState, switched = 10): Grid {
  const g = new Float32Array(COLS * ROWS);
  FACES[f.state](g, f);
  if (previous && previous !== f.state && switched < 0.25) {
    const old = new Float32Array(COLS * ROWS);
    FACES[previous](old, f);
    const k = switched / 0.25;
    for (let i = 0; i < g.length; i++) g[i] = (g[i] ?? 0) * k + (old[i] ?? 0) * (1 - k);
  }
  return g;
}

/** The light in the robot's ears, per state, at time t. */
export const EAR_LIGHT: Record<NekoState, { colour: string | null; pulse: (t: number) => number }> = {
  idle: { colour: null, pulse: (t) => 0.75 + 0.1 * Math.sin(t * 1.4) },
  thinking: { colour: '#ffb547', pulse: (t) => 0.45 + 0.55 * (0.5 + 0.5 * Math.sin(t * 4.2)) },
  searching: { colour: '#5adcff', pulse: (t) => ((t * 2.4) % 1 < 0.18 ? 1 : 0.3) },
  listening: { colour: '#60ff96', pulse: () => 0.95 },
  speaking: { colour: null, pulse: (t) => 0.55 + 0.45 * noise(t * 11) },
  working: { colour: '#6ea0ff', pulse: (t) => ((t * 1.7) % 1 < 0.5 ? 1 : 0.4) },
  happy: { colour: '#ff82c8', pulse: (t) => 0.8 + 0.2 * Math.sin(t * 8) },
  error: { colour: '#ff4a40', pulse: (t) => ((t * 3) % 1 < 0.5 ? 1 : 0.2) },
  sleeping: { colour: null, pulse: () => 0 },
};
