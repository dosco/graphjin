import React from "react";
import { Redirect } from "@docusaurus/router";
import Layout from "@theme/Layout";
import Button from "./components/Button";
import Card from "./components/Card";
import "../css/tailwind.css";

function Home() {
  return (
    <Layout title="Super Graph" description="Fetch data without code">
      <header className="pb-12 md:pb-24 w-full grid grid-cols-1 md:grid-cols-2">
        <div className="p-8 lg:p-24">
          <div className="prose prose-xl">
            <h1>Super Graph</h1>
            <h3>Fetch data without code 100X your development speed</h3>
            <p>
              APIs change often don't waste time on struggling with ORMs, code
              and complex SQL just ask for what you need in simple GraphQL.
            </p>
          </div>
          <div className="mt-12">
            <Button to="/docs/start">Get Started</Button>
          </div>
        </div>
        <div
          className="pb-12"
          style={{ height: "600px", backgroundImage: "url(/img/graphql.png)" }}
        ></div>
      </header>

      <div className="p-4 md:px-24">
        <div className="flex flex-wrap">
          <Card
            title="Queries and Mutations"
            description="Query or update your database with simple GraphQL. Deeply nested queries and mutations are supported"
          />
          <Card
            title="Realtime subscriptions"
            description="Subscribe to a query and instantly get all related updates in realtime"
          />
          <Card
            title="Database discovery"
            description="Just point it at your database and we're good to go. Auto discover of schemas and relationships"
          />
        </div>
        <div className="flex flex-wrap">
          <Card
            title="Access control"
            description="An allow list controls what queries run in production. Additionally role and attribute based access control can be used"
          />
          <Card
            title="Infinite Pagination"
            description="Efficient cursor based pagination to implement fast infinite scroll style features"
          />
          <Card
            title="Full-text search"
            description="Leverage the powerful full-text search capability of Postgres for search and auto-complete"
          />
        </div>
        <div className="flex flex-wrap">
          <Card
            title="Authentication"
            description="Support for JWT, Firebase, Rails cookie and other authentication mechanisms"
          />
          <Card
            title="High Performance"
            description="Designed ground up to be lightning fast ad highly scalable. Build in Go"
          />
          <Card
            title="Not a VC funded startup"
            description="Super Graph is a pure Apache licensed open source project with a fast growing community"
          />
        </div>
      </div>
    </Layout>
  );
}

export default Home;
