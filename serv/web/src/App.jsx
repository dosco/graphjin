import React from "react";
import { BrowserRouter, Routes, Route } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

import Layout from "./components/Layout/Layout";
import ErrorBoundary from "./ErrorBoundary";

import QueryEditor from "./pages/QueryEditor";
import SavedQueries from "./pages/SavedQueries";
import SchemaExplorer from "./pages/SchemaExplorer";
import ConfigViewer from "./pages/ConfigViewer";
import DatabasesInfo from "./pages/DatabasesInfo";
import ApiDocs from "./pages/ApiDocs";

import "./theme-scifi.css";

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
        <BrowserRouter basename={import.meta.env.BASE_URL}>
          <Layout>
            <Routes>
              <Route path="/" element={<QueryEditor />} />
              <Route path="/queries" element={<SavedQueries />} />
              <Route path="/schema" element={<SchemaExplorer />} />
              <Route path="/config" element={<ConfigViewer />} />
              <Route path="/databases" element={<DatabasesInfo />} />
              <Route path="/api-docs" element={<ApiDocs />} />
            </Routes>
          </Layout>
        </BrowserRouter>
      </QueryClientProvider>
    </ErrorBoundary>
  );
};

export default App;
