import React, { Component } from "react";
import { Provider } from "react-redux";
import { Playground, store } from "graphql-playground-react";

import "./index.css";

const fetch = window.fetch;
window.fetch = function () {
  arguments[1].credentials = "include";
  return Promise.resolve(fetch.apply(global, arguments));
};

class App extends Component {
  render() {
    return (
      <div>
        <header
          style={{
            color: "white",
            letterSpacing: "0.15rem",
            paddingTop: "10px",
          }}
        >
          <div
            style={{
              textDecoration: "none",
              margin: "0px",
              fontSize: "16px",
              fontWeight: "600",
              textTransform: "uppercase",
              marginLeft: "10px",
            }}
          >
            GraphJin
          </div>
        </header>

        <Provider store={store}>
          <Playground
            title="Hello"
            endpoint="http://localhost:8080/api/v1/graphql"
            settings={{
              "general.betaUpdates": true,
              "editor.reuseHeaders": true,
              "editor.theme": "dark",
              "prettier.useTabs": true,
              "tracing.hideTracingResponse": true,
              "tracing.tracingSupported": false,
            }}
            codeTheme={
              {
                // editorBackground: "black",
                // resultBackground: "black",
                // rightDrawerBackground: "#141823",
              }
            }
          />
        </Provider>
      </div>
    );
  }
}

// 'schema.polling.enable': false,
// 'request.credentials': 'include',

export default App;
