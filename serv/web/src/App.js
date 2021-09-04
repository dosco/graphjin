import React, { useEffect, useState } from "react";
import GraphiQL from "graphiql";
import GraphiQLExplorer from "graphiql-explorer";
import { createGraphiQLFetcher } from "@graphiql/toolkit";
import { buildClientSchema, getIntrospectionQuery } from "graphql";
import GitHubButton from "react-github-btn";

import "graphiql/graphiql.min.css";

const url = `${window.location.protocol}//${window.location.host}/api/v1/graphql`;
const subscriptionUrl = `ws://${window.location.host}/api/v1/graphql`;

const fetcher = createGraphiQLFetcher({
  url,
  subscriptionUrl,
});

const openLink = (url) => {
  window.open(url, "_blank");
};

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
          <div
            style={{
              display: "flex",
              justifyContent: "center",
              alignItems: "center",
              padding: "2px 0",
            }}
          >
            <div
              style={{
                letterSpacing: "3px",
                paddingBottom: "3px",
                marginRight: "5px",
              }}
            >
              GRAPHJIN
            </div>
            <GitHubButton
              href="https://github.com/dosco/graphjin"
              data-color-scheme="no-preference: dark; light: light; dark: dark;"
              data-size="large"
              data-show-count="true"
              aria-label="Star dosco/graphjin on GitHub"
            >
              Star
            </GitHubButton>
          </div>
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
          <GraphiQL.Menu label="❤️ GraphJin" title="Support the project">
            <GraphiQL.MenuItem
              onSelect={() =>
                openLink(
                  "https://twitter.com/share?text=Build%20APIs%20in%205%20minutes%20with%20GraphJin.%20An%20automagical%20GraphQL%20to%20SQL%20compiler&url=https://github.com/dosco/graphjin"
                )
              }
              label="Share on Twitter"
              title="Share on Twitter"
            />
            <GraphiQL.MenuItem
              onSelect={() => openLink("https://github.com/sponsors/dosco")}
              label="Sponsor on GitHub"
              title="Sponsor on GitHub"
            />
          </GraphiQL.Menu>
          {/* <div style={{ marginLeft: "20px" }}>
            <GitHubButton
              href="https://github.com/dosco/graphjin"
              data-color-scheme="no-preference: dark; light: light; dark: dark;"
              data-size="large"
              data-show-count="true"
              aria-label="Star dosco/graphjin on GitHub"
            >
              Star
            </GitHubButton>
          </div> */}
        </GraphiQL.Toolbar>
      </GraphiQL>
    </div>
  );
};

export default App;
