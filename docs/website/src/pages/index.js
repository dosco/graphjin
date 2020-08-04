import React from "react";
import { Redirect } from "@docusaurus/router";
import Layout from "@theme/Layout";
import Button from "./components/Button";
import Card from "./components/Card";
import GitHubButton from "react-github-btn";
import "../css/tailwind.css";

const HomeContent = () => {
  return (
    <div>
      <header className="pb-12 md:pb-24 w-full grid grid-cols-1 md:grid-cols-2">
        <div className="p-8 lg:p-24 prose-xl">
          <div>
            <h1 className="font-extrabold">Super Graph</h1>
            <h3 className="font-semibold">
              Fetch data without code. 100X your development speed
            </h3>
            <p>
              APIs change often don't waste time struggling with an ORM, code
              and SQL just ask for what you need in simple GraphQL
            </p>
            <small className="pt-4">
              Works with <span className="underline">Postgres</span> and{" "}
              <span className="underline">YugabyteDB</span>
            </small>
          </div>

          <div className="mt-4 flex items-center">
            <Button to="/docs/start">Get Started</Button>
            <div className="ml-8 pt-3">
              <GitHubButton
                href="https://github.com/dosco/super-graph"
                data-color-scheme="no-preference: light; light: light; dark: dark;"
                data-size="large"
                data-show-count="true"
                aria-label="Star dosco/super-graph on GitHub"
              >
                Star
              </GitHubButton>
            </div>
          </div>
        </div>
        <div
          className="pb-12 -ml-6"
          style={{ height: "600px", backgroundImage: "url(/img/graphql.png)" }}
        ></div>
      </header>

      <div className="border-t"></div>
      <div className="container py-12">
        <div className="text-2xl font-bold p-4 py-8">
          Open Source, Secure, High Performance and feature packed
        </div>
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
            description="Designed ground up to be lightning fast and highly scalable. Built in Go"
          />
          <Card
            title="Not a VC funded startup"
            description="Super Graph is a pure Apache licensed open source project with a fast growing community"
          />
        </div>
      </div>

      <div className="border-t"></div>
      <div className="container py-12">
        <div className="pb-12 md:pb-24 w-full grid grid-cols-1 md:grid-cols-2">
          <div className="flex justify-center items-center p-4 pb-12 md:p-12">
            <div>
              <small>Video chat with</small>
              <h1 className="text-2xl md:text-3xl font-bold">
                Brian Ketelsen,
              </h1>
              <h3 className="text-lg md:text-xl font-semibold text-pink-500">
                Co-organizer Gophercon & Principal Cloud Developer Advocate at
                Microsoft
              </h3>
            </div>
          </div>
          <div className="flex justify-center">
            <iframe
              width="560"
              height="315"
              src="https://www.youtube-nocookie.com/embed/4zXy-4gFSpQ"
              frameborder="0"
              allow="accelerometer; autoplay; encrypted-media; gyroscope; picture-in-picture"
              allowfullscreen
            ></iframe>
          </div>
        </div>
      </div>
    </div>
  );
};

const Home = () => {
  return (
    <Layout title="Super Graph" description="Fetch data without code">
      <HomeContent />
    </Layout>
  );
};

export default Home;
