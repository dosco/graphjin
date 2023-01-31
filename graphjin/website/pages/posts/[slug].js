import Link from "next/link";
import { getPostBySlug, getAllPosts } from "../../lib/posts";
import { markdownToHtml } from "../../lib/markdown";
import { Layout } from "../../components/Layout";

export default function Post({ post }) {
  return (
    <Layout
      title={post.title}
      description={post.description}
      image={post.image}
    >
      <div>
        <article
          className=""
          dangerouslySetInnerHTML={{ __html: post.content }}
        />
      </div>
      <Nav post={post} />
    </Layout>
  );
}

const Nav = ({ post }) => (
  <div className="grid justify-items-stretch grid-cols-2 gap-2">
    {post.previous && <NavButton post={post.previous} label="Previous" />}
    {post.next && <NavButton post={post.next} label="Next" />}
  </div>
);

const NavButton = ({ post, label }) => (
  <Link href={post.slug}>
    <div className="border border-gray-500 rounded-full px-6 py-2 text-center cursor-pointer hover:shadow">
      <div className="text-black font-bold">{label}</div>
      <div className="text-xl">{post.title}</div>
    </div>
  </Link>
);

export async function getStaticProps({ params }) {
  const posts = getAllPosts(["slug", "title"]);
  const nl = posts.filter(
    (p, i) =>
      posts[i + 1]?.slug === params.slug ||
      posts[i - 1]?.slug === params.slug ||
      p.slug === params.slug
  );
  const nav =
    nl.length === 3
      ? { previous: nl[0], next: nl[2] } // if
      : nl[0].slug === params.slug
      ? { previous: null, next: nl[1] } // else if
      : nl[1].slug === params.slug
      ? { previous: nl[0], next: null } // else if
      : null; // else

  const post = {
    ...getPostBySlug(params.slug, [
      "title",
      "description",
      "date",
      "slug",
      "author",
      "content",
      "image",
    ]),
    ...nav,
  };

  const content = await markdownToHtml(post.content || "");

  return {
    props: {
      post: {
        ...post,
        content,
      },
    },
  };
}

export async function getStaticPaths() {
  const posts = getAllPosts(["slug"]);

  return {
    paths: posts.map((post) => {
      return {
        params: {
          slug: post.slug,
        },
      };
    }),
    fallback: false,
  };
}
