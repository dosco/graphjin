import React from "react";
import * as ReactDOM from "react-dom/client";
import App from "./App";
import "./index.css";

// Enable Sci-Fi dark theme
document.body.classList.add("graphiql-dark");

const root = ReactDOM.createRoot(document.getElementById("root"));
root.render(
  <React.StrictMode>
    <App />
  </React.StrictMode>
);
