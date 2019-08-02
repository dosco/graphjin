module.exports = {
  title: 'Super Graph',
  description: 'Get an instant GraphQL API for your Rails apps.',

  themeConfig: {
    logo: '/logo.svg',
    nav: [
      { text: 'Docs', link: '/guide' },
      { text: 'Deploy', link: '/deploy' },
      { text: 'Github', link: 'https://github.com/dosco/super-graph' },
      { text: 'Docker', link: 'https://hub.docker.com/r/dosco/super-graph/builds' },
    ],
    serviceWorker: {
      updatePopup: true
    },
  },

  postcss: {
    plugins: [
      require('postcss-import'),
      require('tailwindcss'),
      require('postcss-nested'),
      require('autoprefixer')
    ]
  },

  plugins: [
    '@vuepress/plugin-nprogress',
  ]
}
