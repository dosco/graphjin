import { FaGithub } from "react-icons/fa";

import { Layout } from "../components/Layout";
import { getAllPosts } from "../lib/posts";

export async function getStaticProps() {
  const allPosts = getAllPosts(["title", "slug", "description", "image"]);

  return { props: { posts: allPosts } };
}

export default function Home({ posts }) {
  const copyToClipboard = () => {
    navigator.clipboard.writeText("npm i graphjin");
  };

  return (
    <Layout>
      <div>
        <div className="text-2xl md:text-4xl font-extrabold tracking-tighter text-sky-500">
          A new kind of ORM
        </div>
        <div className="text-5xl md:text-8xl font-extrabold tracking-tighter text-sky-900">
          APIs in 5 mins. not weeks!
        </div>

        <p>
          Are you tired of spending endless hours coding and maintaining custom
          APIs for your applications?{" "}
          <mark>
            Try GraphJin, a totally new way to building complex database backed
            apps 100X faster.
          </mark>
        </p>

        <div className="uppercase text-md font-bold text-black">
          How does it work?
        </div>
        <p>
          Just write simple GraphQL queries to define the data you need and
          GraphJin will auto-magically convert them into efficient SQL queries
          and fetch the data you need.
        </p>

        <div className="uppercase text-md font-bold text-black">
          What does it support?
        </div>
        <p>
          Works with NodeJS and GO. Supports Postgres, MySQL, Yugabyte, AWS
          Aurora/RDS and Google Cloud SQL
        </p>

        <div className="flex gap-2 items-center">
          <a href="https://github.com/dosco/graphjin" target="_blank">
            <FaGithub size={50} className="mr-2" />
          </a>
          <div
            className="flex items-center gap-2 px-4 cursor-pointer border-2 rounded-lg border-black text-black shadow-lg"
            onClick={copyToClipboard}
          >
            npm i graphjin
            <span className="material-symbols-outlined">content_copy</span>
          </div>
        </div>
      </div>

      <div className="list-none mt-6">
        {posts.map((post, i) => (
          <li key={i} className="p-2">
            <a
              href={`/posts/${post.slug}`}
              className="text-3xl font-medium no-underline hover:underline underline-offset-8 decoration-1"
            >
              {post.title}
            </a>
            <div className="text-lg">{post.description}</div>
          </li>
        ))}
      </div>
    </Layout>
  );
}
