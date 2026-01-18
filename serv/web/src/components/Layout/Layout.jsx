import React from "react";
import Header from "./Header";
import Sidebar from "./Sidebar";
import "./Layout.css";

const Layout = ({ children }) => {
  return (
    <div className="gj-app">
      <Header />
      <div className="gj-main">
        <Sidebar />
        <main className="gj-content">{children}</main>
      </div>
    </div>
  );
};

export default Layout;
