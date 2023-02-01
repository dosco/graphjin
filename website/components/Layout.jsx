import Head from "next/head";
import Script from "next/script";
import { FaTwitter, FaGithub } from "react-icons/fa";

export const Layout = ({
  title = "GraphJin - Build APIs in 5 minutes",
  description = "Build APIs in 5 minutes not weeks",
  twitter = "@dosco",
  image = "",
  children,
}) => {
  return (
    <div>
      <Head>
        <title>{title}</title>
        <link rel="icon" href="/favicon.ico" />
        <meta name="description" content={description} />
        <meta name="twitter:title" content={title} />
        <meta name="twitter:card" content="summary" />
        <meta name="twitter:site" content={twitter} />
        <meta name="twitter:image" content={image} href={image} />
        <meta name="twitter:image:alt" content={`{title} Logo`} />
        <meta property="og:image" content={image} />
      </Head>

      <main>
        <nav className="w-full pt-2 px-1 md:px-0">
          <div className="w-full md:w-6/12 mx-auto flex gap-6 items-center justify-between px-2">
            <h1 className="text-2xl font-semibold tracking-widest text-red-500">
              <a href="/">GRAPHJIN</a>
            </h1>

            <div className="flex gap-4">
              <a href="https://twitter.com/dosco" target="_blank">
                <FaTwitter size={40} className="text-red-500" />
              </a>
              <a href="https://github.com/dosco/graphjin" target="_blank">
                <FaGithub size={40} className="text-red-500" />
              </a>
            </div>
          </div>
        </nav>
        <div className="prose lg:prose-xl prose-neutral prose-h1:text-sky-900 mx-auto shadow-xl border-t border-t-gray-200 rounded-xl mt-2 p-4 md:p-6 pb-20">
          {children}
        </div>
      </main>

      {/* <footer className="border-t bg-indigo-500 text-white  text-center">
        <div className="container mx-auto pt-10 pb-20 px-4 max-w-3xl">
          <div id="buzzsprout-large-player"></div>
          <p className="text-xl mt-4">
            Build better APIs while saving time and money
          </p>
          <p className="text-xs uppercase mt-4">GraphJin</p>
        </div>
      </footer> */}
    </div>
  );
};
