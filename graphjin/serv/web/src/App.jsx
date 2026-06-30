import React, { Suspense, lazy } from "react";
import { BrowserRouter, Navigate, Routes, Route } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import Layout from "./components/Layout/Layout";
import ErrorBoundary from "./ErrorBoundary";
import { LoadingState } from "./components/ui";

import Dashboard from "./pages/Dashboard";
import CatalogExplorer from "./pages/CatalogExplorer";
import SecurityAudit from "./pages/SecurityAudit";
import CodeInsight from "./pages/CodeInsight";
import ConfigViewer from "./pages/ConfigViewer";

const QueryEditor = lazy(() => import("./pages/QueryEditor"));
const ApiDocs = lazy(() => import("./pages/ApiDocs"));
const AgentChat = lazy(() => import("./pages/AgentChat"));

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      staleTime: 30000,
      retry: 1,
    },
  },
});

const App = () => {
  return (
    <ErrorBoundary>
      <QueryClientProvider client={queryClient}>
        <BrowserRouter basename="/">
          <Layout>
            <Suspense fallback={<LoadingState label="Loading view" />}>
              <Routes>
                <Route path="/" element={<Dashboard />} />
                <Route path="/workbench" element={<QueryEditor />} />
                <Route path="/agent" element={<AgentChat />} />
                <Route path="/catalog" element={<CatalogExplorer />} />
                <Route path="/schema" element={<Navigate to="/catalog" replace />} />
                <Route path="/queries" element={<Navigate to="/catalog" replace />} />
                <Route path="/security" element={<SecurityAudit />} />
                <Route path="/code" element={<CodeInsight />} />
                <Route path="/config" element={<ConfigViewer />} />
                <Route path="/databases" element={<Navigate to="/" replace />} />
                <Route path="/api-docs" element={<ApiDocs />} />
              </Routes>
            </Suspense>
          </Layout>
        </BrowserRouter>
      </QueryClientProvider>
    </ErrorBoundary>
  );
};

export default App;
