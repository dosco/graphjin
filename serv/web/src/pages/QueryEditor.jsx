import "../setupGraphiQLWorkers";
import React, { useMemo } from "react";
import { createGraphiQLFetcher } from "@graphiql/toolkit";
import { GraphiQL } from "graphiql";

import { PageHeader } from "../components/ui";
import { useTheme } from "@/components/theme-provider";
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
  const { resolvedTheme } = useTheme();
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
    <div className="grid h-[calc(100dvh-6.25rem)] min-h-[38rem] grid-rows-[auto_minmax(0,1fr)] overflow-hidden">
      <div className="border-b px-4 py-4 sm:px-6">
        <PageHeader eyebrow="GraphQL" title="Workbench" description="Run application and system-root queries against the active GraphJin endpoint." />
      </div>
      <div className="graphiql-shell min-h-0">
        <GraphiQL fetcher={fetcher} forcedTheme={resolvedTheme} initialQuery={defaultQuery} />
      </div>
    </div>
  );
};

export default QueryEditor;
