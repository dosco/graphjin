import Image from 'next/image'

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
      </div> 

      <div className="marker:text-lime-500">
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
