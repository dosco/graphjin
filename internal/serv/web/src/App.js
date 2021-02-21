import React, { useEffect, useState } from "react";
import GraphiQL from "graphiql";
import GraphiQLExplorer from "graphiql-explorer";
import { createGraphiQLFetcher } from "@graphiql/toolkit";
import { buildClientSchema, getIntrospectionQuery } from "graphql";

import "graphiql/graphiql.min.css";

const url = `http://${window.location.host}/api/v1/graphql`;
const subscriptionUrl = `ws://${window.location.host}/api/v1/graphql`;

const fetcher = createGraphiQLFetcher({
  url,
  subscriptionUrl,
});

const defaultQuery = `
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
  const [schema, setSchema] = useState(null);
  const [query, setQuery] = useState(defaultQuery);
  const [explorerOpen, setExplorerOpen] = useState(true);

  let graphiql = React.createRef();

  useEffect(() => {
    (async function () {
      let introspect = fetcher({ query: getIntrospectionQuery() });
      let res = await introspect.next();
      setSchema(buildClientSchema(res.value.data));
    })();
  }, []);

  const handleEditQuery = (query) => {
    setQuery(query);
    console.log(">", query);
  };

  const handleToggleExplorer = () => setExplorerOpen(!explorerOpen);

  return (
    <div className="graphiql-container">
      <GraphiQLExplorer
        schema={schema}
        query={query}
        onEdit={handleEditQuery}
        onRunOperation={(operationName) =>
          graphiql.handleRunQuery(operationName)
        }
        explorerIsOpen={explorerOpen}
        onToggleExplorer={handleToggleExplorer}
      />
      <GraphiQL
        ref={(ref) => (graphiql = ref)}
        fetcher={fetcher}
        defaultSecondaryEditorOpen={true}
        headerEditorEnabled={true}
        shouldPersistHeaders={true}
        query={query}
        onEditQuery={handleEditQuery}
      >
        <GraphiQL.Logo>
          <div style={{ letterSpacing: "3px" }}>GRAPHJIN</div>
        </GraphiQL.Logo>

        <GraphiQL.Toolbar>
          <GraphiQL.Button
            onClick={() => graphiql.handlePrettifyQuery()}
            label="Prettify"
            title="Prettify Query (Shift-Ctrl-P)"
          />
          <GraphiQL.Button
            onClick={() => graphiql.handleToggleHistory()}
            label="History"
            title="Show History"
          />
          <GraphiQL.Button
            onClick={handleToggleExplorer}
            label="Explorer"
            title="Toggle Explorer"
          />
        </GraphiQL.Toolbar>
      </GraphiQL>
    </div>
  );
};

export default App;
