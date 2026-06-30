import "../setupGraphiQLWorkers";
import React, { useMemo } from "react";
import { createGraphiQLFetcher } from "@graphiql/toolkit";
import { GraphiQL } from "graphiql";

import { PageHeader } from "../components/ui";
import { Card, CardContent } from "@/components/ui/card";
import "graphiql/style.css";

const defaultEndpoint = import.meta.env.VITE_DEFAULT_ENDPOINT || "/api/v1/graphql";

const defaultQuery = `# GraphJin Workbench
# Query application roots or GraphJin system roots through the normal GraphQL endpoint.

query SourceHealth {
  gj_runtime(where: { kind: { eq: "source" } }, order_by: { source: asc }) {
    source
    source_kind
    status
    severity
    summary
    next_action
  }
}
`;

const QueryEditor = () => {
  const fetcher = useMemo(() => {
    let apiPath = defaultEndpoint;
    const urlParams = new URLSearchParams(window.location.search);
    const ep = urlParams.get("endpoint");

    // Only allow relative paths, reject protocol-relative URLs like //evil.com
    if (ep !== null && ep !== "" && ep.startsWith("/") && !ep.startsWith("//")) {
      apiPath = ep;
    }

    const wsProtocol = window.location.protocol === "https:" ? "wss:" : "ws:";

    const fetcher = createGraphiQLFetcher({
      url: `${window.location.protocol}//${window.location.host}${apiPath}`,
      subscriptionUrl: `${wsProtocol}//${window.location.host}${apiPath}`,
    });

    return fetcher;
  }, []);

  return (
    <div className="mx-auto grid max-w-7xl gap-6">
      <PageHeader
        eyebrow="GraphQL"
        title="Workbench"
        description="Run GraphJin queries against the same endpoint used by applications and system-root views."
      />
      <Card className="overflow-hidden">
        <CardContent className="p-0">
          <div className="graphiql-shell">
            <GraphiQL
              fetcher={fetcher}
              forcedTheme="light"
              initialQuery={defaultQuery}
            />
          </div>
        </CardContent>
      </Card>
    </div>
  );
};

export default QueryEditor;
