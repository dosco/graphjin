import React from "react";
import Layout from "@theme/Layout";
import Button from "./components/Button";
import Card from "./components/Card";
import GitHubButton from "react-github-btn";
import "../css/tailwind.css";

const HomeContent = () => {
  return (
    <>
      <div className="container mx-auto my-24">
        <div className="block md:flex">
          <div className="md:w-3/6">
            <h1 className="text-5xl md:text-7xl font-extrabold">
              Build APIs in 5 minutes not weeks
            </h1>
            <div className="text-xl md:text-2xl mb-4">
              GraphJin is an automagical GraphQL to SQL compiler. Just write
              your query in simple GraphQL we'll deliver the data. Built in GO.
              Use as a library or a standalone service.{" "}
            </div>
            <div className="inline-block text-indigo-800 text-md font-bold mb-4">
              [ Formerly known as Super Graph ]
            </div>

            <div className="mt-2 flex items-center">
              <Button to="/docs/start">Get Started</Button>
              <div className="ml-4 pt-3">
                <GitHubButton
                  href="https://github.com/dosco/graphjin"
                  data-color-scheme="no-preference: light; light: light; dark: dark;"
                  data-size="large"
                  data-show-count="true"
                  aria-label="Star dosco/graphjin on GitHub"
                >
                  Star
                </GitHubButton>
              </div>
            </div>
          </div>
          <div className="hidden md:block w-3/6">
            <img style={{ width: "500px" }} src="/img/hero.png" />
          </div>
        </div>
      </div>

      <div className="container mx-auto my-24">
        <div className="md:grid gap-4 grid-cols-3">
          <Card
            title="Fast queries"
            description="Query or update your database with simple GraphQL. Deeply nested queries and mutations are supported"
          />
          <Card
            title="Realtime subscriptions"
            description="Subscribe to a query and instantly get all related updates in realtime"
          />
          <Card
            title="Database discovery"
            description="Just point at adatabase and we're good to go. Works with Postgres and the distributed Yugabyte DB"
          />
          <Card
            title="Access control"
            description="An allow list controls what queries run in production. Additionally role and attribute based access control can be used"
          />
          <Card
            title="Build fast"
            description="Out of the box support for infinite scroll, threaded comments, activity feed and othr common app patterns"
          />
          <Card
            title="Full-text search"
            description="Leverage the powerful full-text search capability of Postgres for search and auto-complete"
          />
          <Card
            title="Authentication"
            description="Support for JWT, Firebase, Rails cookie and other authentication mechanisms"
          />
          <Card
            title="High Performance"
            description="Designed ground up to be lightning fast and highly scalable. Built in Go"
          />
          <Card
            title="Not a startup"
            description="GraphJin is a pure Apache licensed open source project with a fast growing community"
          />
        </div>
      </div>

      <div className="container mx-auto my-24">
        <div className="grid gap-6 md:grid-cols-2 ">
          <div className="bg-gray-100 p-3">
            <iframe
              width="100%"
              height="315"
              src="https://www.youtube-nocookie.com/embed/4zXy-4gFSpQ"
              frameborder="0"
              allow="accelerometer; autoplay; encrypted-media; gyroscope; picture-in-picture"
              allowfullscreen
            ></iframe>
            <div className="text-md font-semibold p-2">
              Brian Ketelsen, Co-organizer Gophercon & Principal Cloud Developer
              Advocate at Microsoft
            </div>
          </div>

          <div className="bg-gray-100 p-3">
            <iframe
              width="100%"
              height="315"
              src="https://www.youtube-nocookie.com/embed/gzAiAbsCMVA"
              frameborder="0"
              allow="accelerometer; autoplay; encrypted-media; gyroscope; picture-in-picture"
              allowfullscreen
            ></iframe>
            <div className="text-md font-semibold p-2">
              Vikram, Postgres Build 2020 Presentation
            </div>
          </div>
        </div>
      </div>
    </>
  );
};

const Home = () => {
  return (
    <Layout title="GraphJin" description="Fetch data without code">
      <HomeContent />
    </Layout>
  );
};

export default Home;
