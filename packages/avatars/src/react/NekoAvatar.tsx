import { createElement, useEffect, useState, type CSSProperties, type HTMLAttributes, type MouseEvent, type ReactNode } from 'react';
import { renderScene, type SvgComponents } from '../index';
import { useNeko, type NekoOptions } from './useNeko';

const TAGS: SvgComponents = {
  Svg: 'svg', G: 'g', Path: 'path', Circle: 'circle', Ellipse: 'ellipse', Defs: 'defs',
  ClipPath: 'clipPath', RadialGradient: 'radialGradient', LinearGradient: 'linearGradient', Stop: 'stop',
};

function usePrefersReducedMotion() {
  const query = '(prefers-reduced-motion: reduce)';
  const [reduced, setReduced] = useState(() => typeof matchMedia === 'function' && matchMedia(query).matches);
  useEffect(() => {
    if (typeof matchMedia !== 'function') return;
    const mq = matchMedia(query);
    const on = () => setReduced(mq.matches);
    mq.addEventListener('change', on);
    return () => mq.removeEventListener('change', on);
  }, []);
  return reduced;
}

export interface NekoAvatarProps extends NekoOptions, Omit<HTMLAttributes<HTMLSpanElement>, 'color'> {
  /** Width and height in px. Default `48`. */
  size?: number;
  /** Shown over the bottom-right corner, such as a star for the main bot. */
  badge?: ReactNode;
  style?: CSSProperties;
}

/** A neko avatar for a bot. Inside a NekoProvider it takes the chat's style and the bot's assigned colours. */
export function NekoAvatar({
  seed, nekoStyle, state, colors, animated, speed, interactive, floorShadow, onDark,
  size = 48, badge, style, onClick, 'aria-label': ariaLabel, ...rest
}: NekoAvatarProps) {
  const reduced = usePrefersReducedMotion();
  const neko = useNeko({ seed, nekoStyle, state, colors, animated, speed, interactive, floorShadow, onDark }, reduced);
  const click = (e: MouseEvent<HTMLSpanElement>) => {
    neko.poke();
    onClick?.(e);
  };
  return (
    <span
      role="img"
      aria-label={ariaLabel ?? `Neko, ${neko.state}`}
      onClick={click}
      style={{
        position: 'relative', display: 'inline-block', flex: 'none', width: size, height: size, lineHeight: 0,
        cursor: neko.interactive ? 'pointer' : undefined, ...style,
      }}
      {...rest}
    >
      {renderScene(neko.scene, createElement, TAGS, { width: size, height: size, 'aria-hidden': true, style: { display: 'block', overflow: 'visible' } })}
      {badge ? <span style={{ position: 'absolute', right: 0, bottom: 0, lineHeight: 0 }}>{badge}</span> : null}
    </span>
  );
}

export interface NekoGroupProps extends Omit<NekoAvatarProps, 'seed' | 'badge'> {
  /** The bots in a group chat. The first two are drawn, overlapped. */
  seeds: string[];
}

/** Two nekos, overlapped: the avatar of a group chat. */
export function NekoGroup({ seeds, size = 48, style, ...rest }: NekoGroupProps) {
  const small = Math.round(size * 0.66);
  const [a, b] = seeds;
  return (
    <span role="img" aria-label={rest['aria-label'] ?? 'Group of nekos'} style={{ position: 'relative', display: 'inline-block', flex: 'none', width: size, height: size, ...style }}>
      <NekoAvatar {...rest} seed={a} size={small} aria-hidden style={{ position: 'absolute', left: 0, top: 0 }} />
      {b !== undefined ? <NekoAvatar {...rest} seed={b} size={small} aria-hidden style={{ position: 'absolute', right: 0, bottom: 0 }} /> : null}
    </span>
  );
}
