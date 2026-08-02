import React from "react";
import { useQuery } from "@tanstack/react-query";
import { useLocation } from "react-router-dom";

import Header from "./Header";
import { ConsoleProvider } from "./console-context";
import { routeDefinition } from "@/app/workspaces";
import { cn } from "@/lib/utils";
import { fetchConsoleBootstrap } from "@/services/console";
import { operatorIdentityKey, useOperatorIdentity } from "@/services/identity";

const layoutClasses = {
  canvas: "min-h-0 overflow-hidden",
  collection: "mx-auto w-full max-w-[100rem] px-4 py-6 sm:px-6 lg:px-8 lg:py-8",
  document: "mx-auto w-full max-w-[92rem] px-4 py-6 sm:px-6 lg:px-8 lg:py-8",
};

const Layout = ({ children }) => {
  const location = useLocation();
  const identity = useOperatorIdentity();
  const identityKey = operatorIdentityKey(identity);
  const bootstrapQuery = useQuery({
    queryKey: ["console-bootstrap", identityKey],
    queryFn: fetchConsoleBootstrap,
    staleTime: 30000,
    retry: 1,
  });
  const route = routeDefinition(location.pathname);
  const consoleValue = React.useMemo(() => ({
    bootstrap: bootstrapQuery.data,
    isLoading: bootstrapQuery.isLoading,
    error: bootstrapQuery.error,
  }), [bootstrapQuery.data, bootstrapQuery.error, bootstrapQuery.isLoading]);

  return (
    <ConsoleProvider value={consoleValue}>
      <div className="flex min-h-screen flex-col bg-background text-foreground">
        <a href="#main-content" className="sr-only z-[100] rounded-md bg-background px-3 py-2 focus:not-sr-only focus:fixed focus:left-3 focus:top-3 focus:ring-2 focus:ring-ring">
          Skip to content
        </a>
        <Header />
        <main
          id="main-content"
          data-layout={route.layout}
          className={cn("min-w-0 flex-1", layoutClasses[route.layout] || layoutClasses.collection)}
        >
          {children}
        </main>
      </div>
    </ConsoleProvider>
  );
};

export default Layout;
