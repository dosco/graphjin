module.exports = {
  title: 'Super Graph',
  description: 'Get an instant GraphQL API for your Rails apps.',

  themeConfig: {
    logo: '/logo.svg',
    nav: [
      { text: 'Guide', link: '/guide' },
      { text: 'Install', link: '/install' },
      { text: 'Github', link: 'https://github.com/dosco/super-graph' },
      { text: 'Docker', link: 'https://hub.docker.com/r/dosco/super-graph/builds' },
    ],
    serviceWorker: {
      updatePopup: true
    }
  }
}
