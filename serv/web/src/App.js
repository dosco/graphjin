import React, { useState } from "react";

import { createGraphiQLFetcher } from "@graphiql/toolkit";
import { GraphiQL } from "graphiql";
import { explorerPlugin } from "@graphiql/plugin-explorer";

import "graphiql/graphiql.css";

let apiPath = "/api/v1/graphql";

const urlParams = new URLSearchParams(window.location.search);
const ep = urlParams.get("endpoint");

if (ep !== null && ep !== "") {
  apiPath = ep;
}

const fetcher = createGraphiQLFetcher({
  url: `${window.location.protocol}//${window.location.host}${apiPath}`,
  subscriptionUrl: `ws://${window.location.host}${apiPath}`,
});

const explorer = explorerPlugin();

// import GitHubButton from "react-github-btn";

const defaultQuery = `
# Welcome to GraphJin Web

# Use this editor to build and test your GraphQL queries
# Set a query name if you want the query saved to the
# allow list to use in production

query {
  users(id: "3") {
    id
    full_name
    email
  }
}
`;

const App = () => {
  const [query, setQuery] = useState(defaultQuery);

  return (
    <GraphiQL
      fetcher={fetcher}
      query={query}
      onEditQuery={setQuery}
      plugins={[explorer]}
    />
  );
};

export default App;
