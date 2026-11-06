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
    <div className="mx-auto grid max-w-7xl gap-6">
      <PageHeader
        title="API"
      />
      {isLoading ? (
        <LoadingState />
      ) : error ? (
        <DataErrorState
          error={error}
          permissionMessage="Your role can't see the API docs."
          unavailableMessage="Couldn't load the API docs."
        />
      ) : (
        <div className="swagger-shell">
          <Suspense fallback={<LoadingState />}>
            <SwaggerUI spec={data} />
          </Suspense>
        </div>
      )}
    </div>
  );
};

export default ApiDocs;
