import { FaGithub } from "react-icons/fa";

import { Layout } from "../components/Layout"
import { getAllPosts} from "../lib/posts"

export async function getStaticProps() {
  const allPosts = getAllPosts([
    'title',
    'slug',
    'description',
    'image',
  ])


  return { props: {  posts: allPosts  }}
}

export default function Home({ posts }) {

  const copyToClipboard = () => {
    navigator.clipboard.writeText("npm i graphjin");
  }

  return (
   <Layout>
      <div>
        <div className="text-5xl md:text-8xl font-extrabold tracking-tighter text-lime-500">
        APIs in 5 mins. not weeks!
        </div>

        <p>
          GraphJin is a magical library that instantly converts simple GraphQL into fast and secure APIs. Works 
          with NodeJS and GO. Supports Postgres, MySQL, Yugabyte, Cockroach. 
        </p>

        <div class="flex gap-2 items-center">
          <a href="https://github.com/dosco/graphjin" target="_blank">
            <FaGithub size={50} className="mr-2" />
          </a>
          <mark class="flex items-center gap-2 px-4 cursor-pointer" onClick={copyToClipboard}>
            npm i graphjin
            <span class="material-symbols-outlined">content_copy</span>
          </mark>
        </div>
      </div> 

      <div className="marker:text-lime-500 mt-6">
      {posts.map((post, i) => (
        <li key={i} className="hover:marker:text-indigo-500 p-2">
          <a href={`/posts/${post.slug}`} className="text-3xl font-medium no-underline hover:underline underline-offset-8 decoration-1">
            {post.title}
            </a>
          <div className="text-lg">{post.description}</div>
        </li>
      ))}
      </div>
    </Layout>
  )
}
