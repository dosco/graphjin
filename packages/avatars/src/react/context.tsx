import { createContext, useContext, useMemo, type ReactNode } from 'react';
import { assignNekoColors, type NekoColors, type NekoStyle } from '../index';

export interface NekoChat {
  style?: NekoStyle;
  onDark?: boolean;
  colors?: Record<string, NekoColors>;
}

const NekoContext = createContext<NekoChat>({});

export interface NekoProviderProps {
  /** The style the chat's owner picked. Every avatar inside uses it unless it sets its own. */
  nekoStyle?: NekoStyle;
  /** The ids of the bots in the chat. Each gets its own coat colour. */
  bots?: string[];
  /** Change it to reshuffle the colours of this chat. */
  salt?: string;
  /** The chat has a dark background. */
  onDark?: boolean;
  children?: ReactNode;
}

/** Wrap a chat: its avatars share one style, and its bots get coats that differ from each other. */
export function NekoProvider({ nekoStyle, bots, salt, onDark, children }: NekoProviderProps) {
  const key = bots?.join('\u0000') ?? '';
  const colors = useMemo(() => (bots?.length ? assignNekoColors(bots, salt) : undefined), [key, salt]);
  const value = useMemo(() => ({ style: nekoStyle, onDark, colors }), [nekoStyle, onDark, colors]);
  return <NekoContext.Provider value={value}>{children}</NekoContext.Provider>;
}

export const useNekoChat = () => useContext(NekoContext);
