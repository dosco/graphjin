import { describe, expect, it } from 'vitest';
import { NekoMotion, assignNekoColors, buildNeko, nekoCoats, nekoColors, nekoStates, nekoStyles, toSvg } from '../src/index';

describe('neko colours', () => {
  it('gives a seed the same colours every time', () => {
    expect(nekoColors('tax-bot')).toEqual(nekoColors('tax-bot'));
    expect(nekoColors('tax-bot')).not.toEqual(nekoColors('bdr-bot'));
  });

  it('gives bots in one chat different coats while coats remain', () => {
    const bots = Array.from({ length: nekoCoats.length }, (_, i) => `bot-${i}`);
    const coats = Object.values(assignNekoColors(bots)).map((c) => c.coat);
    expect(new Set(coats).size).toBe(bots.length);
  });

  it('keeps a bot on its own colours when its coat is free', () => {
    expect(assignNekoColors(['tax-bot'])['tax-bot']).toEqual(nekoColors('tax-bot'));
  });
});

describe('neko drawing', () => {
  it('draws every state in every style as SVG', () => {
    const motion = new NekoMotion(0.3, 'idle');
    for (const style of nekoStyles) {
      for (const state of nekoStates) {
        motion.setState(state);
        motion.update(0.5);
        for (const frame of [undefined, motion.frame]) {
          const svg = toSvg(buildNeko({ style, state, frame, colors: nekoColors(state), onDark: true }), 64);
          expect(svg).toMatch(/^<svg [^>]*xmlns="http:\/\/www.w3.org\/2000\/svg"/);
          expect(svg).not.toMatch(/NaN|undefined/);
        }
      }
    }
  });
});
