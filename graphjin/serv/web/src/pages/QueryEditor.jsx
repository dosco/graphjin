import "../setupGraphiQLWorkers";
import React, { useMemo } from "react";
import { createGraphiQLFetcher } from "@graphiql/toolkit";
import { GraphiQL } from "graphiql";

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
    <div className="page-stack workbench-page">
      <div className="page-header">
        <div>
          <p className="eyebrow">GraphQL</p>
          <h1>Workbench</h1>
          <p>Run GraphJin queries against the same endpoint used by applications and system-root views.</p>
        </div>
      </div>
      <div className="gj-query-editor">
        <GraphiQL
          fetcher={fetcher}
          forcedTheme="light"
          initialQuery={defaultQuery}
        />
      </div>
    </div>
  );
};

export default QueryEditor;
