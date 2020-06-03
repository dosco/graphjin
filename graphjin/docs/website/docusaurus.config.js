module.exports = {
  title: "Super Graph",
  tagline: "Fetch data without code",
  url: "https://supergraph.dev",
  baseUrl: "/",
  favicon: "img/super-graph-logo.svg",
  organizationName: "dosco", // Usually your GitHub org/user name.
  projectName: "super-graph", // Usually your repo name.
  themeConfig: {
    navbar: {
      title: "Super Graph",
      logo: {
        alt: "Super Graph Logo",
        src: "img/super-graph-logo.svg",
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
        {
          label: "AbtCode",
          href: "https://abtcode.com/s/super-graph",
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
            "https://github.com/dosco/super-graph/edit/master/docs/website",
        },
        blog: {
          showReadingTime: true,
          // Please change this to your repo.
          editUrl:
            "https://github.com/dosco/super-graph/edit/master/docs/website",
        },
        theme: {
          customCss: require.resolve("./src/css/custom.css"),
        },
      },
    ],
  ],
  themes: ["@docusaurus/theme-live-codeblock"],
};
