import React, { useState, useMemo } from "react";
import { createGraphiQLFetcher } from "@graphiql/toolkit";
import { GraphiQL } from "graphiql";
import { explorerPlugin } from "@graphiql/plugin-explorer";

import "graphiql/graphiql.css";

const defaultEndpoint = import.meta.env.VITE_DEFAULT_ENDPOINT || "/api/v1/graphql";

const defaultQuery = `# Welcome to GraphJin
# Use this editor to build and test your GraphQL queries

query getUsers {
  users(limit: 10) {
    id
    full_name
    email
  }
}
`;

const QueryEditor = () => {
  const [query, setQuery] = useState(defaultQuery);

  const { fetcher, explorer } = useMemo(() => {
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

    const explorer = explorerPlugin();

    return { fetcher, explorer };
  }, []);

  return (
    <div className="gj-query-editor">
      <GraphiQL
        fetcher={fetcher}
        query={query}
        onEditQuery={setQuery}
        plugins={[explorer]}
      />
    </div>
  );
};

export default QueryEditor;
