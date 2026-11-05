import React from "react";
import { useQuery } from "@tanstack/react-query";
import { useLocation } from "react-router-dom";

import ContextRail from "./ContextRail";
import Header from "./Header";
import { ConsoleProvider } from "./console-context";
import { routeDefinition } from "@/app/workspaces";
import { cn } from "@/lib/utils";
import { fetchConsoleBootstrap } from "@/services/console";
import { adoptSuggestedIdentity, operatorIdentityKey, useOperatorIdentity } from "@/services/identity";

const layoutClasses = {
  canvas: "min-h-0 overflow-hidden",
  collection: "w-full px-4 py-5 sm:px-6 lg:py-6",
  document: "w-full px-4 py-5 sm:px-6 lg:py-6",
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
  // Adopting rewrites identityKey, which refetches bootstrap with the new
  // headers; the suggestion is then absent and this settles after one pass.
  React.useEffect(() => {
    adoptSuggestedIdentity(bootstrapQuery.data?.identity?.suggested);
  }, [bootstrapQuery.data]);
  const route = routeDefinition(location.pathname);
  const consoleValue = React.useMemo(() => ({
    bootstrap: bootstrapQuery.data,
    isLoading: bootstrapQuery.isLoading,
    error: bootstrapQuery.error,
  }), [bootstrapQuery.data, bootstrapQuery.error, bootstrapQuery.isLoading]);

  return (
    <ConsoleProvider value={consoleValue}>
      <div className="relative min-h-dvh bg-background text-foreground">
        <div className="horizon" aria-hidden="true" />
        <div className="starfield" aria-hidden="true" />
        <a href="#main-content" className="sr-only z-[100] rounded-md bg-foreground px-4 py-2 font-semibold text-background focus:not-sr-only focus:fixed focus:left-3 focus:top-3">
          Skip to content
        </a>
        <div className="relative mx-auto flex min-h-dvh w-full max-w-[100rem] flex-col md:flex-row">
          <Header />
          <main
            id="main-content"
            data-layout={route.layout}
            className={cn(
              "relative min-w-0 flex-1 pb-[var(--chrome-bottom)] md:pb-0",
              layoutClasses[route.layout] || layoutClasses.collection
            )}
          >
            {children}
          </main>
          {route.layout !== "canvas" && <ContextRail />}
        </div>
      </div>
    </ConsoleProvider>
  );
};

export default Layout;
