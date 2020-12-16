const remarkImages = require("remark-images");

module.exports = {
  title: "GraphJin",
  tagline: "Build APIs in 5 minutes not weeks",
  url: "https://graphjin.com",
  baseUrl: "/",
  favicon: "img/graphjin-logo.svg",
  organizationName: "dosco", // Usually your GitHub org/user name.
  projectName: "graphjin", // Usually your repo name.
  themeConfig: {
    navbar: {
      title: "GRAPHJIN",
      logo: {
        alt: "GraphJin Logo",
        src: "img/graphjin-logo.svg",
      },
      items: [
        {
          to: "docs/deploy",
          activeBasePath: "docs",
          label: "Docs",
          position: "left",
        },
        {
          href: "https://github.com/dosco/graphjin",
          label: "Github",
          position: "left",
        },
        {
          label: "Discord",
          href: "https://discord.gg/6pSWCTZ",
          position: "left",
        },
        {
          label: "Twitter",
          href: "https://twitter.com/intent/user?screen_name=dosco",
          position: "left",
        },
      ],
    },
    footer: {
      style: "dark",
      links: [],
      copyright: `Apache Public License 2.0 | Copyright © ${new Date().getFullYear()} Vikram Rangnekar`,
    },
  },
  presets: [
    [
      "@docusaurus/preset-classic",
      {
        docs: {
          sidebarPath: require.resolve("./sidebars.js"),
          // Please change this to your repo.
          editUrl: "https://github.com/dosco/graphjin/edit/master/docs/website",

          remarkPlugins: [remarkImages],
        },
        blog: {
          showReadingTime: true,
          // Please change this to your repo.
          editUrl: "https://github.com/dosco/graphjin/edit/master/docs/website",
        },
        theme: {
          customCss: require.resolve("./src/css/custom.css"),
        },
      },
    ],
  ],
  themes: ["@docusaurus/theme-live-codeblock"],
};
