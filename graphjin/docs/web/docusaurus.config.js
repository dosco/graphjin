module.exports = {
  title: "Super Graph",
  tagline: "Fetch data without code",
  url: "https://supergraph.dev",
  baseUrl: "/",
  favicon: "img/favicon.ico",
  organizationName: "dosco", // Usually your GitHub org/user name.
  projectName: "super-graph", // Usually your repo name.
  themeConfig: {
    navbar: {
      title: "Super Graph",
      logo: {
        alt: "Super Graph Logo",
        src: "img/logo.svg",
      },
      links: [
        {
          to: "docs/deploy",
          activeBasePath: "docs",
          label: "Docs",
          position: "left",
        },
        {
          href: "https://github.com/dosco/super-graph",
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
          href: "https://twitter.com/dosco",
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
          editUrl:
            "https://github.com/facebook/docusaurus/edit/master/website/",
        },
        blog: {
          showReadingTime: true,
          // Please change this to your repo.
          editUrl:
            "https://github.com/facebook/docusaurus/edit/master/website/blog/",
        },
        theme: {
          customCss: require.resolve("./src/css/custom.css"),
        },
      },
    ],
  ],
  themes: ["@docusaurus/theme-live-codeblock"],
};
