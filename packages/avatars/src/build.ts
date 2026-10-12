import type { NekoColors, NekoFrame, NekoState, NekoStyle } from './types';
import { VIEWBOX, type Scene } from './scene';
import { restFrame } from './motion';
import { hash, nekoColors } from './colors';
import { robot } from './styles/robot';
import { flat, ink } from './styles/drawn';

export const nekoStyles: NekoStyle[] = ['robot', 'flat', 'ink'];

export const nekoStates: NekoState[] = ['idle', 'thinking', 'searching', 'listening', 'speaking', 'working', 'happy', 'error', 'sleeping'];

export interface BuildOptions {
  style?: NekoStyle;
  colors?: NekoColors;
  /** A frame from NekoMotion. Without one, the still pose of `state` is drawn. */
  frame?: NekoFrame;
  state?: NekoState;
  /** Prefix for gradient and clip ids. Give each avatar on a page its own. */
  id?: string;
  /** The state before the last change, for the cross-fade on the robot face. */
  previous?: NekoState;
  switched?: number;
  floorShadow?: boolean;
  /** Draw for a dark background: the ink style gets a paper rim, dark flat coats a light one. */
  onDark?: boolean;
}

/** Draw one neko as a renderer-neutral scene. */
export function buildNeko(o: BuildOptions = {}): Scene {
  const style = o.style && nekoStyles.includes(o.style) ? o.style : 'robot';
  const input = {
    colors: o.colors ?? nekoColors(),
    frame: o.frame ?? restFrame(o.state ?? 'idle'),
    id: o.id ?? 'neko',
    previous: o.previous,
    switched: o.switched,
    floorShadow: o.floorShadow ?? false,
  };
  const out = style === 'flat' ? flat(input, o.onDark) : style === 'ink' ? ink(input, o.onDark) : robot(input);
  return { viewBox: VIEWBOX, ...out };
}

/** A 0 to 1 motion seed from any seed, so a row of bots does not blink in step. */
export const motionSeed = (seed: string | number) => (hash(String(seed)) % 100000) / 100000;
