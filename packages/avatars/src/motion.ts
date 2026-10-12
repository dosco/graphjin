import type { NekoFrame, NekoState } from './types';

class Spring {
  v = 0;
  constructor(public x = 0) {}
  step(target: number, dt: number, freq: number, damping: number) {
    const w = freq * Math.PI * 2;
    this.v += (w * w * (target - this.x) - 2 * damping * w * this.v) * dt;
    this.x += this.v * dt;
  }
}

export function rng(seed: number) {
  let a = Math.floor(seed * 4294967295) >>> 0 || 1;
  return () => {
    a = (a + 0x6d2b79f5) >>> 0;
    let t = a;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

const clamp = (v: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, v));

interface Pose {
  tilt: number;
  ear: number;
  whisker: number;
}

const POSES: Record<NekoState, Pose> = {
  idle: { tilt: 0, ear: 0, whisker: 0 },
  thinking: { tilt: 6, ear: 4, whisker: 0 },
  searching: { tilt: 0, ear: -6, whisker: -4 },
  listening: { tilt: 8, ear: -10, whisker: -4 },
  speaking: { tilt: 0, ear: 0, whisker: 0 },
  working: { tilt: -3, ear: 6, whisker: 2 },
  happy: { tilt: 0, ear: -6, whisker: -8 },
  error: { tilt: -2, ear: 38, whisker: 10 },
  sleeping: { tilt: 8, ear: 18, whisker: 14 },
};

/** The still frame of a state, for paused avatars and reduced motion. */
export function restFrame(state: NekoState, seed = 0): NekoFrame {
  const p = POSES[state] ?? POSES.idle;
  const look: Partial<Record<NekoState, [number, number]>> = {
    thinking: [0.8, -0.8],
    searching: [0.7, 0],
    working: [0, 0.8],
  };
  const [lookX, lookY] = look[state] ?? [0, 0];
  return {
    state, time: 0.4, blink: 0, lookX, lookY, bob: 0, hop: 0, tilt: p.tilt, squash: 0,
    earL: p.ear, earR: p.ear, whisker: p.whisker, side: 1, seed,
  };
}

/** The neko's motion: springs chase each state's pose, with blinks, glances, ear twitches and hops on top. */
export class NekoMotion {
  state: NekoState;
  shown: NekoState;
  /** The shown state before the last change. */
  previous: NekoState;
  /** Seconds since the shown state changed. */
  switched = 10;
  frame: NekoFrame;

  private time = 0;
  private since = 0;
  private random: () => number;
  private tilt = new Spring();
  private earL = new Spring();
  private earR = new Spring();
  private whisker = new Spring();
  private squash = new Spring();
  private lookX = new Spring();
  private lookY = new Spring();
  private glance = { x: 0, y: 0 };
  private nextGlance = 0.8;
  private nextBlink: number;
  private blinkAge = -1;
  private nextTwitch: number;
  private nextHop: number;
  private nextSide = 4;
  private side = 1;
  private hopAge = -1;
  private hopHeight = 0;
  private reactUntil = -1;

  constructor(private seed: number, state: NekoState = 'idle') {
    this.random = rng(seed);
    this.state = this.shown = this.previous = state;
    this.nextBlink = 1 + this.random() * 3;
    this.nextTwitch = 2 + this.random() * 4;
    this.nextHop = 7 + this.random() * 6;
    this.frame = restFrame(state, seed);
  }

  setState(s: NekoState) {
    if (s === this.state) return;
    this.state = s;
    this.since = 0;
    this.nextHop = this.time + (s === 'happy' ? 0.15 : 4 + this.random() * 4);
  }

  /** A tap: a hop and a happy face. */
  poke() {
    this.hop(16);
    this.reactUntil = this.time + 1.4;
  }

  private hop(height: number) {
    if (this.hopAge >= 0) return;
    this.hopAge = 0;
    this.hopHeight = height;
  }

  update(dt: number) {
    if (!(dt > 0)) return;
    const steps = Math.max(1, Math.ceil(dt / (1 / 120)));
    for (let i = 0; i < steps; i++) this.step(dt / steps);
  }

  private step(dt: number) {
    this.time += dt;
    this.since += dt;
    this.switched += dt;
    const t = this.time;
    const rand = this.random;
    const s = this.state;

    const shown: NekoState = t < this.reactUntil ? 'happy' : s;
    if (shown !== this.shown) {
      this.previous = this.shown;
      this.shown = shown;
      this.switched = 0;
    }

    const pose = POSES[shown];
    let tilt = pose.tilt;
    let earL = pose.ear, earR = pose.ear;
    let lookX = 0, lookY = 0, bob = Math.sin(t * 1.9) * 1.2;

    if (t > this.nextGlance) {
      this.glance = { x: (rand() - 0.5) * 1.8, y: rand() < 0.3 ? -0.6 : rand() < 0.2 ? 0.6 : 0 };
      this.nextGlance = t + 1.4 + rand() * 2.6;
    }

    switch (shown) {
      case 'idle':
        lookX = this.glance.x;
        lookY = this.glance.y;
        tilt = this.glance.x * 3;
        if (t > this.nextHop) {
          this.hop(10 + rand() * 4);
          this.nextHop = t + 8 + rand() * 7;
        }
        break;
      case 'thinking':
        if (t > this.nextSide) {
          this.side = -this.side;
          this.nextSide = t + 3 + rand() * 2;
        }
        lookX = 0.8 * this.side;
        lookY = -0.8;
        tilt = pose.tilt * this.side;
        if (this.side > 0) earR += 8;
        else earL += 8;
        break;
      case 'searching': {
        const sweep = Math.sin(t * 1.6);
        lookX = sweep;
        tilt = sweep * 4;
        earL += sweep * 10;
        earR -= sweep * 10;
        break;
      }
      case 'listening':
        bob += Math.max(0, Math.sin(t * 2.4)) * 1.5;
        lookY = -0.3;
        break;
      case 'speaking':
        bob += Math.abs(Math.sin(t * 6.3)) * 1.6;
        tilt = Math.sin(t * 0.9) * 3;
        lookX = this.glance.x * 0.4;
        break;
      case 'working':
        bob += Math.abs(Math.sin(t * 7)) * 1.1;
        lookY = 0.8;
        lookX = Math.sin(t * 0.8) * 0.3;
        break;
      case 'happy':
        tilt = Math.sin(t * 4.4) * 6;
        if (t > this.nextHop) {
          this.hop(12);
          this.nextHop = t + 1.1 + rand() * 0.4;
        }
        break;
      case 'error': {
        const c = (t % 2.4) / 0.7;
        tilt += c < 1 ? Math.sin(c * Math.PI * 7) * 9 * (1 - c) : 0;
        break;
      }
      case 'sleeping':
        bob = Math.sin(t * 1.15) * 2.2;
        break;
    }

    if (t > this.nextTwitch) {
      const kick = 260 + rand() * 160;
      if (rand() < 0.5) this.earL.v += kick;
      else this.earR.v += kick;
      if (rand() < 0.4) this.whisker.v -= 120;
      this.nextTwitch = t + (shown === 'listening' ? 1 + rand() * 1.6 : shown === 'sleeping' ? 6 + rand() * 6 : 3 + rand() * 5);
    }

    const slow = shown === 'sleeping' ? 0.5 : 1;
    this.tilt.step(tilt, dt, 1.4 * slow, 0.6);
    this.earL.step(earL, dt, 3.2, 0.32);
    this.earR.step(earR, dt, 3.2, 0.32);
    this.whisker.step(pose.whisker, dt, 2, 0.4);
    this.lookX.step(lookX, dt, 4, 0.8);
    this.lookY.step(lookY, dt, 4, 0.8);

    let hop = 0;
    let squashTarget = 0;
    if (this.hopAge >= 0) {
      this.hopAge += dt;
      const crouch = 0.1, air = 0.42;
      if (this.hopAge < crouch) squashTarget = 0.12;
      else {
        const p = (this.hopAge - crouch) / air;
        if (p < 1) {
          hop = 4 * this.hopHeight * p * (1 - p);
          squashTarget = p < 0.5 ? -0.06 : 0;
        } else {
          this.squash.v += 2.4;
          this.earL.v += 120;
          this.earR.v += 120;
          this.hopAge = -1;
        }
      }
    }
    this.squash.step(squashTarget, dt, 3.2, 0.35);

    if (t > this.nextBlink) {
      this.blinkAge = 0;
      this.nextBlink = t + 2.2 + rand() * 3.5;
      if (rand() < 0.2) this.nextBlink = t + 0.35;
    }
    let blink = 0;
    if (this.blinkAge >= 0) {
      this.blinkAge += dt;
      blink = Math.sin(clamp(this.blinkAge / 0.17, 0, 1) * Math.PI);
      if (this.blinkAge > 0.17) this.blinkAge = -1;
    }

    this.frame = {
      state: shown,
      time: shown === s ? this.since : t - (this.reactUntil - 1.4),
      blink: shown === 'sleeping' ? 0 : blink,
      lookX: this.lookX.x,
      lookY: this.lookY.x,
      bob,
      hop,
      tilt: this.tilt.x,
      squash: clamp(this.squash.x, -0.2, 0.25),
      earL: clamp(this.earL.x, -25, 55),
      earR: clamp(this.earR.x, -25, 55),
      whisker: this.whisker.x,
      side: this.side,
      seed: this.seed,
    };
  }
}
