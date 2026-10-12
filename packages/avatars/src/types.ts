/** What the bot is doing. Each state has its own face and motion. */
export type NekoState =
  | 'idle'
  | 'thinking'
  | 'searching'
  | 'listening'
  | 'speaking'
  | 'working'
  | 'happy'
  | 'error'
  | 'sleeping';

/** How the neko is drawn. A chat picks one style for all of its bots. */
export type NekoStyle = 'robot' | 'flat' | 'ink';

/** The marking on the coat. */
export type NekoPattern = 'solid' | 'patch' | 'tabby';

/** The colours of one neko. */
export interface NekoColors {
  /** The main coat colour. */
  coat: string;
  pattern: NekoPattern;
  /** The colour of the patch or the tabby stripes. */
  marking: string;
  /** The LED face of the robot style. */
  led: string;
  /** The eye colour of the flat and ink styles. */
  eye: string;
}

/** One animation frame. The motion sim makes these; the styles draw them. */
export interface NekoFrame {
  state: NekoState;
  /** Seconds since the state started. */
  time: number;
  /** 0 open, 1 shut. */
  blink: number;
  /** Where the eyes look, -1 to 1. */
  lookX: number;
  lookY: number;
  /** Body offsets in view units (the view is 200 × 200). */
  bob: number;
  hop: number;
  /** Head tilt in degrees. */
  tilt: number;
  /** Positive squashes the body, negative stretches it. */
  squash: number;
  /** Ear rotation in degrees. Positive folds the ear out and down. */
  earL: number;
  earR: number;
  /** Whisker droop in degrees. */
  whisker: number;
  /** Side the thinking pose leans to, 1 or -1. */
  side: number;
  seed: number;
}
