import * as React from "react";

const ConsoleContext = React.createContext({ bootstrap: null, isLoading: true });

export function ConsoleProvider({ value, children }) {
  return <ConsoleContext.Provider value={value}>{children}</ConsoleContext.Provider>;
}

export function useConsole() {
  return React.useContext(ConsoleContext);
}
