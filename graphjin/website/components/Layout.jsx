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
        <link
          rel="stylesheet"
          href="https://fonts.googleapis.com/css2?family=Material+Symbols+Outlined:opsz,wght,FILL,GRAD@48,400,0,0"
        />
      </Head>

      <nav className="w-full p-6 bg-black">
        <div className="container mx-auto px-10 flex gap-2 items-center justify-between">
          <h1 className="text-md font-normal text-white">
            <a href="/">GraphJin | APIs in 5 mins</a>
          </h1>

          <div className="flex gap-4">
            <a href="https://twitter.com/dosco" target="_blank">
              <FaTwitter size={20} className="text-white" />
            </a>
            <a href="https://github.com/dosco/graphjin" target="_blank">
              <FaGithub size={20} className="text-white" />
            </a>
          </div>
        </div>
      </nav>

      <main className="prose lg:prose-xl prose-h1:text-lime-500 container mx-auto mt-12 mb-24">
        {children}
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
