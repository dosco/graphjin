import React from "react";
import Header from "./Header";
import Sidebar from "./Sidebar";

const Layout = ({ children }) => {
  return (
    <div className="min-h-screen bg-background text-foreground">
      <Header />
      <div className="grid min-h-[calc(100vh-4rem)] lg:grid-cols-[240px_minmax(0,1fr)]">
        <Sidebar className="hidden lg:block" />
        <main className="min-w-0 p-4 md:p-6 lg:p-8" id="main-content">
          {children}
        </main>
      </div>
    </div>
  );
};

export default Layout;
