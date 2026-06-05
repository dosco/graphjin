import React, { Suspense, lazy } from "react";
import { useQuery } from "@tanstack/react-query";
import { DataErrorState, LoadingState, PageHeader } from "../components/ui";
import { fetchJSON } from "../services/graphql";
import "swagger-ui-react/swagger-ui.css";

const SwaggerUI = lazy(() => import("swagger-ui-react"));

const ApiDocs = () => {
  const { data, error, isLoading } = useQuery({
    queryKey: ["openapi-spec"],
    queryFn: () => fetchJSON("/api/v1/openapi.json"),
    staleTime: 60000,
  });

  return (
    <div className="page-stack gj-api-docs">
      <PageHeader
        eyebrow="OpenAPI"
        title="API Docs"
        description="Interactive API reference generated from saved REST queries."
      />
      {isLoading ? (
        <LoadingState label="Loading API documentation" />
      ) : error ? (
        <DataErrorState
          error={error}
          permissionMessage="OpenAPI docs use the current GraphJin operator session. Expose this console only to local developers or authenticated admins in agentic mode."
          unavailableMessage="The OpenAPI spec could not be loaded from /api/v1/openapi.json."
        />
      ) : (
        <div className="swagger-container">
          <Suspense fallback={<LoadingState label="Rendering API documentation" />}>
            <SwaggerUI spec={data} />
          </Suspense>
        </div>
      )}
    </div>
  );
};

export default ApiDocs;
