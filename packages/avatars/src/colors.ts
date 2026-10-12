import type { NekoColors, NekoPattern } from './types';
import { isDark, mix } from './color';

/** FNV-1a: a stable 32-bit hash of a string. */
export function hash(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) h = Math.imul(h ^ s.charCodeAt(i), 16777619);
  return h >>> 0;
}

export interface Coat {
  name: string;
  coat: string;
  /** Marking colour for a patch or stripes on this coat. */
  marking: string;
}

/** Coat colours, chosen to stay apart from each other in a chat. */
export const nekoCoats: Coat[] = [
  { name: 'snow', coat: '#f4f1ea', marking: '#9a9ca2' },
  { name: 'ginger', coat: '#ee9c55', marking: '#c66a2c' },
  { name: 'smoke', coat: '#9aa1aa', marking: '#5f6670' },
  { name: 'ink', coat: '#34343b', marking: '#f2efe8' },
  { name: 'cream', coat: '#f1d7a6', marking: '#c9965a' },
  { name: 'cocoa', coat: '#8a5a3c', marking: '#4f3121' },
  { name: 'mint', coat: '#9fdcc0', marking: '#4fa883' },
  { name: 'lilac', coat: '#c7b4e8', marking: '#8a6fc0' },
  { name: 'sky', coat: '#9cc9ef', marking: '#4f8fc7' },
  { name: 'rose', coat: '#f2a8b8', marking: '#cf6a83' },
  { name: 'lemon', coat: '#f3dc7a', marking: '#c9a42f' },
  { name: 'slate', coat: '#5f6b78', marking: '#2f3842' },
];

export const nekoLeds = ['#6cff7f', '#7fe3ff', '#ffcf70', '#ff9fd8', '#e9f1ff'];
export const nekoEyes = ['#f2c94c', '#7bc96f', '#6fb3e8', '#e8a24f'];
const patterns: NekoPattern[] = ['solid', 'patch', 'tabby'];

/** The OpenNeko look: a white coat with a grey patch, after the brand mark. */
export const defaultNekoColors: NekoColors = {
  coat: '#f4f1ea',
  pattern: 'patch',
  marking: '#9a9ca2',
  led: '#6cff7f',
  eye: '#f2c94c',
};

export const nekoPatterns: NekoPattern[] = patterns;

function fromHash(h: number, coatIndex = h % nekoCoats.length, chosen?: NekoPattern): NekoColors {
  const pick = <T>(list: T[], i: number): T => list[i % list.length] as T;
  const c = pick(nekoCoats, coatIndex);
  const pattern = chosen ?? pick(patterns, h >>> 8);
  return {
    coat: c.coat,
    pattern,
    marking: pattern === 'tabby' && !isDark(c.coat) ? mix(c.coat, '#000000', 0.32) : c.marking,
    led: pick(nekoLeds, h >>> 12),
    eye: pick(nekoEyes, h >>> 16),
  };
}

/** The colours for one bot. The same seed always gives the same neko. */
export function nekoColors(seed?: string | number, overrides?: Partial<NekoColors>): NekoColors {
  const base = seed === undefined || seed === '' ? defaultNekoColors : fromHash(hash(String(seed)));
  return { ...base, ...overrides };
}

/** The colours for a bot whose owner chose its coat and pattern. The seed sets the rest. */
export function nekoColorsFor(seed: string, coat: string, pattern: NekoPattern): NekoColors {
  const index = nekoCoats.findIndex((c) => c.name === coat);
  const h = hash(seed);
  return fromHash(h, index >= 0 ? index : undefined, pattern);
}

/**
 * Colours for every bot in one chat, with no two bots on the same coat
 * while coats remain. Each bot keeps its own coat when it is free; a bot
 * that collides takes the next free coat. Pass `salt` to reshuffle a chat.
 */
export function assignNekoColors(seeds: string[], salt = ''): Record<string, NekoColors> {
  const out: Record<string, NekoColors> = {};
  const used = new Set<number>();
  const unique = [...new Set(seeds)].sort((a, b) => hash(salt + a) - hash(salt + b));
  for (const seed of unique) {
    const h = hash(salt + seed);
    let coat = h % nekoCoats.length;
    if (used.size < nekoCoats.length) {
      while (used.has(coat)) coat = (coat + 1) % nekoCoats.length;
    }
    used.add(coat);
    out[seed] = fromHash(h, coat);
  }
  return out;
}
