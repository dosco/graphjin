import { useEffect, useId, useMemo, useReducer, useRef } from 'react';
import {
  NekoMotion,
  buildNeko,
  motionSeed,
  nekoColors,
  nekoStates,
  restFrame,
  subscribe,
  type NekoColors,
  type NekoState,
  type NekoStyle,
  type Scene,
} from '../index';
import { useNekoChat } from './context';

/** The options every neko avatar takes. */
export interface NekoOptions {
  /** The bot's id or name. It picks the colours, so keep it stable. Leave it out for the OpenNeko look. */
  seed?: string;
  /** `robot`, `flat` or `ink`. Default: the chat's style, else `robot`. */
  nekoStyle?: NekoStyle;
  /** What the bot is doing. Default `idle`. */
  state?: NekoState;
  /** Override any of the colours the seed picks. */
  colors?: Partial<NekoColors>;
  /** Animate. Default `true`. Off, the still face of the state is drawn. */
  animated?: boolean;
  /** Multiplier on the animation speed. Default `1`. */
  speed?: number;
  /** Hop and smile when tapped. Default `true`. */
  interactive?: boolean;
  /** A soft shadow under the neko. Default `false`. */
  floorShadow?: boolean;
  /** Draw for a dark background. Default: the chat's setting. */
  onDark?: boolean;
}

/** Everything a renderer needs: the scene for this frame and a poke handler. React Native renders the same scene with react-native-svg. */
export function useNeko(o: NekoOptions, reducedMotion = false) {
  const chat = useNekoChat();
  const state: NekoState = o.state && nekoStates.includes(o.state) ? o.state : 'idle';
  const style = o.nekoStyle ?? chat.style ?? 'robot';
  const onDark = o.onDark ?? chat.onDark ?? false;
  const rawId = useId();
  const id = `neko${rawId.replace(/[^a-zA-Z0-9_-]/g, '')}`;

  const assigned = o.seed !== undefined ? chat.colors?.[o.seed] : undefined;
  const c = o.colors;
  const colors = useMemo(
    () => ({ ...(assigned ?? nekoColors(o.seed)), ...c }),
    [assigned, o.seed, c?.coat, c?.pattern, c?.marking, c?.led, c?.eye],
  );

  const motion = useRef<NekoMotion | null>(null);
  if (!motion.current) motion.current = new NekoMotion(motionSeed(o.seed ?? rawId), state);
  motion.current.setState(state);

  const speed = o.speed ?? 1;
  const speedRef = useRef(speed);
  speedRef.current = speed;
  const still = o.animated === false || reducedMotion || !(speed > 0);
  const [, tick] = useReducer((x: number) => x + 1, 0);

  useEffect(() => {
    if (still) return;
    return subscribe((dt) => {
      motion.current!.update(dt * speedRef.current);
      tick();
    });
  }, [still]);

  const m = motion.current;
  const scene: Scene = buildNeko({
    style,
    colors,
    frame: still ? restFrame(state) : m.frame,
    id,
    previous: still ? undefined : m.previous,
    switched: m.switched,
    floorShadow: o.floorShadow,
    onDark,
  });

  const interactive = o.interactive ?? true;
  const poke = () => {
    if (interactive && !still) m.poke();
  };
  return { scene, poke, state, style, colors, interactive };
}
