import React, { Suspense, lazy } from "react";
import { useQuery } from "@tanstack/react-query";
import { DataErrorState, LoadingState, PageHeader } from "../components/ui";
import { Card, CardContent } from "@/components/ui/card";
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
        <Card className="overflow-hidden">
          <CardContent className="p-4 md:p-6">
            <div className="swagger-shell">
          <Suspense fallback={<LoadingState label="Rendering API documentation" />}>
            <SwaggerUI spec={data} />
          </Suspense>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  );
};

export default ApiDocs;
