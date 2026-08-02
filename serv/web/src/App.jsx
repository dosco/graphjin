import React, { Suspense, lazy } from "react";
import { BrowserRouter, Navigate, Routes, Route } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import Layout from "./components/Layout/Layout";
import { ThemeProvider } from "./components/theme-provider";
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
const MissionControl = lazy(() => import("./pages/MissionControl"));

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
        <ThemeProvider>
          <BrowserRouter basename="/">
            <Layout>
              <Suspense fallback={<LoadingState label="Loading view" />}>
                <Routes>
                  <Route path="/" element={<Navigate to="/user/agent" replace />} />
                  <Route path="/user/agent" element={<AgentChat />} />
                  <Route path="/user/tasks" element={<MissionControl section="tasks" />} />
                  <Route path="/user/watches" element={<MissionControl section="watches" />} />
                  <Route path="/user/artifacts" element={<MissionControl section="annotations" />} />

                  <Route path="/admin/runtime" element={<Dashboard />} />
                  <Route path="/admin/workbench" element={<QueryEditor />} />
                  <Route path="/admin/catalog" element={<CatalogExplorer />} />
                  <Route path="/admin/workflows" element={<CatalogExplorer initialKind="workflow" title="Workflows" description="Discover approved workflows and their GraphJin execution surfaces." />} />
                  <Route path="/admin/security" element={<SecurityAudit />} />
                  <Route path="/admin/code" element={<CodeInsight />} />
                  <Route path="/admin/config" element={<ConfigViewer />} />
                  <Route path="/admin/api" element={<ApiDocs />} />

                  <Route path="/agent" element={<NavigateWithSearch to="/user/agent" />} />
                  <Route path="/mission" element={<LegacyMissionRedirect />} />
                  <Route path="/workbench" element={<NavigateWithSearch to="/admin/workbench" />} />
                  <Route path="/catalog" element={<NavigateWithSearch to="/admin/catalog" />} />
                  <Route path="/schema" element={<NavigateWithSearch to="/admin/catalog" />} />
                  <Route path="/queries" element={<NavigateWithSearch to="/admin/catalog" />} />
                  <Route path="/security" element={<NavigateWithSearch to="/admin/security" />} />
                  <Route path="/code" element={<NavigateWithSearch to="/admin/code" />} />
                  <Route path="/config" element={<NavigateWithSearch to="/admin/config" />} />
                  <Route path="/databases" element={<NavigateWithSearch to="/admin/runtime" />} />
                  <Route path="/api-docs" element={<NavigateWithSearch to="/admin/api" />} />
                  <Route path="*" element={<Navigate to="/user/agent" replace />} />
                </Routes>
              </Suspense>
            </Layout>
          </BrowserRouter>
        </ThemeProvider>
      </QueryClientProvider>
    </ErrorBoundary>
  );
};

function NavigateWithSearch({ to }) {
  const search = window.location.search || "";
  return <Navigate to={`${to}${search}`} replace />;
}

function LegacyMissionRedirect() {
  const params = new URLSearchParams(window.location.search);
  const tab = params.get("tab");
  params.delete("tab");
  const target = tab === "watches" ? "/user/watches" : tab === "annotations" ? "/user/artifacts" : "/user/tasks";
  const search = params.toString();
  return <Navigate to={`${target}${search ? `?${search}` : ""}`} replace />;
}

export default App;
