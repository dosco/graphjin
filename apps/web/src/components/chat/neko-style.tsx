"use client";

import { createContext, useContext, useEffect, useState } from "react";
import { NekoProvider, type NekoStyle } from "@neko/avatars/react";

const KEY = "neko-style";
const STYLES: NekoStyle[] = ["robot", "flat", "ink"];

const NekoStyleContext = createContext<{
  nekoStyle: NekoStyle;
  setNekoStyle: (style: NekoStyle) => void;
}>({ nekoStyle: "robot", setNekoStyle: () => {} });

export function NekoStyleProvider({ children }: { children: React.ReactNode }) {
  const [nekoStyle, setStyle] = useState<NekoStyle>("robot");

  useEffect(() => {
    const id = window.setTimeout(() => {
      try {
        const saved = localStorage.getItem(KEY) as NekoStyle | null;
        if (saved && STYLES.includes(saved)) setStyle(saved);
      } catch {}
    }, 0);
    return () => window.clearTimeout(id);
  }, []);

  function setNekoStyle(style: NekoStyle) {
    setStyle(style);
    try {
      localStorage.setItem(KEY, style);
    } catch {}
  }

  return (
    <NekoStyleContext.Provider value={{ nekoStyle, setNekoStyle }}>
      <NekoProvider nekoStyle={nekoStyle}>{children}</NekoProvider>
    </NekoStyleContext.Provider>
  );
}

export const useNekoStyle = () => useContext(NekoStyleContext);
