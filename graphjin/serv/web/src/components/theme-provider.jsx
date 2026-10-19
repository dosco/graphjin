import * as React from "react";

const ThemeContext = React.createContext({ theme: "system", resolvedTheme: "light", setTheme: () => {} });
const storageKey = "graphjin-console-theme";

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
  document.body.classList.toggle("graphiql-dark", resolved === "dark");
  document.body.classList.toggle("graphiql-light", resolved !== "dark");
}

export function ThemeProvider({ children, defaultTheme = "system" }) {
  const [theme, setTheme] = React.useState(() => localStorage.getItem(storageKey) || defaultTheme);
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
