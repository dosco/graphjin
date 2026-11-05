import * as React from "react";

const ThemeContext = React.createContext({ theme: "dark", resolvedTheme: "dark", setTheme: () => {} });
const storageKey = "graphjin-console-appearance";
// The old key stored "system" on every first visit, so only an explicit
// light or dark choice carries over; everyone else starts on dark.
const legacyStorageKey = "graphjin-console-theme";

function storedTheme() {
  const current = localStorage.getItem(storageKey);
  if (current) {
    return current;
  }
  const legacy = localStorage.getItem(legacyStorageKey);
  return legacy === "light" || legacy === "dark" ? legacy : null;
}

function preferredTheme() {
  if (typeof window === "undefined") {
    return "light";
  }
  return window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
}

function applyTheme(theme) {
  const root = document.documentElement;
  const resolved = theme === "system" ? preferredTheme() : theme;
  root.classList.remove("light", "dark");
  root.classList.add(resolved);
  root.style.colorScheme = resolved;
  document.querySelector('meta[name="theme-color"]')?.setAttribute("content", resolved === "dark" ? "#000000" : "#ffffff");
  document.body.classList.toggle("graphiql-dark", resolved === "dark");
  document.body.classList.toggle("graphiql-light", resolved !== "dark");
}

export function ThemeProvider({ children, defaultTheme = "dark" }) {
  const [theme, setTheme] = React.useState(() => storedTheme() || defaultTheme);
  const [resolvedTheme, setResolvedTheme] = React.useState(() => theme === "system" ? preferredTheme() : theme);

  React.useEffect(() => {
    const syncTheme = () => {
      const resolved = theme === "system" ? preferredTheme() : theme;
      applyTheme(theme);
      setResolvedTheme(resolved);
    };
    syncTheme();
    localStorage.setItem(storageKey, theme);
    if (theme !== "system") {
      return undefined;
    }
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const handleChange = syncTheme;
    media.addEventListener("change", handleChange);
    return () => media.removeEventListener("change", handleChange);
  }, [theme]);

  const value = React.useMemo(() => ({ theme, resolvedTheme, setTheme }), [theme, resolvedTheme]);
  return <ThemeContext.Provider value={value}>{children}</ThemeContext.Provider>;
}

export function useTheme() {
  return React.useContext(ThemeContext);
}
