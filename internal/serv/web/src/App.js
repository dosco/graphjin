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
            color: "lightblue",
            letterSpacing: "0.15rem",
            paddingTop: "10px",
            paddingBottom: "0px",
          }}
        >
          <div
            style={{
              textDecoration: "none",
              margin: "0px",
              fontSize: "14px",
              fontWeight: "500",
              textTransform: "uppercase",
              marginLeft: "10px",
            }}
          >
            Super Graph
          </div>
        </header>

        <Provider store={store}>
          <Playground
            endpoint="http://localhost:8080/api/v1/graphql"
            settings="{
            'general.betaUpdates': true,
            'editor.reuseHeaders': true,
          }"
          />
        </Provider>
      </div>
    );
  }
}

// 'schema.polling.enable': false,
// 'request.credentials': 'include',

export default App;
